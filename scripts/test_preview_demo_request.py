"""Fixed offline Fake request lifecycle; no network, secrets or Docker."""

from concurrent.futures import ThreadPoolExecutor
import contextlib
import io
import json
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

import preview_demo_request as request
import preview_demo_task as task


TASK_ID = "3" * 32
TASK = "合成离线任务：不是付费请求。".encode()
MAXIMUM = 1000
PASSED = {"passed": True, "exitCode": 0, "oomKilled": False,
          "imageId": "sha256:" + "a" * 64, "logsCollected": False}


class RequestFlowTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "private"
        self.root.mkdir()
        self.captured = task.preflight.snapshot(task.preflight.DEFAULT_SOURCE)
        self.archive = task.TaskArchive.create(self.root, TASK_ID, self.captured, TASK, max_calls=3)
        self.flow = request.RequestFlow(self.archive)

    def approval(self, maximum=MAXIMUM):
        return self.flow.preview(maximum)[1]["authorizationSha256"]

    def run_fake(self):
        return self.flow.run_fake(MAXIMUM, self.approval())

    def cli(self, action, *extra):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = request.main([action, "--root", str(self.root), "--task-id", TASK_ID, *extra])
        return code, json.loads(output.getvalue()), output.getvalue()

    def sql(self, query, params=()):
        with contextlib.closing(sqlite3.connect(self.archive.database)) as conn:
            result = conn.execute(query, params).fetchall()
            conn.commit()
            return result

    def test_preview_exact_whitelist_and_no_side_effects(self):
        with patch.object(request, "fake_send_once", side_effect=AssertionError("preview cannot send")):
            payload, descriptor = self.flow.preview(MAXIMUM)
            code, report, text = self.cli("preview")
        value = json.loads(payload)
        self.assertEqual(set(value), {"schemaVersion", "taskId", "taskSha256", "baseSnapshotSha256", "contract", "task", "files"})
        self.assertEqual(value["task"].encode(), TASK)
        self.assertEqual({name: data.encode() for name, data in value["files"].items()}, self.captured)
        self.assertEqual(descriptor["payloadSha256"], request.digest(payload))
        self.assertEqual(descriptor["maximumNanoUsd"], MAXIMUM)
        self.assertEqual(code, 0)
        self.assertEqual(report["modelCalls"], 0)
        for sensitive in (TASK.decode(), "func Greet", str(self.root), '"payload"'):
            self.assertNotIn(sensitive, text)
        self.assertIsNone(self.archive.summary()["request"])
        self.assertEqual(self.archive.budget().summary()["attempts"], 0)
        code, report, _ = self.cli("preview", "--show-payload")
        self.assertEqual(code, 0)
        self.assertEqual(report["payload"], value)

    def test_wrong_or_changed_reservation_approval_does_not_send(self):
        approval = self.approval()
        for maximum, supplied in ((MAXIMUM, "0" * 64), (MAXIMUM + 1, approval)):
            with patch.object(request, "fake_send_once") as send:
                with self.assertRaisesRegex(task.Blocked, "request_approval_mismatch"):
                    self.flow.run_fake(maximum, supplied)
                send.assert_not_called()
        self.assertEqual(self.archive.budget().summary()["attempts"], 0)
        self.assertIsNone(self.flow.summary())

    def test_changed_contract_requires_new_approval(self):
        approval = self.approval()
        with patch.object(request, "CONTRACT", request.CONTRACT + " Changed contract."), patch.object(request, "fake_send_once") as send:
            with self.assertRaisesRegex(task.Blocked, "request_approval_mismatch"):
                self.flow.run_fake(MAXIMUM, approval)
            send.assert_not_called()

    def test_request_budget_and_payload_commit_before_send(self):
        original = request.fake_send_once
        seen = []
        def send(payload):
            reopened = task.TaskArchive.open(self.root, TASK_ID)
            summary = reopened.summary()
            self.assertEqual(summary["request"]["state"], "reserved")
            self.assertEqual(summary["budget"]["attempts"], 1)
            self.assertEqual(summary["budget"]["held_nano_usd"], MAXIMUM)
            self.assertIsNone(summary["proposal"])
            self.assertEqual(self.sql("SELECT payload FROM task_request"), [(payload,)])
            seen.append(1)
            return original(payload)
        with patch.object(request, "fake_send_once", side_effect=send):
            result = self.run_fake()
        self.assertEqual(seen, [1])
        self.assertEqual(result["state"], "completed")
        summary = self.archive.summary()
        self.assertEqual(summary["proposal"]["origin"], "fake_request")
        self.assertEqual(summary["proposal"]["evidence"]["state"], "reviewed")
        self.assertEqual(summary["budget"]["spent_nano_usd"], 0)
        self.assertEqual(summary["budget"]["held_nano_usd"], 0)
        self.assertEqual(summary["request"]["responseSha256"], summary["proposal"]["rawSha256"])
        raw, = self.sql("SELECT response FROM task_request")[0]
        self.assertEqual(self.sql("SELECT raw FROM task_proposal"), [(raw,)])

    def test_cli_response_still_needs_separate_sandbox_approval(self):
        with patch.object(task.preflight, "sandbox", side_effect=AssertionError("request cannot execute")):
            code, report, text = self.cli("run-fake", "--approve-request-sha256", self.approval())
        self.assertEqual(code, 0)
        self.assertFalse(report["sandboxExecuted"])
        self.assertFalse(report["paidExecutionEnabled"])
        self.assertNotIn("Offline Fake request lifecycle only.", text)
        self.assertEqual(report["request"]["state"], "completed")
        candidate, details, journal, _ = self.archive.proposal()
        with self.assertRaisesRegex(task.Blocked, "approval_mismatch"):
            journal.run_once(self.approval(), "golang:1.22", 90, lambda: self.fail("wrong approval"))
        journal.run_once(details["approvalSha256"], "golang:1.22", 90, lambda: PASSED)
        reopened = task.TaskArchive.open(self.root, TASK_ID).summary()
        self.assertEqual(reopened["proposal"]["evidence"]["result"], PASSED)
        self.assertIn(b"Offline Fake request", candidate["greeting.go"])
        self.assertEqual(self.cli("inspect")[0], 0)

    def test_duplicate_requests_never_retry_even_with_remaining_call_budget(self):
        self.run_fake()
        with patch.object(request, "fake_send_once") as send:
            with self.assertRaisesRegex(task.Blocked, "task_request_already_exists"):
                self.run_fake()
            send.assert_not_called()
        self.assertEqual(self.archive.budget().summary()["attempts"], 1)

    def test_manual_proposal_and_request_are_mutually_exclusive(self):
        payload, _ = self.flow.preview(MAXIMUM)
        raw, _ = request.fake_send_once(payload)
        self.archive.propose(raw)
        with patch.object(request, "fake_send_once") as send:
            with self.assertRaisesRegex(task.Blocked, "task_proposal_already_exists"):
                self.run_fake()
            send.assert_not_called()
        self.assertIsNone(self.flow.summary())
        self.assertEqual(self.archive.budget().summary()["attempts"], 0)

    def test_pending_request_prevents_manual_import(self):
        _, payload = self.flow._begin(MAXIMUM, self.approval())
        raw, _ = request.fake_send_once(payload)
        with self.assertRaisesRegex(task.Blocked, "task_request_already_exists"):
            self.archive.propose(raw)
        self.assertIsNone(self.archive.summary()["proposal"])

    def test_budget_limits_and_unresolved_usage_block_before_send(self):
        with patch.object(request, "fake_send_once") as send:
            for maximum in (0, -1, True, 1_000_001):
                with self.assertRaises(task.Blocked):
                    self.flow.run_fake(maximum, "0" * 64)
            self.archive.budget().reserve(10)
            with self.assertRaisesRegex(task.Blocked, "request_unresolved_attempt"):
                self.run_fake()
            send.assert_not_called()
        self.assertIsNone(self.flow.summary())
        self.assertEqual(self.archive.budget().summary()["attempts"], 1)

    def test_call_limit_remains_enforced_in_shared_ledger(self):
        for _ in range(3):
            self.archive.budget().invoke_once(1, lambda: (0, "a" * 64))
        with patch.object(request, "fake_send_once") as send:
            with self.assertRaisesRegex(task.Blocked, "request_call_limit"):
                self.run_fake()
            send.assert_not_called()

    def test_budget_and_request_begin_commit_failure_sends_nothing(self):
        class FailedCommit(sqlite3.Connection):
            def commit(self):
                if self.execute("SELECT name FROM sqlite_master WHERE name='task_request'").fetchone():
                    raise sqlite3.OperationalError("synthetic begin failure")
                return super().commit()
        connect = sqlite3.connect
        with patch.object(request.sqlite3, "connect", side_effect=lambda *a, **k: connect(*a, **k, factory=FailedCommit)), \
                patch.object(request, "fake_send_once") as send:
            with self.assertRaisesRegex(task.Blocked, "request_ledger_unavailable"):
                self.run_fake()
            send.assert_not_called()
        self.assertIsNone(self.flow.summary())
        self.assertEqual(self.archive.budget().summary()["attempts"], 0)

    def test_response_commit_failure_rolls_back_proposal_receipt_and_settlement(self):
        class FailedCommit(sqlite3.Connection):
            def commit(self):
                if self.execute("SELECT name FROM sqlite_master WHERE name='task_request'").fetchone():
                    if self.execute("SELECT state FROM task_request").fetchone() == ("completed",):
                        raise sqlite3.OperationalError("synthetic response failure")
                return super().commit()
        connect = sqlite3.connect
        original = request.fake_send_once
        with patch.object(request.sqlite3, "connect", side_effect=lambda *a, **k: connect(*a, **k, factory=FailedCommit)), \
                patch.object(request, "fake_send_once", wraps=original) as send:
            with self.assertRaisesRegex(task.Blocked, "fake_request_outcome_unknown"):
                self.run_fake()
            self.assertEqual(send.call_count, 1)
        summary = self.archive.summary()
        self.assertEqual(summary["request"]["state"], "unknown")
        self.assertEqual(summary["budget"]["held_nano_usd"], MAXIMUM)
        self.assertEqual(self.sql("SELECT name FROM sqlite_master WHERE name IN ('receipt','task_proposal')"), [])
        self.assertEqual(self.sql("SELECT response FROM task_request"), [(None,)])

    def test_invalid_response_blocks_without_importing_anything(self):
        with patch.object(request, "fake_send_once", return_value=(b'{"command":"not allowed"}', 0)):
            code, report, text = self.cli("run-fake", "--approve-request-sha256", self.approval())
        self.assertEqual(code, 1)
        self.assertEqual(report["reason"], "fake_request_outcome_unknown")
        self.assertEqual(report["request"]["state"], "unknown")
        self.assertIsNone(report["proposal"])
        self.assertNotIn("not allowed", text)

    def test_response_bounds_and_invalid_fake_usage_stay_unknown(self):
        for result in ((b"x" * (request.MAX_PROPOSAL_BYTES + 1), 0), (b"{}", False), (b"{}", 1), None):
            with self.subTest(result_type=type(result).__name__):
                # Independent synthetic task per case; never reset a live ledger.
                task_id = f"{len(list(self.root.iterdir())) + 10:032x}"
                archive = task.TaskArchive.create(self.root, task_id, self.captured, TASK)
                flow = request.RequestFlow(archive)
                approval = flow.preview(MAXIMUM)[1]["authorizationSha256"]
                with patch.object(request, "fake_send_once", return_value=result):
                    with self.assertRaisesRegex(task.Blocked, "fake_request_outcome_unknown"):
                        flow.run_fake(MAXIMUM, approval)
                self.assertEqual(flow.summary()["state"], "unknown")
                self.assertEqual(archive.budget().summary()["held_nano_usd"], MAXIMUM)

    def test_raw_provider_exception_is_not_printed(self):
        with patch.object(request, "fake_send_once", side_effect=RuntimeError("synthetic-private-error")):
            code, report, text = self.cli("run-fake", "--approve-request-sha256", self.approval())
        self.assertEqual(code, 1)
        self.assertNotIn("synthetic-private-error", text)
        self.assertEqual(report["request"]["state"], "unknown")

    def test_unknown_write_failure_keeps_durable_reservation(self):
        with patch.object(request, "fake_send_once", side_effect=TimeoutError), \
                patch.object(request.RequestFlow, "_unknown", side_effect=task.Blocked("request_store_unavailable")):
            with self.assertRaisesRegex(task.Blocked, "fake_request_outcome_unknown"):
                self.run_fake()
        self.assertEqual(self.flow.summary()["state"], "reserved")
        self.assertEqual(self.archive.budget().summary()["held_nano_usd"], MAXIMUM)
        with patch.object(request, "fake_send_once") as send:
            with self.assertRaisesRegex(task.Blocked, "task_request_already_exists"):
                self.run_fake()
            send.assert_not_called()

    def test_keyboard_interrupt_keeps_unknown_without_retry(self):
        with patch.object(request, "fake_send_once", side_effect=KeyboardInterrupt):
            code, report, _ = self.cli("run-fake", "--approve-request-sha256", self.approval())
        self.assertEqual(code, 1)
        self.assertEqual(report["reason"], "interrupted")
        self.assertEqual(report["request"]["state"], "unknown")

    def test_process_death_after_begin_keeps_request_and_reservation(self):
        code = "import os,sys; import preview_demo_task as t, preview_demo_request as r; a=t.TaskArchive.open(sys.argv[1],sys.argv[2]); f=r.RequestFlow(a); r.fake_send_once=lambda _:os._exit(0); f.run_fake(1000,f.preview(1000)[1]['authorizationSha256'])"
        result = subprocess.run([sys.executable, "-B", "-c", code, str(self.root), TASK_ID],
                                cwd=Path(__file__).parent, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(self.archive.summary()["request"]["state"], "reserved")
        with patch.object(request, "fake_send_once") as send:
            self.assertEqual(self.cli("run-fake", "--approve-request-sha256", self.approval())[0], 1)
            send.assert_not_called()

    def test_concurrent_requests_only_call_once(self):
        approval = self.approval()
        barrier = threading.Barrier(2)
        def run():
            barrier.wait(timeout=5)
            try:
                request.RequestFlow(task.TaskArchive(self.root, TASK_ID)).run_fake(MAXIMUM, approval)
                return "completed"
            except task.Blocked as error:
                return str(error)
        with patch.object(request, "fake_send_once", wraps=request.fake_send_once) as send:
            with ThreadPoolExecutor(max_workers=2) as pool:
                results = list(pool.map(lambda _: run(), range(2)))
            self.assertEqual(send.call_count, 1)
        self.assertCountEqual(results, ["completed", "task_request_already_exists"])
        self.assertEqual(self.archive.budget().summary()["attempts"], 1)

    def test_changed_payload_and_response_are_detected(self):
        self.run_fake()
        payload = self.sql("SELECT payload FROM task_request")[0][0]
        self.sql("UPDATE task_request SET payload=?", (payload + b" ",))
        self.assertEqual(self.cli("inspect")[1]["reason"], "request_binding_mismatch")
        self.sql("UPDATE task_request SET payload=?", (payload,))
        self.sql("UPDATE task_request SET response=?", (b"{}",))
        self.assertEqual(self.cli("inspect")[1]["reason"], "request_result_mismatch")

    def test_missing_request_or_budget_link_blocks_fake_proposal(self):
        self.run_fake()
        token = self.flow.summary()["attemptId"]
        self.sql("UPDATE task_request SET attempt_id=?", ("0" * 32,))
        self.assertEqual(self.cli("inspect")[1]["reason"], "request_attempt_mismatch")
        self.sql("UPDATE task_request SET attempt_id=?", (token,))
        self.sql("DROP TABLE task_request")
        # A Fake proposal must not become an untracked manual proposal.
        with self.assertRaises(task.Blocked):
            self.archive.proposal()

    def test_moved_archive_is_rejected_and_cli_has_no_paid_options(self):
        self.run_fake()
        other = Path(self.temp.name) / "other"
        other.mkdir()
        shutil.copytree(self.archive.path, other / TASK_ID)
        with self.assertRaisesRegex(task.Blocked, "task_binding_mismatch"):
            task.TaskArchive.open(other, TASK_ID)
        with contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit):
                self.cli("run-fake", "--approve-request-sha256", self.approval(), "--provider", "deepseek")


if __name__ == "__main__":
    unittest.main()
