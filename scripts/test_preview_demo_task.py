"""Offline task archive tests. Temporary inputs only; no network or sandbox."""

from concurrent.futures import ThreadPoolExecutor
import contextlib
import io
import json
import os
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
TASK = "离线测试任务：只检查输入归档，不调用模型。\n".encode()


class TaskArchiveTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "private"
        self.root.mkdir()
        self.source = Path(self.temp.name) / "source"
        shutil.copytree(demo.preflight.DEFAULT_SOURCE, self.source)
        self.captured = demo.preflight.snapshot(self.source)

    def create(self, **kwargs):
        return demo.TaskArchive.create(self.root, TASK_ID, self.captured, TASK, **kwargs)

    def open(self):
        return demo.TaskArchive.open(self.root, TASK_ID)

    def test_archive_exact_bytes_and_fixed_budget_survive_restart(self):
        archive = self.create()
        captured, task, policy = self.open().load()
        self.assertEqual(captured, self.captured)
        self.assertEqual(task, TASK)
        self.assertEqual(policy["provider"], "fake")
        self.assertEqual(archive.budget().path, archive.database)
        self.assertEqual({p.name for p in archive.path.iterdir()}, {"task.sqlite"})
        summary = self.open().summary()
        self.assertFalse(summary["paidExecutionEnabled"])
        self.assertEqual(summary["budget"]["attempts"], 0)
        text = json.dumps(summary, ensure_ascii=False)
        for sensitive in (str(self.root), TASK.decode().strip(), "func Greet", "package"):
            self.assertNotIn(sensitive, text)

    def test_original_edits_do_not_replace_archived_input(self):
        self.create()
        (self.source / "greeting.go").write_bytes(b"changed source")
        self.assertEqual(self.open().load()[0], self.captured)

    def test_duplicate_id_never_resets_existing_budget(self):
        archive = self.create(max_calls=2)
        archive.budget().invoke_once(10, lambda: (3, "a" * 64))
        with self.assertRaisesRegex(demo.Blocked, "task_already_exists"):
            self.create(max_calls=3)
        summary = self.open().summary()["budget"]
        self.assertEqual(summary["attempts"], 1)
        self.assertEqual(summary["spent_nano_usd"], 3)

    def test_unfinished_budget_blocks_after_reopen(self):
        self.create().budget().reserve(10)
        self.assertTrue(self.open().summary()["budget"]["unfinished"])
        with self.assertRaisesRegex(demo.BudgetBlocked, "unresolved_attempt"):
            self.open().budget().invoke_once(10, lambda: self.fail("must not invoke"))

    def test_same_root_concurrent_creation_has_one_winner(self):
        barrier = threading.Barrier(2)
        def create():
            barrier.wait(timeout=5)
            try:
                self.create()
                return "created"
            except demo.Blocked as error:
                return str(error)
        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(lambda _: create(), range(2)))
        self.assertCountEqual(results, ["created", "task_already_exists"])
        self.open()

    def test_copied_directory_and_renamed_id_rejected(self):
        archive = self.create()
        other = Path(self.temp.name) / "other"
        other.mkdir()
        shutil.copytree(archive.path, other / TASK_ID)
        with self.assertRaisesRegex(demo.Blocked, "task_binding_mismatch"):
            demo.TaskArchive.open(other, TASK_ID)
        shutil.copytree(archive.path, self.root / ("2" * 32))
        with self.assertRaisesRegex(demo.Blocked, "task_binding_mismatch"):
            demo.TaskArchive.open(self.root, "2" * 32)

    def test_invalid_inputs_do_not_create_directory(self):
        for task in (b"", b" \n", b"\0", b"\xff", "text", b"x" * (demo.MAX_TASK_BYTES + 1)):
            with self.subTest(task_type=type(task).__name__), self.assertRaisesRegex(demo.Blocked, "invalid_task_text"):
                demo.TaskArchive.create(self.root, TASK_ID, self.captured, task)
        for kwargs in ({"max_calls": True}, {"max_calls": 4}, {"max_cost_nano_usd": -1}, {"max_cost_nano_usd": 1.2}):
            with self.assertRaisesRegex(demo.Blocked, "invalid_task_policy"):
                self.create(**kwargs)
        for task_id in ("bad", "../escape", "A" * 32, "1" * 33):
            with self.assertRaisesRegex(demo.Blocked, "invalid_task_id"):
                demo.TaskArchive.create(self.root, task_id, self.captured, TASK)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_task_text_exact_size_boundary(self):
        task = b"x" * demo.MAX_TASK_BYTES
        archive = demo.TaskArchive.create(self.root, TASK_ID, self.captured, task)
        self.assertEqual(archive.load()[1], task)

    def test_missing_directory_database_or_archive_not_recreated(self):
        with self.assertRaises(demo.Blocked):
            self.open()
        self.assertFalse((self.root / TASK_ID).exists())
        archive = self.create()
        archive.database.unlink()
        with self.assertRaises(demo.Blocked):
            self.open()
        self.assertFalse(archive.database.exists())
        with self.assertRaisesRegex(demo.Blocked, "task_already_exists"):
            self.create()

    def test_initialization_failure_leaves_id_reserved(self):
        with patch.object(demo.BudgetLedger, "create", side_effect=demo.BudgetBlocked("private failure")):
            with self.assertRaisesRegex(demo.Blocked, "task_initialization_failed"):
                self.create()
        self.assertTrue((self.root / TASK_ID).is_dir())
        with self.assertRaisesRegex(demo.Blocked, "task_already_exists"):
            self.create()

    def test_archive_commit_failure_leaves_incomplete_not_ready(self):
        class FailedCommit(sqlite3.Connection):
            def commit(self):
                if self.execute("SELECT name FROM sqlite_master WHERE name='task_archive'").fetchone():
                    raise sqlite3.OperationalError("fail archive commit")
                return super().commit()
        connect = sqlite3.connect
        with patch.object(demo.sqlite3, "connect", side_effect=lambda *a, **k: connect(*a, **k, factory=FailedCommit)):
            with self.assertRaisesRegex(demo.Blocked, "task_archive_unavailable"):
                self.create()
        with self.assertRaises(demo.Blocked):
            self.open()
        with self.assertRaisesRegex(demo.Blocked, "task_already_exists"):
            self.create()

    def test_process_exit_during_initialization_preserves_reservation(self):
        code = "import os,sys; import preview_demo_task as d; d.BudgetLedger.create=lambda *a,**k:os._exit(0); d.TaskArchive.create(sys.argv[1],sys.argv[2],d.preflight.snapshot(d.preflight.DEFAULT_SOURCE),b'fake task')"
        result = subprocess.run([sys.executable, "-B", "-c", code, str(self.root), TASK_ID],
                                cwd=Path(__file__).parent, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0)
        self.assertTrue((self.root / TASK_ID).is_dir())
        with self.assertRaisesRegex(demo.Blocked, "task_already_exists"):
            self.create()
        with self.assertRaises(demo.Blocked):
            self.open()

    def test_content_and_policy_drift_detected(self):
        archive = self.create()
        with contextlib.closing(sqlite3.connect(archive.database)) as conn:
            conn.execute("UPDATE task_sources SET data=? WHERE name='greeting.go'", (b"changed",))
            conn.commit()
        with self.assertRaisesRegex(demo.Blocked, "archived_content_mismatch"):
            self.open()
        with contextlib.closing(sqlite3.connect(archive.database)) as conn:
            conn.execute("UPDATE task_sources SET data=? WHERE name='greeting.go'", (self.captured["greeting.go"],))
            policy = json.loads(conn.execute("SELECT body FROM policy").fetchone()[0])
            policy["provider"] = "deepseek"
            conn.execute("UPDATE policy SET body=?", (json.dumps(policy),))
            conn.commit()
        with self.assertRaisesRegex(demo.Blocked, "archived_content_mismatch"):
            self.open()

    def test_extra_archived_source_rejected(self):
        archive = self.create()
        with contextlib.closing(sqlite3.connect(archive.database)) as conn:
            conn.execute("INSERT INTO task_sources VALUES('.env',?)", (b"not a real credential",))
            conn.commit()
        with self.assertRaisesRegex(demo.Blocked, "invalid_archived_snapshot"):
            self.open()

    def test_hardlinks_traversal_and_reparse_rejected(self):
        archive = self.create()
        os.link(archive.database, self.root / "alias.sqlite")
        with self.assertRaises(demo.Blocked):
            self.open()
        with self.assertRaisesRegex(demo.Blocked, "unsafe_task_path"):
            demo.TaskArchive.open(self.root / ".." / "private", TASK_ID)
        with patch.object(demo.preflight, "linked", return_value=True):
            with self.assertRaisesRegex(demo.Blocked, "unsafe_task_path"):
                self.open()

    def cli(self, action, *extra):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = demo.main([action, "--root", str(self.root), "--task-id", TASK_ID, *extra])
        return code, json.loads(output.getvalue()), output.getvalue()

    def test_cli_prepare_and_inspect_without_original_inputs(self):
        task_file = Path(self.temp.name) / "task.txt"
        task_file.write_bytes(TASK)
        code, report, text = self.cli("prepare", "--task-file", str(task_file), "--source", str(self.source))
        self.assertEqual(code, 0)
        self.assertEqual(report["modelCalls"], 0)
        self.assertFalse(report["readyForPaidExecution"])
        self.assertNotIn(str(self.root), text)
        self.assertNotIn(TASK.decode().strip(), text)
        task_file.unlink()
        self.assertEqual(self.cli("inspect")[0], 0)
        self.assertEqual(demo.preflight.snapshot(self.source), self.captured)

    def test_cli_rejects_root_inside_source_and_missing_input(self):
        code, report, _ = self.cli("prepare", "--root", str(self.source), "--source", str(self.source), "--task-file", "missing")
        self.assertEqual(code, 1)
        self.assertEqual(report["reason"], "task_root_inside_source")
        self.assertEqual(demo.preflight.snapshot(self.source), self.captured)
        code, report, text = self.cli("prepare", "--source", str(self.source), "--task-file", str(self.root / "private-missing"))
        self.assertEqual(code, 1)
        self.assertNotIn("private-missing", text)
        self.assertEqual(list(self.root.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
