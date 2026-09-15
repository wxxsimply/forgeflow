"""Archive-to-receipt integration; synthetic input and Fake sandbox only."""

from concurrent.futures import ThreadPoolExecutor
import contextlib
import hashlib
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

import preview_demo_task as demo


TASK_ID = "1" * 32
TASK = "Synthetic offline task, not a model request.".encode()
PASSED = {"passed": True, "exitCode": 0, "oomKilled": False,
          "imageId": "sha256:" + "a" * 64, "logsCollected": False}


class TaskProposalTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "private"
        self.root.mkdir()
        self.source = Path(self.temp.name) / "source"
        shutil.copytree(demo.preflight.DEFAULT_SOURCE, self.source)
        self.captured = demo.preflight.snapshot(self.source)
        self.archive = demo.TaskArchive.create(self.root, TASK_ID, self.captured, TASK)
        self.value = {"schemaVersion": "forgeflow.demo.patch/v1", "taskId": TASK_ID,
                      "taskSha256": hashlib.sha256(TASK).hexdigest(),
                      "baseSnapshotSha256": demo.preflight.manifest(self.captured)["snapshotSha256"],
                      "files": {"greeting.go": self.captured["greeting.go"].decode() + "\n// Synthetic archived proposal.\n"}}
        self.raw = json.dumps(self.value, indent=2).encode() + b"\n"

    def stage(self):
        self.archive.propose(self.raw)
        return self.archive.proposal()[1]["approvalSha256"]

    def cli(self, action, *extra):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = demo.main([action, "--root", str(self.root), "--task-id", TASK_ID, *extra])
        return code, json.loads(output.getvalue()), output.getvalue()

    def sql(self, query, params=()):
        with contextlib.closing(sqlite3.connect(self.archive.database)) as conn:
            result = conn.execute(query, params).fetchall()
            conn.commit()
            return result

    def test_import_exact_bytes_and_receipt_share_budget_database(self):
        self.archive.budget().invoke_once(10, lambda: (3, "b" * 64))
        before = self.archive.summary()["budget"]
        self.stage()
        self.assertEqual(self.sql("SELECT raw FROM task_proposal"), [(self.raw,)])
        candidate, details, journal, metadata = self.archive.proposal()
        self.assertEqual(journal.path, self.archive.database)
        self.assertEqual(metadata["rawSha256"], hashlib.sha256(self.raw).hexdigest())
        self.assertEqual(metadata["origin"], "manual_import")
        self.assertEqual(candidate["greeting.go"], self.value["files"]["greeting.go"].encode())
        self.assertEqual(journal.summary()["state"], "reviewed")
        self.assertEqual(self.archive.summary()["budget"], before)
        self.assertEqual({p.name for p in self.archive.path.iterdir()}, {"task.sqlite"})

    def test_cli_import_review_test_and_reopen_without_external_input(self):
        proposal_file = Path(self.temp.name) / "proposal.json"
        proposal_file.write_bytes(self.raw)
        with patch.object(demo.preflight, "sandbox", side_effect=AssertionError("not approved")):
            self.assertEqual(self.cli("propose", "--proposal", str(proposal_file))[0], 0)
            proposal_file.unlink()
            shutil.rmtree(self.source)
            code, report, _ = self.cli("review")
        self.assertEqual(code, 0)
        self.assertIn("+// Synthetic archived proposal.", report["review"]["diff"])
        approval = report["review"]["approvalSha256"]
        with patch.object(demo.preflight, "sandbox", return_value=PASSED) as sandbox:
            code, result, text = self.cli("test", "--approve-sha256", approval)
        self.assertEqual(code, 0)
        self.assertTrue(result["sandboxAttempted"])
        self.assertTrue(result["sandboxExecuted"])
        self.assertEqual(sandbox.call_args.args[0]["greeting.go"], self.value["files"]["greeting.go"].encode())
        self.assertEqual(sandbox.call_args.args[1:], ("golang:1.22", 90))
        self.assertEqual(result["proposal"]["evidence"]["state"], "completed")
        self.assertEqual(result["modelCalls"], 0)
        self.assertEqual(result["budget"]["attempts"], 0)
        self.assertNotIn("Synthetic archived proposal", text)
        self.assertEqual(self.cli("inspect")[1]["proposal"]["evidence"]["result"], PASSED)

    def test_inspect_is_private_and_never_executes(self):
        self.stage()
        with patch.object(demo.preflight, "sandbox", side_effect=AssertionError("no execution")):
            code, report, text = self.cli("inspect")
        self.assertEqual(code, 0)
        self.assertFalse(report["sandboxAttempted"])
        for sensitive in ("func Greet", "Synthetic archived proposal", TASK.decode(), str(self.root), '"diff"'):
            self.assertNotIn(sensitive, text)

    def test_old_archive_without_proposal_remains_inspectable(self):
        self.assertIsNone(self.archive.summary()["proposal"])
        self.assertEqual(self.cli("inspect")[0], 0)
        for action, extra in (("review", ()), ("test", ("--approve-sha256", "0" * 64))):
            with patch.object(demo.preflight, "sandbox") as sandbox:
                code, report, _ = self.cli(action, *extra)
                self.assertEqual(code, 1)
                self.assertEqual(report["reason"], "task_proposal_missing")
                sandbox.assert_not_called()

    def test_duplicate_import_cannot_replace_or_reset_consumed_receipt(self):
        approval = self.stage()
        self.archive.proposal()[2].run_once(approval, "golang:1.22", 90, lambda: PASSED)
        for raw in (self.raw, json.dumps(self.value).encode()):
            with self.assertRaisesRegex(demo.Blocked, "task_proposal_already_exists"):
                self.archive.propose(raw)
        self.assertEqual(self.sql("SELECT raw FROM task_proposal"), [(self.raw,)])
        self.assertEqual(self.archive.summary()["proposal"]["evidence"]["state"], "completed")

    def test_concurrent_import_has_only_one_winner(self):
        barrier = threading.Barrier(2)
        def stage():
            barrier.wait(timeout=5)
            try:
                demo.TaskArchive(self.root, TASK_ID).propose(self.raw)
                return "created"
            except demo.Blocked as error:
                return str(error)
        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(lambda _: stage(), range(2)))
        self.assertCountEqual(results, ["created", "task_proposal_already_exists"])
        self.assertEqual(self.archive.summary()["proposal"]["evidence"]["state"], "reviewed")

    def test_initialization_failure_rolls_back_both_new_tables(self):
        original = demo.initialize_receipt
        def fail(conn, details):
            original(conn, details)
            raise sqlite3.OperationalError("synthetic failure after both inserts")
        with patch.object(demo, "initialize_receipt", side_effect=fail):
            with self.assertRaisesRegex(demo.Blocked, "task_archive_unavailable"):
                self.stage()
        self.assertEqual(self.sql("SELECT name FROM sqlite_master WHERE name IN ('receipt','task_proposal')"), [])
        self.assertIsNone(self.archive.summary()["proposal"])
        self.stage()  # Failed import had no execution or partially committed proposal.

    def test_commit_failure_rolls_back_import_without_resetting_budget(self):
        self.archive.budget().reserve(10)
        class FailedCommit(sqlite3.Connection):
            def commit(self):
                if self.execute("SELECT name FROM sqlite_master WHERE name='task_proposal'").fetchone():
                    raise sqlite3.OperationalError("synthetic commit failure")
                return super().commit()
        connect = sqlite3.connect
        with patch.object(demo.sqlite3, "connect", side_effect=lambda *a, **k: connect(*a, **k, factory=FailedCommit)):
            with self.assertRaisesRegex(demo.Blocked, "task_archive_unavailable"):
                self.stage()
        summary = self.archive.summary()
        self.assertIsNone(summary["proposal"])
        self.assertTrue(summary["budget"]["unfinished"])
        self.assertEqual(summary["budget"]["attempts"], 1)

    def test_invalid_binding_and_unsafe_files_leave_no_proposal(self):
        for key, value in (("taskId", "2" * 32), ("taskSha256", "0" * 64),
                           ("baseSnapshotSha256", "0" * 64), ("files", {".env": "not a credential"})):
            with self.subTest(key=key):
                with self.assertRaises(demo.Blocked):
                    self.archive.propose(json.dumps({**self.value, key: value}).encode())
                self.assertIsNone(self.archive.summary()["proposal"])

    def test_invalid_or_oversized_raw_is_rejected(self):
        for raw in (b"", b"invalid JSON", b" " * (demo.MAX_PROPOSAL_BYTES + 1)):
            with self.assertRaises(demo.Blocked):
                self.archive.propose(raw)
        self.assertIsNone(self.archive.summary()["proposal"])

    def test_raw_corruption_and_binding_replacement_are_blocked(self):
        self.stage()
        self.sql("UPDATE task_proposal SET raw=?", (self.raw + b" ",))
        self.assertEqual(self.cli("inspect")[1]["reason"], "archived_proposal_mismatch")
        changed = {**self.value, "files": {"greeting.go": self.value["files"]["greeting.go"] + "// changed\n"}}
        raw = json.dumps(changed).encode()
        self.sql("UPDATE task_proposal SET raw=?,sha256=?", (raw, hashlib.sha256(raw).hexdigest()))
        with patch.object(demo.preflight, "sandbox") as sandbox:
            self.assertEqual(self.cli("test", "--approve-sha256", "0" * 64)[1]["reason"], "evidence_binding_mismatch")
            sandbox.assert_not_called()

    def test_missing_either_table_fails_closed(self):
        self.stage()
        for table in ("task_proposal", "receipt"):
            # Roll back deliberate damage after checking each scenario.
            with contextlib.closing(sqlite3.connect(self.archive.database)) as conn:
                conn.execute("BEGIN")
                conn.execute("DROP TABLE " + table)
                with self.assertRaisesRegex(demo.Blocked, "incomplete_task_proposal"):
                    demo.TaskArchive._has_proposal(conn)
                conn.rollback()
        self.sql("DROP TABLE receipt")
        self.assertEqual(self.cli("inspect")[1]["reason"], "incomplete_task_proposal")
        with self.assertRaisesRegex(demo.Blocked, "incomplete_task_proposal"):
            self.archive.propose(self.raw)

    def test_wrong_approval_does_not_consume_receipt(self):
        self.stage()
        with patch.object(demo.preflight, "sandbox") as sandbox:
            code, report, _ = self.cli("test", "--approve-sha256", "0" * 64)
            sandbox.assert_not_called()
        self.assertEqual(code, 1)
        self.assertEqual(report["reason"], "approval_mismatch")
        self.assertEqual(report["proposal"]["evidence"]["state"], "reviewed")

    def test_missing_approval_and_external_evidence_path_are_not_cli_options(self):
        self.stage()
        for extra in ([], ["--approve-sha256", "0" * 64, "--evidence-db", "elsewhere.sqlite"]):
            with contextlib.redirect_stderr(io.StringIO()), patch.object(demo.preflight, "sandbox") as sandbox:
                with self.assertRaises(SystemExit):
                    self.cli("test", *extra)
                sandbox.assert_not_called()

    def test_failed_test_records_failure_and_blocks_repetition(self):
        approval = self.stage()
        failed = {**PASSED, "passed": False, "exitCode": 1}
        with patch.object(demo.preflight, "sandbox", return_value=failed) as sandbox:
            code, report, _ = self.cli("test", "--approve-sha256", approval)
            self.assertEqual(code, 1)
            self.assertEqual(report["proposal"]["evidence"]["result"], failed)
            self.assertEqual(self.cli("test", "--approve-sha256", approval)[1]["reason"], "evidence_already_consumed")
            self.assertEqual(sandbox.call_count, 1)

    def test_cleanup_error_is_reported_without_raw_exception_or_retry(self):
        approval = self.stage()
        with patch.object(demo.preflight, "sandbox", side_effect=demo.Blocked("cleanup_unconfirmed", "owned-demo-container")):
            code, report, _ = self.cli("test", "--approve-sha256", approval)
        self.assertEqual(code, 1)
        self.assertTrue(report["sandboxAttempted"])
        self.assertFalse(report["sandboxExecuted"])
        self.assertEqual(report["cleanupContainerName"], "owned-demo-container")
        self.assertEqual(report["proposal"]["evidence"]["state"], "unknown")
        self.assertEqual(self.cli("test", "--approve-sha256", approval)[1]["reason"], "evidence_already_consumed")

    def test_interruption_preserves_unknown_state(self):
        approval = self.stage()
        with patch.object(demo.preflight, "sandbox", side_effect=KeyboardInterrupt):
            code, report, _ = self.cli("test", "--approve-sha256", approval)
        self.assertEqual(code, 1)
        self.assertEqual(report["reason"], "interrupted")
        self.assertEqual(report["proposal"]["evidence"]["state"], "unknown")

    def test_execution_result_write_failure_does_not_claim_success(self):
        approval = self.stage()
        with patch.object(demo.preflight, "sandbox", return_value=PASSED), \
                patch.object(demo.EvidenceJournal, "_finish", side_effect=demo.Blocked("evidence_unavailable")):
            code, report, _ = self.cli("test", "--approve-sha256", approval)
        self.assertEqual(code, 1)
        self.assertTrue(report["sandboxExecuted"])
        self.assertEqual(report["proposal"]["evidence"]["state"], "running")
        self.assertEqual(self.cli("test", "--approve-sha256", approval)[1]["reason"], "evidence_already_consumed")

    def test_process_death_after_begin_stays_consumed_on_restart(self):
        approval = self.stage()
        code = "import os,sys; import preview_demo_task as d; a=d.TaskArchive.open(sys.argv[1],sys.argv[2]); a.proposal()[2].run_once(sys.argv[3],'golang:1.22',90,lambda:os._exit(0))"
        result = subprocess.run([sys.executable, "-B", "-c", code, str(self.root), TASK_ID, approval],
                                cwd=Path(__file__).parent, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(self.cli("inspect")[1]["proposal"]["evidence"]["state"], "running")
        with patch.object(demo.preflight, "sandbox") as sandbox:
            self.assertEqual(self.cli("test", "--approve-sha256", approval)[0], 1)
            sandbox.assert_not_called()

    def test_concurrent_test_consumes_same_database_once(self):
        approval = self.stage()
        barrier = threading.Barrier(2)
        calls = []
        def execute():
            journal = demo.TaskArchive.open(self.root, TASK_ID).proposal()[2]
            barrier.wait(timeout=5)
            try:
                journal.run_once(approval, "golang:1.22", 90, lambda: (calls.append(1), PASSED)[1])
                return "completed"
            except demo.Blocked as error:
                return str(error)
        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(lambda _: execute(), range(2)))
        self.assertCountEqual(results, ["completed", "evidence_already_consumed"])
        self.assertEqual(calls, [1])


if __name__ == "__main__":
    unittest.main()
