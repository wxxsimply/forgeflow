"""Persistent offline budget primitives. No HTTP client, keys or paid CLI.

Amounts are integer nano-USD (1 USD = 1_000_000_000 units), not RMB.
Use a caller-owned private directory; this is not tamper-proof billing storage.
"""

from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import stat
import uuid


MAX_AMOUNT = 10**15
POLICY_KEYS = {"snapshot_sha256", "task_sha256", "pricing_sha256", "provider", "model", "max_calls", "max_cost_nano_usd"}


class BudgetBlocked(Exception):
    """Fixed codes only; never include raw provider errors or local paths."""


def digest(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def amount(value, allow_zero=False):
    if type(value) is not int or not (0 if allow_zero else 1) <= value <= MAX_AMOUNT:
        raise BudgetBlocked("invalid_amount")


def policy_json(policy):
    if not isinstance(policy, dict) or set(policy) != POLICY_KEYS:
        raise BudgetBlocked("invalid_policy")
    if not all(digest(policy[key]) for key in ("snapshot_sha256", "task_sha256", "pricing_sha256")):
        raise BudgetBlocked("invalid_policy")
    if policy["provider"] not in ("fake", "deepseek"):
        raise BudgetBlocked("invalid_policy")
    if not isinstance(policy["model"], str) or not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9._/-]{0,99}", policy["model"]):
        raise BudgetBlocked("invalid_policy")
    if type(policy["max_calls"]) is not int or not 1 <= policy["max_calls"] <= 3:
        raise BudgetBlocked("invalid_policy")
    amount(policy["max_cost_nano_usd"])
    return json.dumps(policy, sort_keys=True, separators=(",", ":"))


def check_path(path, require_file=True):
    try:
        for item in (path, *path.parents) if require_file else path.parents:
            info = item.lstat()
            if stat.S_ISLNK(info.st_mode) or getattr(info, "st_file_attributes", 0) & 0x400:
                raise BudgetBlocked("unsafe_ledger_path")
        if require_file:
            info = path.lstat()
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
                raise BudgetBlocked("unsafe_ledger_path")
    except OSError:
        raise BudgetBlocked("ledger_unavailable") from None


class BudgetLedger:
    def __init__(self, path, policy):
        if ".." in Path(path).parts:
            raise BudgetBlocked("unsafe_ledger_path")
        self.path = Path(path).absolute()
        self._policy_json = policy_json(policy)
        self._policy = json.loads(self._policy_json)

    @classmethod
    def create(cls, path, policy):
        ledger = cls(path, policy)
        check_path(ledger.path, require_file=False)
        try:
            fd = os.open(ledger.path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            os.close(fd)
        except FileExistsError:
            raise BudgetBlocked("ledger_already_exists") from None
        except OSError:
            raise BudgetBlocked("ledger_unavailable") from None
        # Failed initialization stays in place; create never overwrites evidence.
        try:
            with ledger._connection() as conn:
                conn.execute("BEGIN IMMEDIATE")
                conn.execute("CREATE TABLE policy (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL, body TEXT NOT NULL)")
                conn.execute("INSERT INTO policy VALUES(1, 1, ?)", (ledger._policy_json,))
                conn.execute("""CREATE TABLE attempts (
                    id TEXT PRIMARY KEY, reserved INTEGER NOT NULL CHECK(reserved>0),
                    status TEXT NOT NULL CHECK(status IN ('reserved','unknown','settled')),
                    actual INTEGER CHECK(actual>=0), receipt TEXT, reconciled INTEGER NOT NULL DEFAULT 0)""")
                conn.commit()
        except (sqlite3.Error, OSError):
            raise BudgetBlocked("ledger_unavailable") from None
        return ledger

    @classmethod
    def open(cls, path, policy):
        ledger = cls(path, policy)
        ledger.summary()
        return ledger

    @contextmanager
    def _connection(self):
        check_path(self.path)
        conn = None
        try:
            # mode=rw refuses to silently recreate a missing ledger on restart.
            conn = sqlite3.connect(self.path.as_uri() + "?mode=rw", uri=True, timeout=2, isolation_level=None)
            conn.execute("PRAGMA synchronous=FULL")
            yield conn
        finally:
            if conn is not None:
                conn.close()

    @contextmanager
    def _transaction(self):
        try:
            with self._connection() as conn:
                conn.execute("BEGIN IMMEDIATE")
                if conn.execute("SELECT id, version, body FROM policy").fetchall() != [(1, 1, self._policy_json)]:
                    raise BudgetBlocked("policy_mismatch")
                self._summary(conn)
                yield conn
                conn.commit()
        except (sqlite3.Error, OSError):
            raise BudgetBlocked("ledger_unavailable") from None

    def _summary(self, conn):
        rows = conn.execute("SELECT id, reserved, status, actual, receipt, reconciled FROM attempts").fetchall()
        if len(rows) > self._policy["max_calls"]:
            raise BudgetBlocked("invalid_ledger")
        spent = held = pending = 0
        exceeded = False
        for token, reserved, status, actual, receipt, reconciled in rows:
            if not isinstance(token, str) or not re.fullmatch(r"[0-9a-f]{32}", token) or reconciled not in (0, 1):
                raise BudgetBlocked("invalid_ledger")
            amount(reserved)
            if reserved > self._policy["max_cost_nano_usd"]:
                raise BudgetBlocked("invalid_ledger")
            if status == "settled":
                amount(actual, allow_zero=True)
                if not digest(receipt):
                    raise BudgetBlocked("invalid_ledger")
                spent += actual
                exceeded |= actual > reserved
            elif status in ("reserved", "unknown") and actual is None and receipt is None and reconciled == 0:
                held += reserved
                pending += 1
            else:
                raise BudgetBlocked("invalid_ledger")
        limit = self._policy["max_cost_nano_usd"]
        if pending > 1 or (spent + held > limit and not exceeded):
            raise BudgetBlocked("invalid_ledger")
        return {"attempts": len(rows), "spent_nano_usd": spent, "held_nano_usd": held,
                "remaining_nano_usd": max(0, limit - spent - held), "unfinished": bool(pending),
                "estimate_exceeded": exceeded or spent > limit,
                "policy_sha256": hashlib.sha256(self._policy_json.encode()).hexdigest()}

    def summary(self):
        with self._transaction() as conn:
            return self._summary(conn)

    def reserve(self, maximum_nano_usd):
        amount(maximum_nano_usd)
        with self._transaction() as conn:
            state = self._summary(conn)
            if state["unfinished"]:
                raise BudgetBlocked("unresolved_attempt")
            if state["estimate_exceeded"]:
                raise BudgetBlocked("cost_estimate_exceeded")
            if state["attempts"] >= self._policy["max_calls"]:
                raise BudgetBlocked("call_limit")
            if maximum_nano_usd > state["remaining_nano_usd"]:
                raise BudgetBlocked("cost_limit")
            token = uuid.uuid4().hex
            conn.execute("INSERT INTO attempts(id,reserved,status) VALUES(?,?,'reserved')", (token, maximum_nano_usd))
        return token  # Durable commit before invoking an external side effect.

    def mark_unknown(self, token):
        with self._transaction() as conn:
            result = conn.execute("UPDATE attempts SET status='unknown' WHERE id=? AND status IN ('reserved','unknown')", (token,))
            if result.rowcount != 1:
                raise BudgetBlocked("attempt_not_pending")

    def _settle(self, token, actual_nano_usd, receipt_sha256, reconcile):
        amount(actual_nano_usd, allow_zero=True)
        if not digest(receipt_sha256):
            raise BudgetBlocked("invalid_receipt")
        with self._transaction() as conn:
            row = conn.execute("SELECT status FROM attempts WHERE id=?", (token,)).fetchone()
            allowed = ("reserved", "unknown") if reconcile else ("reserved",)
            if row is None or row[0] not in allowed:
                raise BudgetBlocked("attempt_not_pending")
            conn.execute("UPDATE attempts SET status='settled',actual=?,receipt=?,reconciled=? WHERE id=?",
                         (actual_nano_usd, receipt_sha256, int(reconcile), token))
            return self._summary(conn)

    def settle(self, token, actual_nano_usd, receipt_sha256):
        return self._settle(token, actual_nano_usd, receipt_sha256, False)

    def reconcile(self, token, actual_nano_usd, evidence_sha256):
        """Only after operator review; never an automatic retry/refund path."""
        return self._settle(token, actual_nano_usd, evidence_sha256, True)

    def invoke_once(self, maximum_nano_usd, send_once):
        """Fake-tested integration seam, NOT a ready-to-use paid provider adapter.

        send_once must not retry internally; returns (actual nano-USD, receipt hash).
        A future adapter must route EACH network attempt through this boundary.
        """
        token = self.reserve(maximum_nano_usd)
        try:
            result = send_once()
            if type(result) is not tuple or len(result) != 2:
                raise BudgetBlocked("invalid_usage")
            state = self.settle(token, result[0], result[1])
        except Exception:
            # If this write also fails, the earlier reserved row still blocks retry.
            self.mark_unknown(token)
            raise BudgetBlocked("outcome_unknown") from None
        if state["estimate_exceeded"]:
            raise BudgetBlocked("cost_estimate_exceeded")
        return state
