"""Synthetic real-task integration: HTTP is always replaced, never paid."""

from concurrent.futures import ThreadPoolExecutor
import contextlib
import json
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

import preview_demo_real_task as real
import preview_demo_task as task


TASK_ID = "7" * 32
KEY = "synthetic-key-not-a-credential"
TEXT = b"Synthetic task only; no network."
PASSED = {"passed": True, "exitCode": 0, "oomKilled": False,
          "imageId": "sha256:" + "a" * 64, "logsCollected": False}


class RealTaskTests(unittest.TestCase):
    def setUp(self):
        # Guard the paid seam BEFORE any preparation. Tests explicitly replace
        # this default only with local callbacks or synthetic HTTPResult values.
        guard = patch.object(real.transport, "send_once", side_effect=AssertionError("network forbidden"))
        self.send = guard.start()
        self.addCleanup(guard.stop)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "private"
        self.root.mkdir()
        self.now = int(time.time())
        self.captured = task.preflight.snapshot(task.preflight.DEFAULT_SOURCE)
        self.price = real.protocol.PricingSnapshot("deepseek-flash", 1_000_000_000, 100_000_000,
                                                  2_000_000_000, 7_200_000, self.now - 1, self.now + 3600)
        self.archive = real.RealTask.create(self.root, TASK_ID, self.captured, TEXT,
                                            self.price, now=self.now, rmb_fen=10_000)
        self.plan = self.archive.prepared().summary()["planSha256"]
        proposal = {"schemaVersion": "forgeflow.demo.patch/v1", "taskId": TASK_ID,
                    "taskSha256": real.protocol.sha(TEXT),
                    "baseSnapshotSha256": real.manifest(self.captured)["snapshotSha256"],
                    "files": {"greeting.go": self.captured["greeting.go"].decode() + "\n// Synthetic response.\n"}}
        self.value = {"id": "synthetic-response", "object": "chat.completion", "model": "deepseek-flash",
                      "choices": [{"index": 0, "finish_reason": "stop",
                                   "message": {"role": "assistant", "content": json.dumps(proposal)}}],
                      "usage": {"prompt_tokens": 100, "prompt_cache_hit_tokens": 20,
                                "prompt_cache_miss_tokens": 80, "completion_tokens": 10, "total_tokens": 110}}

    def sql(self, statement, values=()):
        with contextlib.closing(sqlite3.connect(self.archive.database)) as conn:
            rows = conn.execute(statement, values).fetchall()
            conn.commit()
            return rows

    def approve(self, *, sandbox=True):
        self.archive.approve(self.plan, "synthetic owner confirmation", now=self.now)
        if sandbox:
            self.archive.record_sandbox(self.plan, "golang:1.22", 90, PASSED, now=self.now)

    def response(self):
        return real.transport.HTTPResult(json.dumps(self.value).encode())

    def execute(self):
        return self.archive.execute(KEY, self.plan)

    def complete(self):
        self.approve()
        self.send.side_effect = None
        self.send.return_value = self.response()
        return self.execute()

    def assert_held(self, state):
        summary = real.RealTask.open(self.root, TASK_ID).summary()
        self.assertEqual(summary["state"], state)
        self.assertEqual(summary["budget"]["attempts"], 1)
        self.assertEqual(summary["budget"]["held_nano_usd"], self.archive.prepared().reserved_nano_usd)
        return summary

    def assert_consumed(self):
        calls = self.send.call_count
        with self.assertRaises(real.Blocked):
            self.execute()
        self.assertEqual(self.send.call_count, calls)

    def test_preparation_archives_real_policy_and_exact_inputs_without_calls(self):
        captured, text, policy = self.archive.load()
        self.assertEqual((captured, text), (self.captured, TEXT))
        self.assertEqual((policy["provider"], policy["max_calls"]), ("deepseek", 1))
        report = self.archive.summary()
        self.assertEqual(report["state"], "prepared")
        self.assertEqual(report["budget"]["attempts"], 0)
        self.assertIsNone(report["sandbox"])
        self.assertTrue(report["paidCLIEnabled"])
        for private in (TEXT.decode(), "func Greet", str(self.root), KEY):
            self.assertNotIn(private, json.dumps(report))
        self.send.assert_not_called()

    def test_fake_and_real_cannot_open_each_other_or_reuse_task_id(self):
        with self.assertRaises(real.Blocked):
            task.TaskArchive.open(self.root, TASK_ID)
        task.TaskArchive.create(self.root, "8" * 32, self.captured, TEXT)
        with self.assertRaises(real.Blocked):
            real.RealTask.open(self.root, "8" * 32)
        with self.assertRaisesRegex(real.Blocked, "task_already_exists"):
            real.RealTask.create(self.root, TASK_ID, self.captured, TEXT, self.price, rmb_fen=10_000)

    def test_insufficient_budget_rejected_before_creating_directory(self):
        with self.assertRaisesRegex(real.Blocked, "real_budget_insufficient"):
            real.RealTask.create(self.root, "9" * 32, self.captured, TEXT, self.price, rmb_fen=1)
        self.assertFalse((self.root / ("9" * 32)).exists())
        self.send.assert_not_called()

    def test_initialization_failure_leaves_occupied_task_not_resettable(self):
        with patch.object(real.BudgetLedger, "create", side_effect=real.BudgetBlocked("synthetic")):
            with self.assertRaisesRegex(real.Blocked, "real_initialization_failed"):
                real.RealTask.create(self.root, "9" * 32, self.captured, TEXT, self.price, rmb_fen=10_000)
        with self.assertRaisesRegex(real.Blocked, "task_already_exists"):
            real.RealTask.create(self.root, "9" * 32, self.captured, TEXT, self.price, rmb_fen=10_000)

    def test_approval_required_and_bad_key_rejected_before_budget_or_send(self):
        with self.assertRaisesRegex(real.Blocked, "real_not_approved_or_consumed"):
            self.execute()
        self.approve()
        with self.assertRaises(real.Blocked):
            self.archive.execute("bad", self.plan)
        with self.assertRaisesRegex(real.Blocked, "real_approval_mismatch"):
            self.archive.execute(KEY, "0" * 64)
        self.assertEqual(self.archive.summary()["budget"]["attempts"], 0)
        self.send.assert_not_called()

    def test_approval_persists_hash_binding_and_cannot_be_replaced(self):
        for plan, reference in (("0" * 64, "owner"), (self.plan, ""), (self.plan, "line\nbreak")):
            with self.assertRaises(real.Blocked):
                self.archive.approve(plan, reference, now=self.now)
        self.approve()
        summary = real.RealTask.open(self.root, TASK_ID).summary()
        self.assertEqual(summary["state"], "approved")
        self.assertEqual(summary["approvalReferenceSha256"], real.protocol.sha(b"synthetic owner confirmation"))
        self.assertNotIn("synthetic owner confirmation", json.dumps(summary))
        with self.assertRaisesRegex(real.Blocked, "real_approval_already_recorded"):
            self.approve()
        self.send.assert_not_called()

    def test_sandbox_receipt_is_required_bound_and_single_use(self):
        with self.assertRaisesRegex(real.Blocked, "real_sandbox_requires_approval"):
            self.archive.record_sandbox(self.plan, "golang:1.22", 90, PASSED, now=self.now)
        self.approve(sandbox=False)
        self.sql("DROP TABLE real_sandbox")
        with self.assertRaisesRegex(real.Blocked, "real_sandbox_not_ready"):
            self.execute()
        self.send.assert_not_called()
        self.assertEqual(self.archive.summary()["budget"]["attempts"], 0)
        for plan, image, timeout, result in (
                ("0" * 64, "golang:1.22", 90, PASSED),
                (self.plan, "--privileged", 90, PASSED),
                (self.plan, "golang:1.22", 0, PASSED),
                (self.plan, "golang:1.22", 90, {}),
                (self.plan, "golang:1.22", 90, {**PASSED, "passed": False})):
            with self.assertRaises(real.Blocked):
                self.archive.record_sandbox(plan, image, timeout, result, now=self.now)
        receipt = self.archive.record_sandbox(self.plan, "golang:1.22", 90, PASSED, now=self.now)
        self.assertEqual(receipt["planSha256"], self.plan)
        self.assertEqual(receipt["imageId"], PASSED["imageId"])
        self.assertEqual(receipt["sandboxProfile"], real.SANDBOX_PROFILE)
        with self.assertRaisesRegex(real.Blocked, "real_sandbox_already_recorded"):
            self.archive.record_sandbox(self.plan, "golang:1.22", 90, PASSED, now=self.now)

    def test_tampered_sandbox_receipt_fails_closed(self):
        self.approve()
        self.sql("UPDATE real_sandbox SET image_id=?", ("sha256:" + "0" * 63,))
        with self.assertRaisesRegex(real.Blocked, "real_sandbox_mismatch"):
            self.archive.summary()
        self.send.assert_not_called()

    def test_approval_cannot_precede_plan_creation(self):
        with self.assertRaisesRegex(real.Blocked, "real_approval_mismatch"):
            self.archive.approve(self.plan, "owner", now=self.now - 1)
        self.assertEqual(self.archive.summary()["state"], "prepared")

    def test_expired_price_blocks_new_send_but_keeps_inspection_readable(self):
        self.approve()
        with patch.object(real.time, "time", return_value=self.price.valid_until + 1):
            self.assertEqual(self.archive.summary()["state"], "approved")
            with self.assertRaises(real.Blocked):
                self.execute()
        self.send.assert_not_called()
        self.assertEqual(self.archive.summary()["budget"]["attempts"], 0)

    def test_call_happens_after_durable_hold_then_raw_receipt_and_cost_saved(self):
        self.approve()
        response = self.response()
        def send(prepared, key):
            self.assert_held("running")
            self.assertEqual(key, KEY)
            self.assertEqual(prepared.body, self.archive.prepared().body)
            return response
        self.send.side_effect = send
        with patch.object(task.preflight, "sandbox", side_effect=AssertionError("no candidate execution")):
            result = self.execute()
        self.assertEqual(result["state"], "completed")
        self.assertEqual(result["budget"]["held_nano_usd"], 0)
        self.assertEqual(result["budget"]["spent_nano_usd"], 102_000)
        self.assertEqual(result["accountingMode"], "conservative_estimate_not_invoice")
        self.assertEqual(self.sql("SELECT raw FROM real_state"), [(response.body,)])
        self.assertEqual(self.sql("SELECT state FROM receipt"), [("reviewed",)])
        self.assertEqual(self.archive.proposal()[3]["origin"], "deepseek_response")
        self.assert_consumed()

    def test_candidate_still_requires_separate_approval_and_runs_only_once(self):
        self.complete()
        _, details, journal, _ = self.archive.proposal()
        callback = unittest.mock.Mock(return_value=PASSED)
        with self.assertRaisesRegex(real.Blocked, "approval_mismatch"):
            journal.run_once(self.plan, "golang:1.26-alpine", 60, callback)
        callback.assert_not_called()
        journal.run_once(details["approvalSha256"], "golang:1.26-alpine", 60, callback)
        self.assertEqual(callback.call_count, 1)
        with self.assertRaisesRegex(real.Blocked, "evidence_already_consumed"):
            journal.run_once(details["approvalSha256"], "golang:1.26-alpine", 60, callback)
        self.assertEqual(self.archive.summary()["state"], "completed")
        self.assertTrue(self.archive.proposal()[2].summary()["result"]["passed"])

    def test_bad_proposal_retains_known_cost_without_candidate_receipt(self):
        self.value["choices"][0]["finish_reason"] = "length"
        result = self.complete()
        self.assertEqual(result["state"], "rejected")
        self.assertEqual(result["budget"]["spent_nano_usd"], 102_000)
        self.assertIsNone(self.archive.proposal(required=False))
        self.assertEqual(self.sql("SELECT name FROM sqlite_master WHERE name='receipt'"), [])
        self.assertEqual(self.sql("SELECT raw FROM real_state"), [(self.response().body,)])
        self.assert_consumed()

    def test_unknown_usage_keeps_raw_and_entire_hold_without_retry(self):
        del self.value["usage"]
        self.complete()
        report = self.assert_held("usage_unknown")
        self.assertIsNone(report["response"])
        self.assertEqual(self.sql("SELECT raw FROM real_state"), [(self.response().body,)])
        self.assertEqual(self.sql("SELECT name FROM sqlite_master WHERE name='receipt'"), [])
        self.assert_consumed()

    def test_timeout_is_sanitized_and_preserves_hold_and_cleanup_pid(self):
        self.approve()
        failure = real.Blocked("synthetic secret " + KEY)
        failure.worker_pid = 12345
        self.send.side_effect = failure
        with self.assertRaisesRegex(real.Blocked, "^real_transport_unknown$") as caught:
            self.execute()
        self.assertEqual(caught.exception.worker_pid, 12345)
        report = self.assert_held("unknown")
        self.assertNotIn(KEY, json.dumps(report))
        self.assertEqual(self.sql("SELECT raw FROM real_state"), [(None,)])
        self.assert_consumed()

    def test_invalid_transport_result_keeps_hold(self):
        self.approve()
        self.send.side_effect = None
        self.send.return_value = None
        with self.assertRaisesRegex(real.Blocked, "real_result_not_saved"):
            self.execute()
        self.assert_held("unknown")
        self.assert_consumed()

    def fail_commit(self, target):
        class FailedCommit(sqlite3.Connection):
            def commit(self):
                if self.execute("SELECT state FROM real_state WHERE id=1").fetchone() == (target,):
                    raise sqlite3.OperationalError("synthetic commit failure")
                return super().commit()
        connect = sqlite3.connect
        return patch.object(real.sqlite3, "connect", side_effect=lambda *a, **k: connect(*a, **k, factory=FailedCommit))

    def test_begin_commit_failure_sends_nothing_and_rolls_back_hold(self):
        self.approve()
        with self.fail_commit("running"):
            with self.assertRaises(real.Blocked):
                self.execute()
        self.send.assert_not_called()
        self.assertEqual(self.archive.summary()["state"], "approved")
        self.assertEqual(self.archive.summary()["budget"]["attempts"], 0)

    def test_result_commit_failure_rolls_back_raw_receipt_and_settlement(self):
        self.approve()
        self.send.side_effect = None
        self.send.return_value = self.response()
        with self.fail_commit("completed"):
            with self.assertRaisesRegex(real.Blocked, "real_result_not_saved"):
                self.execute()
        self.assert_held("unknown")
        self.assertEqual(self.sql("SELECT raw,report FROM real_state"), [(None, None)])
        self.assertEqual(self.sql("SELECT name FROM sqlite_master WHERE name='receipt'"), [])
        self.assert_consumed()

    def test_unknown_commit_failure_still_leaves_durable_running_block(self):
        self.approve()
        self.send.side_effect = TimeoutError("synthetic")
        with self.fail_commit("unknown"):
            with self.assertRaisesRegex(real.Blocked, "real_transport_unknown"):
                self.execute()
        self.assert_held("running")
        self.assert_consumed()

    def test_process_death_inside_mocked_transport_blocks_restart(self):
        self.approve()
        code = "import os,sys; from unittest.mock import patch; import preview_demo_real_task as r; " \
               "p=patch.object(r.transport,'send_once',side_effect=lambda *a: os._exit(23)); p.start(); " \
               "a=r.RealTask.open(sys.argv[1],sys.argv[2]); a.execute('synthetic-key-not-a-credential',sys.argv[3])"
        result = subprocess.run([sys.executable, "-B", "-c", code, str(self.root), TASK_ID, self.plan],
                                cwd=Path(real.__file__).parent, capture_output=True, timeout=10,
                                creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        self.assertEqual(result.returncode, 23, result.stderr)
        self.assert_held("running")
        self.assert_consumed()
        self.send.assert_not_called()

    def test_two_concurrent_executors_only_send_once(self):
        self.approve()
        barrier = threading.Barrier(2)
        self.send.side_effect = None
        self.send.return_value = self.response()
        def run(_):
            barrier.wait(timeout=5)
            try:
                return real.RealTask(self.root, TASK_ID).execute(KEY, self.plan)["state"]
            except real.Blocked as error:
                return str(error)
        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(run, range(2)))
        self.assertCountEqual(results, ["completed", "real_not_approved_or_consumed"])
        self.assertEqual(self.send.call_count, 1)

    def test_changed_inputs_policy_state_or_raw_fail_closed(self):
        document = self.sql("SELECT document FROM real_plan")[0][0]
        self.sql("UPDATE real_plan SET document=?", (document + b" ",))
        with self.assertRaises(real.Blocked):
            self.archive.summary()
        self.sql("UPDATE real_plan SET document=?", (document,))
        policy = self.sql("SELECT body FROM policy")[0][0]
        self.sql("UPDATE policy SET body='{}'")
        with self.assertRaises(real.Blocked):
            self.archive.summary()
        self.sql("UPDATE policy SET body=?", (policy,))
        self.complete()
        self.sql("UPDATE real_state SET raw=?", (b"{}",))
        with self.assertRaises(real.Blocked):
            self.archive.summary()
        self.assert_consumed()

    def test_copied_directory_and_manual_import_are_rejected(self):
        other = Path(self.temp.name) / "other"
        other.mkdir()
        shutil.copytree(self.archive.path, other / TASK_ID)
        with self.assertRaisesRegex(real.Blocked, "real_plan_binding_mismatch"):
            real.RealTask.open(other, TASK_ID)
        with self.assertRaisesRegex(real.Blocked, "real_manual_import_disabled"):
            self.archive.propose(b"{}")
        self.send.assert_not_called()


if __name__ == "__main__":
    unittest.main()
