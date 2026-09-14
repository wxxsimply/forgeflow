"""Offline patch guards: Fake sandbox only, no candidate code execution."""

import contextlib
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import tempfile
import types
import unittest
from unittest.mock import patch

import preview_demo_patch as demo

TASK_ID = "1" * 32
TASK_SHA = hashlib.sha256(b"Offline synthetic review test").hexdigest()
PASSED = {"passed": True, "exitCode": 0, "oomKilled": False, "imageId": "sha256:" + "a" * 64, "logsCollected": False}


class DemoPatchTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "source"
        shutil.copytree(demo.preflight.DEFAULT_SOURCE, self.root)
        self.captured = demo.preflight.snapshot(self.root)
        self.proposal = {
            "schemaVersion": demo.SCHEMA, "taskId": TASK_ID, "taskSha256": TASK_SHA,
            "baseSnapshotSha256": demo.preflight.manifest(self.captured)["snapshotSha256"],
            "files": {"greeting.go": self.captured["greeting.go"].decode() + "\n// Fake review only.\n"},
        }
        self.path = Path(self.temp.name) / "proposal.json"
        self.save()
        self.receipt_count = 0

    def save(self):
        self.path.write_bytes(json.dumps(self.proposal).encode())

    def review(self, value=None, captured=None):
        return demo.review(self.captured if captured is None else captured,
                           json.dumps(self.proposal if value is None else value).encode(), TASK_ID, TASK_SHA)

    def cli(self, *extra):
        # Each independent sandbox scenario gets an explicitly prepared receipt.
        # Reuse/concurrency is covered separately in the evidence tests.
        if "--sandbox" in extra:
            self.receipt_count += 1
            receipt = Path(self.temp.name) / (str(self.receipt_count) + ".sqlite")
            demo.EvidenceJournal.create(receipt, self.review()[1])
            extra = (*extra, "--evidence-db", str(receipt))
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = demo.main(["--source", str(self.root), "--proposal", str(self.path),
                              "--task-id", TASK_ID, "--task-sha256", TASK_SHA, *extra])
        return code, json.loads(output.getvalue()), output.getvalue()

    def test_default_review_does_not_execute_or_modify_source(self):
        with patch.object(demo.preflight, "sandbox", side_effect=AssertionError("no execution")):
            code, result, _ = self.cli()
        self.assertEqual(code, 0)
        self.assertIn("+// Fake review only.", result["diff"])
        self.assertEqual(result["changedFiles"], ["greeting.go"])
        self.assertFalse(result["sandboxAttempted"])
        self.assertFalse(result["paidExecutionEnabled"])
        self.assertEqual(result["modelCalls"], 0)
        self.assertEqual(demo.preflight.snapshot(self.root), self.captured)
        candidate, _ = self.review()
        self.assertIsNot(candidate, self.captured)
        for name in ("README.md", "go.mod", "greeting_test.go"):
            self.assertEqual(candidate[name], self.captured[name])

    def test_rejects_paths_and_immutable_files(self):
        for name in ("../greeting.go", "a/greeting.go", "/greeting.go", "C:\\greeting.go",
                     "greeting.go:stream", "GREETING.GO", "greeting.go ", "go.mod", "README.md",
                     ".env", "new.go", "greeting.go\x00"):
            value = copy.deepcopy(self.proposal)
            value["files"][name] = "package demo\n"
            with self.subTest(name=name), self.assertRaisesRegex(demo.Blocked, "disallowed_files"):
                self.review(value)

    def test_rejects_model_commands_approval_and_bad_schema(self):
        for key in ("command", "approved", "approveSha256", "delete", "rename"):
            with self.subTest(key=key), self.assertRaisesRegex(demo.Blocked, "invalid_proposal_schema"):
                self.review({**self.proposal, key: True})
        for value in ([], {}, {**self.proposal, "schemaVersion": "v2"}):
            with self.assertRaisesRegex(demo.Blocked, "invalid_proposal_schema"):
                self.review(value)

    def test_rejects_duplicate_keys_and_malformed_json(self):
        for raw, reason in ((b'{"files":{},"files":{}}', "duplicate_json_key"),
                            (b'{"files":{"greeting.go":"a","greeting.go":"b"}}', "duplicate_json_key"),
                            (b"\xff", "invalid_proposal_json"), (b"```json\n{}\n```", "invalid_proposal_json"),
                            (b"[" * 2000, "invalid_proposal_json")):
            with self.subTest(reason=reason), self.assertRaisesRegex(demo.Blocked, reason):
                demo.review(self.captured, raw, TASK_ID, TASK_SHA)

    def test_rejects_invalid_replacements_and_no_changes(self):
        for content, reason in ((None, "invalid_replacement"), ([], "invalid_replacement"),
                                ("\ud800", "invalid_replacement"), ("abc\x00", "binary_file"),
                                (" \n", "empty_replacement"),
                                (self.captured["greeting.go"].decode(), "no_changes")):
            value = {**self.proposal, "files": {"greeting.go": content}}
            with self.subTest(reason=reason), self.assertRaisesRegex(demo.Blocked, reason):
                self.review(value)
        for files in ({}, [], None):
            with self.assertRaisesRegex(demo.Blocked, "disallowed_files"):
                self.review({**self.proposal, "files": files})

    def test_size_boundary_includes_unchanged_files(self):
        fixed = sum(len(value) for name, value in self.captured.items() if name != "greeting.go")
        value = copy.deepcopy(self.proposal)
        value["files"] = {"greeting.go": "x" * (demo.preflight.MAX_BYTES - fixed)}
        self.assertEqual(self.review(value)[1]["candidateBytes"], demo.preflight.MAX_BYTES)
        value["files"]["greeting.go"] += "x"
        with self.assertRaisesRegex(demo.Blocked, "snapshot_too_large"):
            self.review(value)
        with self.assertRaisesRegex(demo.Blocked, "proposal_too_large"):
            demo.review(self.captured, b"x" * (demo.MAX_PROPOSAL_BYTES + 1), TASK_ID, TASK_SHA)

    def test_task_and_base_must_match_caller(self):
        for key, changed, reason in (("taskId", "2" * 32, "task_mismatch"),
                                     ("taskSha256", "2" * 64, "task_mismatch"),
                                     ("baseSnapshotSha256", "2" * 64, "base_snapshot_mismatch")):
            with self.subTest(key=key), self.assertRaisesRegex(demo.Blocked, reason):
                self.review({**self.proposal, key: changed})
        for task_id, digest in (("bad", TASK_SHA), (TASK_ID, "bad"), (TASK_ID, True)):
            with self.assertRaisesRegex(demo.Blocked, "invalid_task_binding"):
                demo.review(self.captured, self.path.read_bytes(), task_id, digest)

    def test_digest_binds_task_base_and_candidate_not_json_formatting(self):
        original = self.review()[1]["approvalSha256"]
        formatted = json.dumps(self.proposal, indent=4).encode()
        self.assertEqual(demo.review(self.captured, formatted, TASK_ID, TASK_SHA)[1]["approvalSha256"], original)
        value = copy.deepcopy(self.proposal)
        value["files"]["greeting.go"] += "// Changed\n"
        self.assertNotEqual(self.review(value)[1]["approvalSha256"], original)
        for key, changed in (("taskId", "2" * 32), ("taskSha256", "2" * 64)):
            value = {**self.proposal, key: changed}
            result = demo.review(self.captured, json.dumps(value).encode(), value["taskId"], value["taskSha256"])
            self.assertNotEqual(result[1]["approvalSha256"], original)
        captured = {**self.captured, "README.md": self.captured["README.md"] + b"\n"}
        value = {**self.proposal, "baseSnapshotSha256": demo.preflight.manifest(captured)["snapshotSha256"]}
        self.assertNotEqual(self.review(value, captured)[1]["approvalSha256"], original)

    def test_missing_or_stale_approval_never_executes(self):
        digest = self.review()[1]["approvalSha256"]
        with patch.object(demo.preflight, "sandbox", side_effect=AssertionError("no execution")):
            for args, reason in ((("--sandbox",), "explicit_patch_approval_required"),
                                 (("--approve-sha256", digest), "explicit_patch_approval_required"),
                                 (("--sandbox", "--approve-sha256", "0" * 64), "approval_mismatch")):
                code, result, _ = self.cli(*args)
                self.assertEqual(code, 1)
                self.assertEqual(result["reason"], reason)
                self.assertFalse(result["sandboxAttempted"])
            self.proposal["files"]["greeting.go"] += "// New code\n"
            self.save()
            self.assertEqual(self.cli("--sandbox", "--approve-sha256", digest)[1]["reason"], "approval_mismatch")

    def test_source_changes_before_and_during_approval_rejected(self):
        digest = self.review()[1]["approvalSha256"]
        changed = {**self.captured, "README.md": b"changed"}
        with patch.object(demo.preflight, "snapshot", side_effect=[self.captured, changed]), \
                patch.object(demo.preflight, "sandbox", side_effect=AssertionError("no execution")):
            self.assertEqual(self.cli("--sandbox", "--approve-sha256", digest)[1]["reason"], "source_changed")
        (self.root / "README.md").write_bytes(b"changed")
        with patch.object(demo.preflight, "sandbox", side_effect=AssertionError("no execution")):
            self.assertEqual(self.cli("--sandbox", "--approve-sha256", digest)[1]["reason"], "base_snapshot_mismatch")

    def test_only_approved_copy_sent_to_fixed_sandbox(self):
        candidate, details = self.review()
        with patch.object(demo.preflight, "sandbox", return_value=PASSED) as sandbox:
            code, result, _ = self.cli("--sandbox", "--approve-sha256", details["approvalSha256"])
        sandbox.assert_called_once_with(candidate, "golang:1.22-alpine", 90)
        self.assertEqual(code, 0)
        self.assertTrue(result["sandboxExecuted"])
        self.assertNotIn("diff", result)
        self.assertEqual(demo.preflight.snapshot(self.root), self.captured)

    def test_test_failure_cleanup_failure_and_interrupt_are_not_success(self):
        digest = self.review()[1]["approvalSha256"]
        with patch.object(demo.preflight, "sandbox", return_value={**PASSED, "passed": False, "exitCode": 1}):
            code, result, _ = self.cli("--sandbox", "--approve-sha256", digest)
            self.assertEqual(code, 1)
            self.assertTrue(result["sandboxExecuted"])
        for error, reason in ((demo.Blocked("cleanup_unconfirmed", "owned-container"), "cleanup_unconfirmed"),
                              (KeyboardInterrupt(), "interrupted")):
            with patch.object(demo.preflight, "sandbox", side_effect=error):
                code, result, _ = self.cli("--sandbox", "--approve-sha256", digest)
                self.assertEqual(code, 1)
                self.assertEqual(result["reason"], reason)
                self.assertTrue(result["sandboxAttempted"])
                self.assertFalse(result["sandboxExecuted"])
                if reason == "cleanup_unconfirmed":
                    self.assertEqual(result["containerRequiringInspection"], "owned-container")

    def test_bounded_regular_file_read_and_safe_errors(self):
        self.assertEqual(demo.read_proposal(self.path), self.path.read_bytes())
        self.path.write_bytes(b"x" * (demo.MAX_PROPOSAL_BYTES + 1))
        with self.assertRaisesRegex(demo.Blocked, "proposal_too_large"):
            demo.read_proposal(self.path)
        for path in (self.root, self.root / ".." / "proposal.json"):
            with self.assertRaisesRegex(demo.Blocked, "unsafe_proposal_path"):
                demo.read_proposal(path)
        self.path.unlink()
        code, result, text = self.cli()
        self.assertEqual(code, 1)
        self.assertEqual(result["reason"], "proposal_unreadable")
        self.assertNotIn(str(self.path), text)

    def test_hardlinked_proposal_rejected(self):
        alias = self.path.with_name("alias.json")
        os.link(self.path, alias)
        with self.assertRaisesRegex(demo.Blocked, "unsafe_proposal_path"):
            demo.read_proposal(alias)

    def test_changed_proposal_during_read_rejected(self):
        info = self.path.stat()
        fields = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns")
        changed = types.SimpleNamespace(**{key: getattr(info, key) for key in fields})
        changed.st_mtime_ns += 1
        with patch.object(demo.os, "fstat", side_effect=[info, changed]):
            with self.assertRaisesRegex(demo.Blocked, "proposal_changed"):
                demo.read_proposal(self.path)

    def test_reparse_proposal_rejected_without_symlink_privileges(self):
        with patch.object(demo.preflight, "linked", return_value=True):
            with self.assertRaisesRegex(demo.Blocked, "unsafe_proposal_path"):
                demo.read_proposal(self.path)

    def test_both_editable_files_allowed_and_invalid_snapshot_rejected(self):
        value = copy.deepcopy(self.proposal)
        value["files"]["greeting_test.go"] = self.captured["greeting_test.go"].decode() + "\n// Fake test edit.\n"
        self.assertEqual(self.review(value)[1]["changedFiles"], ["greeting.go", "greeting_test.go"])
        for captured, reason in (({}, "invalid_snapshot"),
                                 ({**self.captured, "greeting.go": "text"}, "invalid_snapshot"),
                                 ({**self.captured, "go.mod": b"module other"}, "module_contract_changed"),
                                 ({**self.captured, "README.md": b"\xff"}, "non_utf8_file")):
            with self.assertRaisesRegex(demo.Blocked, reason):
                self.review(captured=captured)

    def test_symlinked_proposal_rejected(self):
        alias = self.path.with_name("alias.json")
        try:
            alias.symlink_to(self.path)
        except OSError:
            self.skipTest("OS does not grant symlink creation")
        with self.assertRaisesRegex(demo.Blocked, "unsafe_proposal_path"):
            demo.read_proposal(alias)

    def test_review_escapes_controls_and_marks_missing_newline(self):
        self.proposal["files"]["greeting.go"] += "// \x1b[31m control without newline"
        self.save()
        code, result, text = self.cli()
        self.assertEqual(code, 0)
        self.assertNotIn("\x1b", text)
        self.assertIn("\\u001b", text)
        self.assertIn("\\ No newline at end of file", result["diff"])


if __name__ == "__main__":
    unittest.main()
