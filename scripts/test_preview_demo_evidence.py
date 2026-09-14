"""Offline durable approval/receipt tests; never invoke Docker or a model."""

from concurrent.futures import ThreadPoolExecutor
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import Mock, patch

import preview_demo_evidence as evidence
import preview_demo_patch as demo


PASSED = {"passed": True, "exitCode": 0, "oomKilled": False,
          "imageId": "sha256:" + "a" * 64, "logsCollected": False}
IMAGE = "golang:1.22-alpine"


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "review.sqlite"
        captured = demo.preflight.snapshot(demo.preflight.DEFAULT_SOURCE)
        self.proposal = {"schemaVersion": demo.SCHEMA, "taskId": "1" * 32, "taskSha256": "2" * 64,
                         "baseSnapshotSha256": demo.preflight.manifest(captured)["snapshotSha256"],
                         "files": {"greeting.go": captured["greeting.go"].decode() + "\n// Evidence Fake.\n"}}
        self.raw = json.dumps(self.proposal).encode()
        _, self.details = demo.review(captured, self.raw, "1" * 32, "2" * 64)
        self.approval = self.details["approvalSha256"]
        self.journal = evidence.EvidenceJournal.create(self.path, self.details)

    def open(self):
        return evidence.EvidenceJournal.open(self.path, self.details)

    def execute(self, runner=lambda: PASSED):
        return self.open().run_once(self.approval, IMAGE, 90, runner)

    def test_persisted_review_excludes_raw_source_diff_and_paths(self):
        summary = self.open().summary()
        self.assertEqual(summary["state"], "reviewed")
        self.assertEqual(summary["binding"]["approvalSha256"], self.approval)
        self.assertIsNone(summary["startedAt"])
        raw = self.path.read_bytes()
        for forbidden in (b"Evidence Fake", b"func Greet", b"diff", str(self.path).encode()):
            self.assertNotIn(forbidden, raw)

    def test_approval_committed_before_callback_and_result_survives_restart(self):
        def run():
            saved = self.open().summary()
            self.assertEqual(saved["state"], "running")
            self.assertEqual(saved["requestedImage"], IMAGE)
            self.assertEqual(saved["timeoutSeconds"], 90)
            return PASSED
        self.assertEqual(self.execute(run), PASSED)
        saved = self.open().summary()
        self.assertEqual(saved["state"], "completed")
        self.assertEqual(saved["result"], PASSED)
        self.assertIsNotNone(saved["finishedAt"])
        runner = Mock()
        with self.assertRaisesRegex(demo.Blocked, "evidence_already_consumed"):
            self.execute(runner)
        runner.assert_not_called()

    def test_nonzero_result_is_saved_not_success(self):
        failed = {**PASSED, "passed": False, "exitCode": 1}
        self.assertEqual(self.execute(lambda: failed), failed)
        self.assertFalse(self.open().summary()["result"]["passed"])

    def test_exception_is_unknown_without_raw_error_or_retry(self):
        runner = Mock(side_effect=demo.Blocked("secret-message-must-not-be-stored"))
        with self.assertRaises(demo.Blocked):
            self.execute(runner)
        self.assertEqual(self.open().summary()["state"], "unknown")
        self.assertNotIn(b"secret-message", self.path.read_bytes())
        with self.assertRaisesRegex(demo.Blocked, "evidence_already_consumed"):
            self.execute(runner)
        self.assertEqual(runner.call_count, 1)

    def test_interrupt_records_unknown(self):
        with self.assertRaises(KeyboardInterrupt):
            self.execute(Mock(side_effect=KeyboardInterrupt()))
        self.assertEqual(self.open().summary()["state"], "unknown")

    def test_abrupt_process_exit_leaves_running_and_blocks_retry(self):
        code = "import json,os,sys; from preview_demo_evidence import EvidenceJournal; j=EvidenceJournal.open(sys.argv[1],json.loads(sys.argv[2])); j.run_once(j.binding['approvalSha256'],'golang:1.22-alpine',90,lambda: os._exit(0))"
        result = subprocess.run([sys.executable, "-B", "-c", code, str(self.path), json.dumps(self.details)],
                                cwd=Path(__file__).parent, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0, "abrupt-exit child failed")
        self.assertEqual(self.open().summary()["state"], "running")
        with self.assertRaisesRegex(demo.Blocked, "evidence_already_consumed"):
            self.execute(Mock(side_effect=AssertionError("must not run")))

    def test_concurrent_process_connections_only_one_begin(self):
        barrier = threading.Barrier(2)
        def attempt():
            journal = self.open()
            barrier.wait(timeout=5)
            try:
                journal.begin(self.approval, IMAGE, 90)
                return "began"
            except demo.Blocked as error:
                return str(error)
        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(lambda _: attempt(), range(2)))
        self.assertCountEqual(results, ["began", "evidence_already_consumed"])

    def test_wrong_approval_and_config_do_not_consume(self):
        for approval, image, seconds, reason in (("0" * 64, IMAGE, 90, "approval_mismatch"),
                                                (self.approval, "bad image", 90, "invalid_execution_config"),
                                                (self.approval, IMAGE, True, "invalid_execution_config"),
                                                (self.approval, IMAGE, 121, "invalid_execution_config")):
            with self.assertRaisesRegex(demo.Blocked, reason):
                self.journal.begin(approval, image, seconds)
        self.assertEqual(self.open().summary()["state"], "reviewed")

    def test_binding_drift_overwrite_missing_and_corruption_block(self):
        changed = {key: self.details[key] for key in evidence.FIELDS}
        changed["taskId"] = "3" * 32
        changed["approvalSha256"] = hashlib.sha256(evidence.encode(changed).encode()).hexdigest()
        with self.assertRaisesRegex(demo.Blocked, "evidence_binding_mismatch"):
            evidence.EvidenceJournal.open(self.path, changed)
        with self.assertRaisesRegex(demo.Blocked, "evidence_already_exists"):
            evidence.EvidenceJournal.create(self.path, self.details)
        missing = self.path.with_name("missing.sqlite")
        with self.assertRaises(demo.Blocked):
            evidence.EvidenceJournal.open(missing, self.details)
        self.assertFalse(missing.exists())
        corrupt = self.path.with_name("corrupt.sqlite")
        corrupt.write_bytes(b"not-a-database")
        with self.assertRaisesRegex(demo.Blocked, "evidence_unavailable"):
            evidence.EvidenceJournal.open(corrupt, self.details)
        self.assertEqual(corrupt.read_bytes(), b"not-a-database")

    def test_failed_initialization_keeps_file_and_never_overwrites(self):
        path = self.path.with_name("failed.sqlite")
        with patch.object(evidence.sqlite3, "connect", side_effect=sqlite3.OperationalError("private-path")):
            with self.assertRaisesRegex(demo.Blocked, "^evidence_unavailable$"):
                evidence.EvidenceJournal.create(path, self.details)
        self.assertTrue(path.exists())
        with self.assertRaisesRegex(demo.Blocked, "evidence_already_exists"):
            evidence.EvidenceJournal.create(path, self.details)

    def test_reservation_commit_failure_never_calls_runner(self):
        class FailedCommit(sqlite3.Connection):
            def commit(self):
                raise sqlite3.OperationalError("private failure")
        connect = sqlite3.connect
        runner = Mock()
        with patch.object(evidence.sqlite3, "connect", side_effect=lambda *a, **k: connect(*a, **k, factory=FailedCommit)):
            with self.assertRaisesRegex(demo.Blocked, "evidence_unavailable"):
                self.journal.run_once(self.approval, IMAGE, 90, runner)
        runner.assert_not_called()
        self.assertEqual(self.open().summary()["state"], "reviewed")

    def test_result_write_failure_keeps_running(self):
        with patch.object(self.journal, "_finish", side_effect=demo.Blocked("evidence_unavailable")):
            with self.assertRaisesRegex(demo.Blocked, "evidence_unavailable"):
                self.journal.run_once(self.approval, IMAGE, 90, lambda: PASSED)
        self.assertEqual(self.open().summary()["state"], "running")

    def test_invalid_results_become_unknown(self):
        variants = [None, {}, {**PASSED, "exitCode": True}, {**PASSED, "passed": False},
                    {**PASSED, "stdout": "raw secret"}, {**PASSED, "imageId": "tag-only"},
                    {**PASSED, "logsCollected": True}]
        for index, value in enumerate(variants):
            journal = evidence.EvidenceJournal.create(self.path.with_name(f"bad-{index}.sqlite"), self.details)
            with self.assertRaisesRegex(demo.Blocked, "invalid_execution_result"):
                journal.run_once(self.approval, IMAGE, 90, lambda: value)
            self.assertEqual(journal.summary()["state"], "unknown")

    def test_hardlink_traversal_and_reparse_paths_rejected(self):
        alias = self.path.with_name("alias.sqlite")
        os.link(self.path, alias)
        with self.assertRaisesRegex(demo.Blocked, "evidence_path_unavailable"):
            evidence.EvidenceJournal.open(alias, self.details)
        with self.assertRaisesRegex(demo.Blocked, "unsafe_evidence_path"):
            evidence.EvidenceJournal.open(self.path.parent / ".." / self.path.name, self.details)
        with patch.object(evidence, "check_path", side_effect=evidence.BudgetBlocked("unsafe_ledger_path")):
            with self.assertRaisesRegex(demo.Blocked, "evidence_path_unavailable"):
                self.open()

    def cli(self, *extra):
        proposal = self.path.with_name("proposal.json")
        proposal.write_bytes(self.raw)
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = demo.main(["--proposal", str(proposal), "--task-id", "1" * 32, "--task-sha256", "2" * 64, *extra])
        return code, json.loads(output.getvalue())

    def test_cli_requires_saved_receipt_and_cannot_reuse_it(self):
        with patch.object(demo.preflight, "sandbox", return_value=PASSED) as runner:
            code, report = self.cli("--sandbox", "--approve-sha256", self.approval)
            self.assertEqual(report["reason"], "existing_review_receipt_required")
            self.assertEqual(code, 1)
            runner.assert_not_called()
            args = ("--sandbox", "--approve-sha256", self.approval, "--evidence-db", str(self.path))
            code, report = self.cli(*args)
            self.assertEqual(code, 0)
            self.assertEqual(report["evidence"]["state"], "completed")
            code, report = self.cli(*args)
            self.assertEqual(code, 1)
            self.assertEqual(report["reason"], "evidence_already_consumed")
            self.assertFalse(report["sandboxAttempted"])
            self.assertEqual(runner.call_count, 1)

    def test_cli_explicit_review_record_and_status_do_not_execute(self):
        path = self.path.with_name("explicit.sqlite")
        with patch.object(demo.preflight, "sandbox", side_effect=AssertionError("no execution")):
            code, report = self.cli("--record-review", str(path))
            self.assertEqual(code, 0)
            self.assertEqual(report["evidence"]["state"], "reviewed")
            self.assertIn("diff", report)
            self.assertEqual(self.cli("--record-review", str(path))[1]["reason"], "evidence_already_exists")
            self.assertEqual(self.cli("--evidence-db", str(path))[1]["evidence"]["state"], "reviewed")

    def test_cli_refuses_receipt_inside_source_without_creating_it(self):
        path = demo.preflight.DEFAULT_SOURCE / "must-not-be-created.sqlite"
        self.assertFalse(path.exists())
        code, report = self.cli("--record-review", str(path))
        self.assertEqual(code, 1)
        self.assertEqual(report["reason"], "evidence_inside_source")
        self.assertFalse(path.exists())

    def test_cli_result_storage_failure_is_not_a_success(self):
        with patch.object(demo.preflight, "sandbox", return_value=PASSED), \
                patch.object(evidence.EvidenceJournal, "_finish", side_effect=demo.Blocked("evidence_unavailable")):
            code, report = self.cli("--sandbox", "--approve-sha256", self.approval, "--evidence-db", str(self.path))
        self.assertEqual(code, 1)
        self.assertFalse(report["checksPassed"])
        self.assertTrue(report["sandboxExecuted"])
        self.assertEqual(report["evidence"]["state"], "running")


if __name__ == "__main__":
    unittest.main()
