"""Operator command tests: temporary public data, no model or real sandbox."""

import contextlib
import io
import json
import os
from pathlib import Path
import shutil
import sqlite3
import tempfile
import time
import unittest
from unittest.mock import patch

import preview_demo_real as cli
import preview_demo_real_task as real
import preview_demo_task as fake


TASK_ID = "a" * 32
TEXT = b"Synthetic task; do not publish this text."
PASSED = {"passed": True, "exitCode": 0, "oomKilled": False,
          "imageId": "sha256:" + "b" * 64, "logsCollected": False}


class RealCLITests(unittest.TestCase):
    def setUp(self):
        for target, name in ((real.transport, "send_once"), (real.RealTask, "execute"), (cli.preflight, "sandbox")):
            guard = patch.object(target, name, side_effect=AssertionError("unexpected external execution"))
            guard.start()
            self.addCleanup(guard.stop)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / "private"
        self.root.mkdir()
        self.source = self.base / "source"
        shutil.copytree(cli.preflight.DEFAULT_SOURCE, self.source)
        self.task_file = self.base / "task.txt"
        self.task_file.write_bytes(TEXT)
        self.reference = self.base / "reference.txt"
        self.reference.write_bytes(b"synthetic owner approval\r\n")
        self.now = int(time.time())
        self.price = real.protocol.PricingSnapshot("deepseek-flash", 1_000_000_000, 100_000_000,
                                                  2_000_000_000, 7_200_000, self.now - 1, self.now + 3600)
        self.price_file = self.base / "price.json"
        self.price_file.write_text(json.dumps(self.price.__dict__), encoding="utf-8")

    def invoke(self, *args):
        output = io.StringIO()
        errors = io.StringIO()
        with contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
            code = cli.main(list(args))
        self.assertEqual(errors.getvalue(), "")
        return code, json.loads(output.getvalue()), output.getvalue()

    def command(self, action, *extra):
        return self.invoke(action, "--root", str(self.root), "--task-id", TASK_ID, *extra)

    def prepare(self, *extra):
        return self.command("prepare", "--source", str(self.source), "--task-file", str(self.task_file),
                            "--price-file", str(self.price_file), "--rmb-fen", "10000", *extra)

    def archive(self):
        return real.RealTask.open(self.root, TASK_ID)

    def approve(self, plan):
        return self.command("approve", "--approve-plan-sha256", plan, "--reference-file", str(self.reference))

    def complete_synthetic(self):
        self.assertEqual(self.prepare()[0], 0)
        archive = self.archive()
        plan = archive.plan()["planSha256"]
        self.assertEqual(self.approve(plan)[0], 0)
        # Seed an in-memory synthetic result through the transaction seams only.
        # execute/send_once stay patched and are never called by this suite.
        prepared, token = archive._begin(plan, int(time.time()))
        captured, _, policy = archive.load()
        proposal = {"schemaVersion": "forgeflow.demo.patch/v1", "taskId": TASK_ID,
                    "taskSha256": policy["task_sha256"], "baseSnapshotSha256": policy["snapshot_sha256"],
                    "files": {"greeting.go": captured["greeting.go"].decode() + "\n// Synthetic CLI fixture.\n"}}
        value = {"id": "synthetic", "object": "chat.completion", "model": prepared.pricing.model,
                 "choices": [{"index": 0, "finish_reason": "stop", "message": {
                     "role": "assistant", "content": json.dumps(proposal)}}],
                 "usage": {"prompt_tokens": 100, "prompt_cache_hit_tokens": 0,
                           "prompt_cache_miss_tokens": 100, "completion_tokens": 10, "total_tokens": 110}}
        archive._save(token, real.transport.HTTPResult(json.dumps(value).encode()))
        return archive.proposal()[1]["approvalSha256"]

    def test_prepare_and_plan_show_numeric_configuration_without_raw_content(self):
        code, report, output = self.prepare()
        self.assertEqual(code, 0)
        self.assertEqual(report["state"], "prepared")
        self.assertEqual(report["plan"]["rmbFen"], 10_000)
        self.assertEqual(report["plan"]["pricing"], self.price.__dict__)
        self.assertEqual(report["plan"]["maxCalls"], 1)
        self.assertTrue(report["priceWindowValid"])
        for private in (TEXT.decode(), "func Greet", str(self.root), "synthetic owner approval"):
            self.assertNotIn(private, output)
        self.assertEqual(self.command("plan")[1]["plan"], report["plan"])
        self.assertEqual(self.command("inspect")[1]["budget"]["attempts"], 0)
        self.assertEqual(self.archive().load()[1], TEXT)

    def test_payload_requires_explicit_flag_and_uses_archive_not_changed_source(self):
        self.prepare()
        before = self.archive().summary()
        (self.source / "greeting.go").write_bytes(b"changed original")
        code, report, _ = self.command("plan", "--show-payload")
        self.assertEqual(code, 0)
        self.assertEqual(report["payload"], json.loads(self.archive().prepared().body))
        self.assertEqual(self.archive().summary(), before)
        self.assertNotIn("payload", self.command("plan")[1])

    def test_approve_records_reference_without_sending_and_refuses_repeat(self):
        self.prepare()
        plan = self.archive().plan()["planSha256"]
        self.assertEqual(self.approve("0" * 64)[0], 1)
        code, report, output = self.approve(plan)
        self.assertEqual(code, 0)
        self.assertEqual(report["state"], "approved")
        self.assertEqual(report["modelCalls"], 0)
        self.assertEqual(report["budget"]["attempts"], 0)
        self.assertFalse(report["paidExecutionEnabled"])
        self.assertNotIn("synthetic owner approval", output)
        self.assertEqual(self.approve(plan)[1]["reason"], "real_approval_already_recorded")

    def test_no_key_send_provider_override_or_abbreviated_flags_are_accepted(self):
        secret = "synthetic-value-do-not-echo"
        for args in (("send", "--key", secret), ("prepare", "--api-key", secret),
                     ("inspect", "--root", str(self.root), "--task-id", TASK_ID, "--provider", secret),
                     ("plan", "--root", str(self.root), "--task-id", TASK_ID, "--show-pay")):
            code, report, output = self.invoke(*args)
            self.assertEqual(code, 1)
            self.assertEqual(report["reason"], "real_cli_arguments")
            self.assertNotIn(secret, output)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_strict_price_schema_rejects_duplicates_extra_fields_and_nonfinite(self):
        for raw in (b'{"model":"x","model":"y"}', b'{"rate":NaN}', b'[]',
                    json.dumps({**self.price.__dict__, "api_key": "synthetic"}).encode(), b"x" * 4097):
            self.price_file.write_bytes(raw)
            self.assertEqual(self.prepare()[0], 1)
            self.assertEqual(list(self.root.iterdir()), [])

    def test_invalid_limits_and_expired_price_do_not_create_archive(self):
        for extra in (("--rmb-fen", "1"), ("--rmb-fen", "1.5"), ("--timeout", "121"),
                      ("--max-output-tokens", "8193")):
            self.assertEqual(self.prepare(*extra)[0], 1)
            self.assertEqual(list(self.root.iterdir()), [])
        with patch.object(real.time, "time", return_value=self.price.valid_until):
            self.assertEqual(self.prepare()[0], 1)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_expired_archive_is_inspectable_but_cannot_be_approved(self):
        self.prepare()
        plan = self.archive().plan()["planSha256"]
        with patch.object(real.time, "time", return_value=self.price.valid_until):
            self.assertEqual(self.command("inspect")[0], 0)
            code, report, _ = self.command("plan")
            self.assertEqual(code, 0)
            self.assertFalse(report["priceWindowValid"])
            self.assertEqual(self.approve(plan)[0], 1)
        self.assertEqual(self.archive().summary()["state"], "prepared")

    def test_bad_reference_and_missing_files_are_sanitized(self):
        self.prepare()
        plan = self.archive().plan()["planSha256"]
        for raw in (b"", b"x" * 259, b"embedded\nline", b"\xff"):
            self.reference.write_bytes(raw)
            self.assertEqual(self.approve(plan)[0], 1)
        code, _, output = self.command("approve", "--approve-plan-sha256", plan,
                                       "--reference-file", str(self.base / "private-missing-file"))
        self.assertEqual(code, 1)
        self.assertNotIn("private-missing-file", output)
        self.assertEqual(self.archive().summary()["state"], "prepared")

    def test_fake_archive_duplicate_id_and_missing_root_are_not_recreated(self):
        fake.TaskArchive.create(self.root, TASK_ID, cli.preflight.snapshot(self.source), TEXT)
        self.assertEqual(self.command("inspect")[0], 1)
        self.assertEqual(self.prepare()[1]["reason"], "task_already_exists")
        self.assertEqual(fake.TaskArchive.open(self.root, TASK_ID).summary()["provider"], "fake")
        missing = self.base / "missing-root"
        self.assertEqual(self.command("inspect", "--root", str(missing))[0], 1)
        self.assertFalse(missing.exists())

    def test_input_whitelist_task_limit_and_nested_root_are_enforced(self):
        (self.source / ".env").write_bytes(b"synthetic marker")
        self.assertEqual(self.prepare()[1]["reason"], "unexpected_files")
        (self.source / ".env").unlink()
        self.task_file.write_bytes(b"x" * 20_001)
        self.assertEqual(self.prepare()[1]["reason"], "invalid_task_text")
        self.task_file.write_bytes(TEXT)
        self.assertEqual(self.prepare("--root", str(self.source))[1]["reason"], "task_root_inside_source")
        self.assertEqual(list(self.root.iterdir()), [])

    def test_hard_linked_price_is_refused(self):
        linked = self.base / "linked.json"
        os.link(self.price_file, linked)
        self.assertEqual(self.prepare("--price-file", str(linked))[0], 1)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_review_shows_candidate_but_inspect_only_shows_receipt(self):
        approval = self.complete_synthetic()
        code, report, output = self.command("inspect")
        self.assertEqual(code, 0)
        self.assertEqual(report["state"], "completed")
        self.assertEqual(report["candidate"]["evidence"]["state"], "reviewed")
        self.assertNotIn("Synthetic CLI fixture", output)
        code, report, _ = self.command("review")
        self.assertEqual(code, 0)
        self.assertEqual(report["review"]["approvalSha256"], approval)
        self.assertIn("Synthetic CLI fixture", report["review"]["diff"])
        self.assertFalse(report["sandboxAttempted"])

    def test_candidate_execution_uses_archived_bytes_and_separate_single_use_approval(self):
        approval = self.complete_synthetic()
        with patch.object(cli.preflight, "sandbox", return_value=PASSED) as sandbox:
            plan = self.archive().plan()["planSha256"]
            self.assertEqual(self.command("test", "--approve-candidate-sha256", plan)[0], 1)
            sandbox.assert_not_called()
            code, report, _ = self.command("test", "--approve-candidate-sha256", approval)
            self.assertEqual(code, 0)
            self.assertTrue(report["sandboxExecuted"])
            self.assertEqual(report["candidate"]["evidence"]["state"], "completed")
            self.assertEqual(sandbox.call_args.args[0], self.archive().proposal()[0])
            self.assertEqual(self.command("test", "--approve-candidate-sha256", approval)[0], 1)
            self.assertEqual(sandbox.call_count, 1)

    def test_failed_candidate_is_reported_failed_not_request_completed_success(self):
        approval = self.complete_synthetic()
        with patch.object(cli.preflight, "sandbox", return_value={**PASSED, "passed": False, "exitCode": 1}):
            code, report, _ = self.command("test", "--approve-candidate-sha256", approval)
        self.assertEqual(code, 1)
        self.assertFalse(report["checksPassed"])
        self.assertEqual(report["state"], "completed")
        self.assertFalse(report["candidate"]["evidence"]["result"]["passed"])

    def test_interrupted_sandbox_preserves_unknown_receipt_and_does_not_retry(self):
        approval = self.complete_synthetic()
        with patch.object(cli.preflight, "sandbox", side_effect=KeyboardInterrupt()) as sandbox:
            code, report, _ = self.command("test", "--approve-candidate-sha256", approval)
            self.assertEqual(code, 1)
            self.assertEqual(report["reason"], "interrupted")
            self.assertEqual(report["candidate"]["evidence"]["state"], "unknown")
            self.assertEqual(self.command("test", "--approve-candidate-sha256", approval)[0], 1)
            self.assertEqual(sandbox.call_count, 1)

    def test_unknown_request_cannot_be_approved_or_tested_again(self):
        self.prepare()
        archive = self.archive()
        plan = archive.plan()["planSha256"]
        self.approve(plan)
        _, token = archive._begin(plan, self.now)
        archive._unknown(token)
        self.assertEqual(self.command("inspect")[1]["state"], "unknown")
        self.assertEqual(self.approve(plan)[0], 1)
        self.assertEqual(self.command("test", "--approve-candidate-sha256", "0" * 64)[0], 1)
        self.assertEqual(archive.summary()["budget"]["attempts"], 1)

    def test_corrupt_archive_is_reported_unavailable_without_raw_traceback(self):
        self.prepare()
        with contextlib.closing(sqlite3.connect(self.archive().database)) as conn:
            conn.execute("UPDATE real_plan SET document=?", (b"synthetic private corruption",))
            conn.commit()
        code, report, output = self.command("inspect")
        self.assertEqual(code, 1)
        self.assertFalse(report["checksPassed"])
        self.assertNotIn("synthetic private corruption", output)
        self.assertNotIn("Traceback", output)


if __name__ == "__main__":
    unittest.main()
