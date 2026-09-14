"""Local, single-use patch execution receipts. No network or code execution.

Use a private operator-owned directory. This is not tamper-proof storage or
cross-database task identity enforcement. Failed/unknown attempts never reset.
"""

from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3

from preview_demo_budget import BudgetBlocked, check_path
from preview_demo_preflight import Blocked


FIELDS = ("schemaVersion", "taskId", "taskSha256", "baseSnapshotSha256", "candidateSnapshotSha256")


def encode(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False)


def binding(review):
    if not isinstance(review, dict) or not all(key in review for key in (*FIELDS, "approvalSha256")):
        raise Blocked("invalid_evidence_binding")
    result = {key: review[key] for key in FIELDS}
    if (result["schemaVersion"] != "forgeflow.demo.patch/v1"
            or not isinstance(result["taskId"], str) or not re.fullmatch(r"[0-9a-f]{32}", result["taskId"])
            or any(not isinstance(result[key], str) or not re.fullmatch(r"[0-9a-f]{64}", result[key])
                   for key in FIELDS[2:])):
        raise Blocked("invalid_evidence_binding")
    approval = hashlib.sha256(encode(result).encode()).hexdigest()
    if review["approvalSha256"] != approval:
        raise Blocked("invalid_evidence_binding")
    return {**result, "approvalSha256": approval}


def validate_execution(image, seconds):
    if (not isinstance(image, str) or not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9._/@:-]{0,255}", image)
            or type(seconds) is not int or not 1 <= seconds <= 120):
        raise Blocked("invalid_execution_config")


def validate_result(value):
    keys = {"passed", "exitCode", "oomKilled", "imageId", "logsCollected"}
    if (not isinstance(value, dict) or set(value) != keys
            or type(value["passed"]) is not bool or type(value["oomKilled"]) is not bool
            or type(value["exitCode"]) is not int or not 0 <= value["exitCode"] <= 255
            or value["logsCollected"] is not False
            or not isinstance(value["imageId"], str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", value["imageId"])
            or value["passed"] != (value["exitCode"] == 0 and not value["oomKilled"])):
        raise Blocked("invalid_execution_result")


class EvidenceJournal:
    def __init__(self, path, review):
        if ".." in Path(path).parts:
            raise Blocked("unsafe_evidence_path")
        self.path = Path(path).absolute()
        self.binding = binding(review)
        self._binding = encode(self.binding)

    def _check_path(self, require_file=True):
        try:
            check_path(self.path, require_file=require_file)
        except BudgetBlocked:
            raise Blocked("evidence_path_unavailable") from None

    @classmethod
    def create(cls, path, review):
        journal = cls(path, review)
        journal._check_path(require_file=False)
        try:
            fd = os.open(journal.path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            os.close(fd)
        except FileExistsError:
            raise Blocked("evidence_already_exists") from None
        except OSError:
            raise Blocked("evidence_unavailable") from None
        # Leave failed initialization in place for inspection; never overwrite.
        with journal._transaction(initializing=True) as conn:
            conn.execute("""CREATE TABLE receipt (
                id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL,
                binding TEXT NOT NULL, state TEXT NOT NULL, approval TEXT,
                image TEXT, seconds INTEGER, result TEXT,
                reviewed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
                started_at TEXT, finished_at TEXT)""")
            conn.execute("INSERT INTO receipt(id,version,binding,state) VALUES(1,1,?,'reviewed')", (journal._binding,))
        return journal

    @classmethod
    def open(cls, path, review):
        journal = cls(path, review)
        journal.summary()
        return journal

    @contextmanager
    def _transaction(self, initializing=False):
        self._check_path()
        conn = None
        try:
            conn = sqlite3.connect(self.path.as_uri() + "?mode=rw", uri=True, timeout=2, isolation_level=None)
            conn.execute("PRAGMA synchronous=FULL")
            conn.execute("BEGIN IMMEDIATE")
            if not initializing:
                self._read(conn)
            yield conn
            conn.commit()
        except (sqlite3.Error, OSError, ValueError, TypeError):
            raise Blocked("evidence_unavailable") from None
        finally:
            if conn is not None:
                conn.close()

    def _read(self, conn):
        rows = conn.execute("SELECT id,version,binding,state,approval,image,seconds,result,reviewed_at,started_at,finished_at FROM receipt").fetchall()
        if len(rows) != 1 or rows[0][:3] != (1, 1, self._binding):
            raise Blocked("evidence_binding_mismatch")
        _, _, _, state, approval, image, seconds, raw, reviewed_at, started_at, finished_at = rows[0]
        if state not in ("reviewed", "running", "unknown", "completed") or not isinstance(reviewed_at, str):
            raise Blocked("invalid_evidence_state")
        result = None
        if state == "reviewed":
            if any(value is not None for value in (approval, image, seconds, raw, started_at, finished_at)):
                raise Blocked("invalid_evidence_state")
        else:
            if approval != self.binding["approvalSha256"] or not isinstance(started_at, str):
                raise Blocked("invalid_evidence_state")
            validate_execution(image, seconds)
            if state == "completed":
                result = json.loads(raw)
                validate_result(result)
            elif raw is not None:
                raise Blocked("invalid_evidence_state")
            if (state == "running" and finished_at is not None
                    or state in ("unknown", "completed") and not isinstance(finished_at, str)):
                raise Blocked("invalid_evidence_state")
        return {"state": state, "binding": json.loads(self._binding), "result": result,
                "requestedImage": image, "timeoutSeconds": seconds,
                "reviewedAt": reviewed_at, "startedAt": started_at, "finishedAt": finished_at}

    def summary(self):
        with self._transaction() as conn:
            return self._read(conn)

    def begin(self, approval, image, seconds):
        validate_execution(image, seconds)
        if approval != self.binding["approvalSha256"]:
            raise Blocked("approval_mismatch")
        with self._transaction() as conn:
            if self._read(conn)["state"] != "reviewed":
                raise Blocked("evidence_already_consumed")
            conn.execute("""UPDATE receipt SET state='running',approval=?,image=?,seconds=?,
                started_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1""", (approval, image, seconds))

    def _finish(self, result=None):
        if result is not None:
            validate_result(result)
        with self._transaction() as conn:
            if self._read(conn)["state"] != "running":
                raise Blocked("invalid_evidence_transition")
            conn.execute("""UPDATE receipt SET state=?,result=?,
                finished_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1""",
                         ("completed" if result is not None else "unknown", encode(result) if result is not None else None))

    def run_once(self, approval, image, seconds, run):
        # Commit approval consumption before invoking the trusted sandbox adapter.
        self.begin(approval, image, seconds)
        try:
            result = run()
            validate_result(result)
        except BaseException:
            # No raw exception/logs stored. A failed write leaves 'running', which
            # is also blocking after restart. Abrupt process death does likewise.
            self._finish()
            raise
        self._finish(result)
        return result
