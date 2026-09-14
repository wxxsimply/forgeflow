"""Offline-only task archive with a fixed, colocated Fake budget ledger.

prepare explicitly saves private input bytes; inspect emits hashes/status only.
There is no provider, sandbox, credentials or paid execution command here.
"""

import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3

from preview_demo_budget import BudgetBlocked, BudgetLedger, check_path, policy_json
from preview_demo_patch import read_proposal, validate_snapshot
import preview_demo_preflight as preflight


Blocked = preflight.Blocked
DEFAULT_ROOT = Path(__file__).resolve().parent.parent / ".forgeflow" / "demo-tasks"
MAX_TASK_BYTES = 20_000
FAKE_MODEL = "offline-fake-v1"
FAKE_PRICING = hashlib.sha256(b"forgeflow.offline-fake-not-a-provider-price/v1").hexdigest()


def task_bytes(value):
    if type(value) is not bytes or not 1 <= len(value) <= MAX_TASK_BYTES or b"\0" in value:
        raise Blocked("invalid_task_text")
    try:
        if not value.decode("utf-8").strip():
            raise Blocked("invalid_task_text")
    except UnicodeDecodeError:
        raise Blocked("invalid_task_text") from None


def directory(path):
    if ".." in Path(path).parts:
        raise Blocked("unsafe_task_path")
    path = Path(path).absolute()
    try:
        for item in (path, *path.parents):
            if preflight.linked(item.lstat()):
                raise Blocked("unsafe_task_path")
        if not path.is_dir():
            raise Blocked("unsafe_task_path")
    except OSError:
        raise Blocked("task_directory_unavailable") from None
    return path


def make_policy(captured, task, calls, cost):
    validate_snapshot(captured)
    task_bytes(task)
    policy = {"snapshot_sha256": preflight.manifest(captured)["snapshotSha256"],
              "task_sha256": hashlib.sha256(task).hexdigest(), "pricing_sha256": FAKE_PRICING,
              "provider": "fake", "model": FAKE_MODEL, "max_calls": calls, "max_cost_nano_usd": cost}
    try:
        policy_json(policy)
    except BudgetBlocked:
        raise Blocked("invalid_task_policy") from None
    return policy


class TaskArchive:
    def __init__(self, root, task_id):
        if not isinstance(task_id, str) or not re.fullmatch(r"[0-9a-f]{32}", task_id):
            raise Blocked("invalid_task_id")
        self.root = directory(root)
        self.task_id = task_id
        self.path = self.root / task_id
        self.database = self.path / "task.sqlite"

    @contextmanager
    def _connection(self):
        conn = None
        try:
            directory(self.path)
            check_path(self.database)
            conn = sqlite3.connect(self.database.as_uri() + "?mode=rw", uri=True, timeout=2, isolation_level=None)
            conn.execute("PRAGMA synchronous=FULL")
            conn.execute("BEGIN IMMEDIATE")
            yield conn
            conn.commit()
        except (sqlite3.Error, OSError, BudgetBlocked, ValueError, TypeError, RecursionError):
            raise Blocked("task_archive_unavailable") from None
        finally:
            if conn is not None:
                conn.close()

    @classmethod
    def create(cls, root, task_id, captured, task, *, max_calls=1, max_cost_nano_usd=1_000_000):
        # Validate every byte/config before reserving the directory. No caller
        # can supply a DeepSeek provider or a second budget path to this entry.
        policy = make_policy(captured, task, max_calls, max_cost_nano_usd)
        archive = cls(root, task_id)
        try:
            archive.path.mkdir(mode=0o700)
        except FileExistsError:
            raise Blocked("task_already_exists") from None
        except OSError:
            raise Blocked("task_directory_unavailable") from None
        # mkdir is the same-root ID reservation. Any subsequent failure leaves
        # the directory occupied for manual inspection; no overwrite/resume.
        try:
            BudgetLedger.create(archive.database, policy)
        except BudgetBlocked:
            raise Blocked("task_initialization_failed") from None
        with archive._connection() as conn:
            conn.execute("""CREATE TABLE task_archive (
                id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL,
                task_id TEXT NOT NULL, location TEXT NOT NULL, task BLOB NOT NULL)""")
            conn.execute("CREATE TABLE task_sources (name TEXT PRIMARY KEY, data BLOB NOT NULL)")
            conn.execute("INSERT INTO task_archive VALUES(1,1,?,?,?)",
                         (task_id, os.path.normcase(str(archive.path)), task))
            conn.executemany("INSERT INTO task_sources VALUES(?,?)", [(name, captured[name]) for name in preflight.FILES])
        return archive

    @classmethod
    def open(cls, root, task_id):
        archive = cls(root, task_id)
        archive.summary()
        return archive

    def load(self):
        """Return captured bytes to trusted local callers only, never stdout."""
        with self._connection() as conn:
            rows = conn.execute("SELECT id,version,task_id,location,length(task),typeof(task) FROM task_archive").fetchall()
            if (len(rows) != 1 or rows[0][:4] != (1, 1, self.task_id, os.path.normcase(str(self.path)))
                    or rows[0][5] != "blob" or not 1 <= rows[0][4] <= MAX_TASK_BYTES):
                raise Blocked("task_binding_mismatch")
            sizes = conn.execute("SELECT name,length(data),typeof(data) FROM task_sources").fetchall()
            if (len(sizes) != len(preflight.FILES) or {row[0] for row in sizes} != set(preflight.FILES)
                    or any(kind != "blob" or type(size) is not int or size < 0 for _, size, kind in sizes)
                    or sum(size for _, size, _ in sizes) > preflight.MAX_BYTES):
                raise Blocked("invalid_archived_snapshot")
            policies = conn.execute("SELECT id,version,length(body) FROM policy").fetchall()
            if len(policies) != 1 or policies[0][:2] != (1, 1) or not 1 <= policies[0][2] <= 4096:
                raise Blocked("invalid_archived_policy")
            policy = json.loads(conn.execute("SELECT body FROM policy WHERE id=1").fetchone()[0])
            if not isinstance(policy, dict):
                raise Blocked("invalid_archived_policy")
            task = conn.execute("SELECT task FROM task_archive WHERE id=1").fetchone()[0]
            captured = dict(conn.execute("SELECT name,data FROM task_sources").fetchall())
            expected = make_policy(captured, task, policy.get("max_calls"), policy.get("max_cost_nano_usd"))
            if policy != expected:
                raise Blocked("archived_content_mismatch")
        return captured, task, expected

    def budget(self):
        _, _, policy = self.load()
        try:
            return BudgetLedger.open(self.database, policy)
        except BudgetBlocked:
            raise Blocked("task_budget_unavailable") from None

    def summary(self):
        captured, task, policy = self.load()
        try:
            budget = BudgetLedger.open(self.database, policy).summary()
        except BudgetBlocked:
            raise Blocked("task_budget_unavailable") from None
        return {"taskId": self.task_id, "taskSha256": policy["task_sha256"], "taskBytes": len(task),
                **preflight.manifest(captured), "provider": "fake", "model": FAKE_MODEL,
                "budget": budget, "paidExecutionEnabled": False, "readyForPaidExecution": False}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    for action in ("prepare", "inspect"):
        command = commands.add_parser(action)
        command.add_argument("--root", type=Path, default=DEFAULT_ROOT, help="existing private task root; no parent directories created")
        command.add_argument("--task-id", required=True, help="32 lowercase hex characters")
        if action == "prepare":
            command.add_argument("--source", type=Path, default=preflight.DEFAULT_SOURCE)
            command.add_argument("--task-file", type=Path, required=True)
            command.add_argument("--max-calls", type=int, choices=(1, 2, 3), default=1)
            command.add_argument("--max-cost-nano-usd", type=int, default=1_000_000, help="Fake accounting limit, not paid authorization")
    args = parser.parse_args(argv)
    report = {"schemaVersion": "forgeflow.demo.task-archive/v1", "modelCalls": 0,
              "paidExecutionEnabled": False, "readyForPaidExecution": False}
    try:
        if args.action == "prepare":
            source = args.source.absolute()
            if source == args.root.absolute() or source in args.root.absolute().parents:
                raise Blocked("task_root_inside_source")
            captured = preflight.snapshot(args.source)
            # Reuses bounded, no-link, stable-file reading; accepted task <=20 KB.
            task = read_proposal(args.task_file)
            archive = TaskArchive.create(args.root, args.task_id, captured, task,
                                         max_calls=args.max_calls, max_cost_nano_usd=args.max_cost_nano_usd)
        else:
            archive = TaskArchive.open(args.root, args.task_id)
        report.update(archive.summary(), checksPassed=True)
    except Blocked as error:
        report.update(checksPassed=False, reason=str(error))
    except (OSError, ValueError, TypeError):
        report.update(checksPassed=False, reason="task_archive_failed")
    except KeyboardInterrupt:
        report.update(checksPassed=False, reason="interrupted")
    print(json.dumps(report, ensure_ascii=True, indent=2))
    return 0 if report["checksPassed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
