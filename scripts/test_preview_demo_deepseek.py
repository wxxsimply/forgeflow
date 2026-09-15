"""Synthetic protocol/price fixtures only; never read a key or contact DeepSeek."""

import copy
from dataclasses import FrozenInstanceError, replace
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import preview_demo_deepseek as demo
import preview_demo_request as request
import preview_demo_task as task


NOW = 10_000
TASK_ID = "4" * 32


class DeepSeekProtocolTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.captured = task.preflight.snapshot(task.preflight.DEFAULT_SOURCE)
        self.archive = task.TaskArchive.create(self.root, TASK_ID, self.captured, b"Synthetic task; no network.")
        # Synthetic prices/FX, deliberately not a current provider quotation.
        self.price = demo.PricingSnapshot("deepseek-flash", 1_000_000_000, 100_000_000,
                                          2_000_000_000, 7_200_000, NOW - 1, NOW + 3600)
        self.prepared = demo.prepare(self.archive, self.price, now=NOW, rmb_fen=10_000)
        payload = request.RequestFlow(self.archive).preview(1000)[0]
        proposal = request.fake_send_once(payload)[0].decode()
        self.value = {"id": "synthetic-response-1", "object": "chat.completion", "model": "deepseek-flash",
                      "choices": [{"index": 0, "finish_reason": "stop", "message": {"role": "assistant", "content": proposal}}],
                      "usage": {"prompt_tokens": 100, "prompt_cache_hit_tokens": 20,
                                "prompt_cache_miss_tokens": 80, "completion_tokens": 10, "total_tokens": 110}}

    def parse(self, value=None):
        return demo.parse_response(json.dumps(self.value if value is None else value).encode(), self.prepared)

    def test_request_uses_archived_whitelist_and_explicit_safe_options(self):
        wire = json.loads(self.prepared.body)
        self.assertEqual(set(wire), {"model", "stream", "thinking", "max_tokens", "response_format", "messages"})
        self.assertIs(wire["stream"], False)
        self.assertEqual(wire["thinking"], {"type": "disabled"})
        self.assertEqual(wire["response_format"], {"type": "json_object"})
        self.assertEqual(wire["max_tokens"], 4096)
        self.assertEqual([message["role"] for message in wire["messages"]], ["system", "user"])
        data = json.loads(wire["messages"][1]["content"])
        self.assertEqual(set(data), {"taskId", "taskSha256", "baseSnapshotSha256", "task", "files"})
        self.assertEqual(set(data["files"]), set(task.preflight.FILES))
        self.assertEqual({name: value.encode() for name, value in data["files"].items()}, self.captured)
        self.assertEqual(demo.ENDPOINT, "https://api.deepseek.com/chat/completions")

    def test_preparation_never_changes_fake_ledger_or_exposes_raw_data(self):
        before = self.archive.summary()
        with patch.object(request, "fake_send_once", side_effect=AssertionError("no callback")), \
                patch.object(task.preflight, "sandbox", side_effect=AssertionError("no execution")):
            summary = demo.prepare(self.archive, self.price, now=NOW, rmb_fen=10_000).summary()
        self.assertEqual(self.archive.summary(), before)
        self.assertFalse(summary["paidExecutionEnabled"])
        self.assertFalse(summary["readyForPaidExecution"])
        for secret in ("func Greet", "Synthetic task", str(self.root)):
            self.assertNotIn(secret, json.dumps(summary))
            self.assertNotIn(secret, repr(self.prepared))
        with self.assertRaises(FrozenInstanceError):
            self.prepared.body = b"changed"

    def test_fee_is_integer_rounded_up_and_limit_rounded_down(self):
        self.assertEqual(self.price.cost(100, 20, 10), 102_000)
        tiny = replace(self.price, input_nano_usd_per_million=1, cached_nano_usd_per_million=1,
                       output_nano_usd_per_million=1)
        self.assertEqual(tiny.cost(1, 0, 1), 1)
        self.assertEqual(self.price.usd_limit(100), 138_888_888)
        self.assertEqual(self.price.cost(0, 0, 0), 0)

    def test_reserve_uses_full_input_ceiling_and_output_not_cache_guess(self):
        self.assertEqual(self.prepared.reserved_nano_usd,
                         self.price.cost(demo.INPUT_TOKEN_CEILING, 0, 4096))
        self.assertTrue(self.prepared.summary()["fitsBudget"])
        tiny = demo.prepare(self.archive, self.price, now=NOW, rmb_fen=1)
        self.assertFalse(tiny.summary()["fitsBudget"])
        self.assertFalse(tiny.summary()["readyForPaidExecution"])

    def test_plan_hash_changes_with_price_fx_model_and_output(self):
        base = self.prepared.summary()["planSha256"]
        for price in (replace(self.price, input_nano_usd_per_million=2_000_000_000),
                      replace(self.price, fx_micro_cny_per_usd=7_500_000),
                      replace(self.price, model="deepseek-v4-pro")):
            self.assertNotEqual(demo.prepare(self.archive, price, now=NOW, rmb_fen=10_000).summary()["planSha256"], base)
        changed = demo.prepare(self.archive, self.price, now=NOW, rmb_fen=10_000, max_output_tokens=2048)
        self.assertNotEqual(changed.summary()["planSha256"], base)

    def test_expired_future_and_short_price_windows_block(self):
        for price in (replace(self.price, valid_until=NOW), replace(self.price, valid_from=NOW + 1),
                      replace(self.price, valid_until=NOW + 60), replace(self.price, valid_until=NOW + 86401)):
            with self.assertRaises(demo.Blocked):
                demo.prepare(self.archive, price, now=NOW, rmb_fen=100)

    def test_invalid_rates_fx_models_and_units_rejected(self):
        for changes in ({"model": "deepseek-chat"}, {"model": []}, {"source": "https://example.invalid"},
                        {"input_nano_usd_per_million": True}, {"output_nano_usd_per_million": 0},
                        {"cached_nano_usd_per_million": 2_000_000_000}, {"fx_micro_cny_per_usd": 7.2},
                        {"valid_from": False}):
            with self.assertRaises(demo.Blocked):
                replace(self.price, **changes).validate()
        for value in (0, -1, True, 0.01, "100"):
            with self.assertRaises(demo.Blocked):
                self.price.usd_limit(value)
        for output in (0, True, 8193):
            with self.assertRaises(demo.Blocked):
                demo.prepare(self.archive, self.price, now=NOW, rmb_fen=100, max_output_tokens=output)

    def test_valid_response_retains_exact_raw_and_private_proposal(self):
        result = self.parse()
        self.assertIsNotNone(result.proposal)
        self.assertEqual(result.proposal.decode(), self.value["choices"][0]["message"]["content"])
        self.assertEqual(result.raw, json.dumps(self.value).encode())
        self.assertEqual(result.estimated_nano_usd, 102_000)
        self.assertFalse(result.limit_exceeded)
        self.assertTrue(result.summary()["proposalAccepted"])
        self.assertNotIn("Offline Fake request lifecycle", json.dumps(result.summary()))
        self.assertNotIn("Offline Fake request lifecycle", repr(result))

    def test_missing_and_inconsistent_usage_never_imply_free_call(self):
        for key in self.value["usage"]:
            value = copy.deepcopy(self.value)
            del value["usage"][key]
            with self.assertRaisesRegex(demo.Blocked, "deepseek_usage_unknown"):
                self.parse(value)
        for key, number in (("prompt_tokens", 99), ("total_tokens", 111), ("completion_tokens", True),
                            ("prompt_cache_hit_tokens", -1), ("prompt_cache_miss_tokens", 1.0)):
            value = copy.deepcopy(self.value)
            value["usage"][key] = number
            with self.assertRaisesRegex(demo.Blocked, "deepseek_invalid_usage"):
                self.parse(value)

    def test_optional_usage_breakdowns_must_agree(self):
        self.value["usage"]["prompt_tokens_details"] = {"cached_tokens": 20}
        self.value["usage"]["completion_tokens_details"] = {"reasoning_tokens": 0}
        self.assertIsNotNone(self.parse().proposal)
        for detail in ({"cached_tokens": 21}, {"cached_tokens": True}, None):
            self.value["usage"]["prompt_tokens_details"] = detail
            with self.assertRaisesRegex(demo.Blocked, "deepseek_invalid_usage"):
                self.parse()

    def test_invalid_or_truncated_proposal_keeps_known_cost(self):
        cases = []
        truncated = copy.deepcopy(self.value)
        truncated["choices"][0]["finish_reason"] = "length"
        cases.append(truncated)
        invalid = copy.deepcopy(self.value)
        invalid["choices"][0]["message"]["content"] = "not JSON"
        cases.append(invalid)
        missing = copy.deepcopy(self.value)
        missing["choices"] = []
        cases.append(missing)
        for value in cases:
            result = self.parse(value)
            self.assertIsNone(result.proposal)
            self.assertEqual(result.estimated_nano_usd, 102_000)
            self.assertIsNotNone(result.proposal_reason)

    def test_tool_calls_and_reasoning_are_not_executable_proposals(self):
        for key, content in (("tool_calls", [{"function": {"name": "execute"}}]),
                             ("function_call", {"name": "execute"}), ("reasoning_content", "synthetic reasoning")):
            value = copy.deepcopy(self.value)
            value["choices"][0]["message"][key] = content
            result = self.parse(value)
            self.assertIsNone(result.proposal)
            self.assertEqual(result.estimated_nano_usd, 102_000)
        self.value["usage"]["completion_tokens_details"] = {"reasoning_tokens": 5}
        result = self.parse()
        self.assertEqual(result.proposal_reason, "deepseek_unexpected_reasoning")
        self.assertEqual(result.completion_tokens, 10)  # Never subtract reasoning usage.

    def test_token_overrun_preserves_estimate_but_blocks_candidate(self):
        self.value["usage"].update(completion_tokens=8193, total_tokens=8293)
        result = self.parse()
        self.assertTrue(result.limit_exceeded)
        self.assertIsNone(result.proposal)
        self.assertEqual(result.estimated_nano_usd, self.price.cost(100, 20, 8193))

    def test_returned_model_and_object_must_match_without_alias_fallback(self):
        for key, value in (("model", "deepseek-v4-flash"), ("object", "chat.completion.chunk"), ("id", "bad\nidentifier")):
            with self.assertRaisesRegex(demo.Blocked, "deepseek_response_identity"):
                self.parse({**self.value, key: value})

    def test_non_200_redirects_and_rate_limits_remain_unknown(self):
        for status in (301, 302, 401, 429, 500, True):
            with self.assertRaisesRegex(demo.Blocked, "deepseek_http_outcome_unknown"):
                demo.parse_response(b"synthetic error, not for stdout", self.prepared, status=status)

    def test_duplicate_keys_nonfinite_and_invalid_encodings_rejected(self):
        for raw in (b'{"id":"one","id":"two"}', b'{"x":NaN}', b'{"x":Infinity}', b'\xff', b'{'):
            with self.assertRaisesRegex(demo.Blocked, "deepseek_invalid_json"):
                demo.parse_response(raw, self.prepared)

    def test_response_and_content_bounds_are_enforced(self):
        for raw in (b"", b" " * (demo.MAX_BODY_BYTES + 1)):
            with self.assertRaisesRegex(demo.Blocked, "deepseek_response_size"):
                demo.parse_response(raw, self.prepared)
        self.value["choices"][0]["message"]["content"] = "x" * (demo.MAX_PROPOSAL_BYTES + 1)
        result = self.parse()
        self.assertIsNone(result.proposal)
        self.assertEqual(result.proposal_reason, "deepseek_proposal_size")
        self.assertEqual(result.estimated_nano_usd, 102_000)

    def test_existing_patch_guards_reject_wrong_task_and_extra_files(self):
        original = json.loads(self.value["choices"][0]["message"]["content"])
        for changes in ({"taskId": "5" * 32}, {"files": {".env": "not a real credential"}}):
            self.value["choices"][0]["message"]["content"] = json.dumps({**original, **changes})
            result = self.parse()
            self.assertIsNone(result.proposal)
            self.assertEqual(result.estimated_nano_usd, 102_000)


if __name__ == "__main__":
    unittest.main()
