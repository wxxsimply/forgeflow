"""Internal real-task lifecycle. Offline commands live in preview_demo_real.

execute() can send a paid request after explicit persisted approval. Tests must
replace transport.send_once. Never migrate or reuse a Fake task as a real task.
No credential loading is provided here.
"""

from contextlib import contextmanager
import os
import re
import sqlite3
import time
from types import SimpleNamespace

from preview_demo_budget import BudgetBlocked, BudgetLedger, policy_json
from preview_demo_evidence import EvidenceJournal, encode, initialize_receipt
from preview_demo_patch import review, validate_snapshot
from preview_demo_preflight import Blocked, FILES, SANDBOX_PROFILE, manifest, valid_image_reference
from preview_demo_task import TaskArchive, task_bytes
import preview_demo_deepseek as protocol
import preview_demo_https as transport


PLAN_KEYS = {"taskId", "task", "files", "pricing", "rmbFen", "maxOutputTokens", "timeoutSeconds", "createdAt"}
SANDBOX_RESULT_KEYS = {"passed", "exitCode", "oomKilled", "imageId", "logsCollected"}
SANDBOX_TABLE = """CREATE TABLE real_sandbox(
    id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL,
    plan_sha TEXT NOT NULL,snapshot_sha TEXT NOT NULL,image_ref TEXT NOT NULL,
    image_id TEXT NOT NULL,timeout_seconds INTEGER NOT NULL,profile TEXT NOT NULL,
    checked_at INTEGER NOT NULL)"""


def materialize(document):
    try:
        if not isinstance(document, dict) or set(document) != PLAN_KEYS:
            raise Blocked("real_invalid_plan")
        captured = {name: value.encode("utf-8") for name, value in document["files"].items()}
        text = document["task"].encode("utf-8")
        validate_snapshot(captured)
        task_bytes(text)
        price = protocol.PricingSnapshot(**document["pricing"])
        policy = {"provider": "deepseek", "model": price.model, "max_calls": 1,
                  "max_cost_nano_usd": price.usd_limit(document["rmbFen"]),
                  "pricing_sha256": price.fingerprint(), "task_sha256": protocol.sha(text),
                  "snapshot_sha256": manifest(captured)["snapshotSha256"]}
        policy_json(policy)
        view = SimpleNamespace(task_id=document["taskId"], load=lambda: (captured, text, policy))
        prepared = protocol.prepare(view, price, now=document["createdAt"], rmb_fen=document["rmbFen"],
                                    max_output_tokens=document["maxOutputTokens"], timeout_seconds=document["timeoutSeconds"])
        if not prepared.summary()["fitsBudget"]:
            raise Blocked("real_budget_insufficient")
        return captured, text, policy, prepared
    except (AttributeError, UnicodeError, ValueError, TypeError, RecursionError, BudgetBlocked):
        raise Blocked("real_invalid_plan") from None


class RealTask(TaskArchive):
    @classmethod
    def create(cls, root, task_id, captured, text, pricing, *, rmb_fen, now=None,
               max_output_tokens=4096, timeout_seconds=60):
        task_bytes(text)
        validate_snapshot(captured)
        archive = cls(root, task_id)
        document = {"taskId": task_id, "task": text.decode("utf-8"),
                    "files": {name: captured[name].decode("utf-8") for name in FILES},
                    "pricing": pricing.__dict__, "rmbFen": rmb_fen, "maxOutputTokens": max_output_tokens,
                    "timeoutSeconds": timeout_seconds, "createdAt": int(time.time()) if now is None else now}
        _, _, policy, prepared = materialize(document)
        raw = encode(document).encode()
        if len(raw) > protocol.MAX_BODY_BYTES:
            raise Blocked("real_plan_too_large")
        try:
            archive.path.mkdir(mode=0o700)
        except FileExistsError:
            raise Blocked("task_already_exists") from None
        except OSError:
            raise Blocked("task_directory_unavailable") from None
        try:
            BudgetLedger.create(archive.database, policy)
        except BudgetBlocked:
            raise Blocked("real_initialization_failed") from None
        # Failed initialization intentionally leaves an occupied directory.
        with archive._connection() as conn:
            conn.execute("""CREATE TABLE real_plan(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL,
                location TEXT NOT NULL,document BLOB NOT NULL,digest TEXT NOT NULL)""")
            conn.execute("INSERT INTO real_plan VALUES(1,1,?,?,?)", (os.path.normcase(str(archive.path)), raw, protocol.sha(raw)))
            conn.execute("""CREATE TABLE real_state(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL,
                plan_sha TEXT NOT NULL,reference TEXT,approved_at INTEGER,token TEXT,state TEXT NOT NULL,
                raw BLOB,response_sha TEXT,report TEXT,updated_at INTEGER NOT NULL)""")
            conn.execute("INSERT INTO real_state VALUES(1,1,?,NULL,NULL,NULL,'prepared',NULL,NULL,NULL,?)",
                         (prepared.summary()["planSha256"], document["createdAt"]))
            conn.execute(SANDBOX_TABLE)
        return archive

    def _snapshot(self):
        with self._connection() as conn:
            rows = conn.execute("SELECT id,version,location,length(document),typeof(document),digest FROM real_plan").fetchall()
            if (len(rows) != 1 or rows[0][:3] != (1, 1, os.path.normcase(str(self.path)))
                    or rows[0][4] != "blob" or not 1 <= rows[0][3] <= protocol.MAX_BODY_BYTES):
                raise Blocked("real_plan_binding_mismatch")
            raw = conn.execute("SELECT document FROM real_plan WHERE id=1").fetchone()[0]
            if protocol.sha(raw) != rows[0][5]:
                raise Blocked("real_plan_binding_mismatch")
            document = protocol.strict_json(raw)
            captured, text, policy, prepared = materialize(document)
            if document["taskId"] != self.task_id or encode(document).encode() != raw:
                raise Blocked("real_plan_binding_mismatch")
            if conn.execute("SELECT id,version,body FROM policy").fetchall() != [(1, 1, policy_json(policy))]:
                raise Blocked("real_policy_mismatch")
        return captured, text, policy, prepared, document

    def load(self):
        return self._snapshot()[:3]

    def prepared(self):
        return self._snapshot()[3]

    def plan(self):
        """Reviewed numeric configuration, never source, response or credentials."""
        _, _, _, prepared, document = self._snapshot()
        summary = prepared.summary()
        if self.summary()["planSha256"] != summary["planSha256"]:
            raise Blocked("real_plan_binding_mismatch")
        # These protocol-only defaults predate the governed CLI. Readiness is
        # calculated from current persisted state by the CLI, not frozen here.
        summary.pop("paidExecutionEnabled")
        summary.pop("readyForPaidExecution")
        return {**summary, "pricing": dict(prepared.pricing.__dict__),
                "rmbFen": document["rmbFen"], "createdAt": document["createdAt"], "maxCalls": 1}

    def _sandbox(self, conn, prepared):
        objects = conn.execute("SELECT type,name FROM sqlite_master WHERE name='real_sandbox'").fetchall()
        if not objects:
            return None
        if objects != [("table", "real_sandbox")]:
            raise Blocked("real_sandbox_mismatch")
        rows = conn.execute("""SELECT id,version,plan_sha,snapshot_sha,image_ref,image_id,
            timeout_seconds,profile,checked_at FROM real_sandbox""").fetchall()
        if not rows:
            return None
        if len(rows) != 1:
            raise Blocked("real_sandbox_mismatch")
        row = rows[0]
        expected_plan = prepared.summary()["planSha256"]
        expected_snapshot = manifest(dict(prepared.captured))["snapshotSha256"]
        if (row[0:4] != (1, 1, expected_plan, expected_snapshot)
                or not valid_image_reference(row[4])
                or not isinstance(row[5], str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", row[5])
                or type(row[6]) is not int or not 1 <= row[6] <= 120
                or row[7] != SANDBOX_PROFILE or type(row[8]) is not int):
            raise Blocked("real_sandbox_mismatch")
        return {"state": "passed", "planSha256": row[2], "snapshotSha256": row[3],
                "image": row[4], "imageId": row[5], "timeoutSeconds": row[6],
                "sandboxProfile": row[7], "checkedAt": row[8]}

    def sandbox(self, *, required=False):
        with self._transaction() as (prepared, _, conn):
            receipt = self._sandbox(conn, prepared)
        if receipt is None and required:
            raise Blocked("real_sandbox_not_ready")
        return receipt

    def record_sandbox(self, plan_sha, image, timeout_seconds, result, *, now=None):
        now = int(time.time()) if now is None else now
        if (not valid_image_reference(image) or type(timeout_seconds) is not int
                or not 1 <= timeout_seconds <= 120 or not isinstance(result, dict)
                or set(result) != SANDBOX_RESULT_KEYS or result["passed"] is not True
                or type(result["exitCode"]) is not int or result["exitCode"] != 0
                or result["oomKilled"] is not False or result["logsCollected"] is not False
                or not isinstance(result["imageId"], str)
                or not re.fullmatch(r"sha256:[0-9a-f]{64}", result["imageId"])):
            raise Blocked("real_invalid_sandbox_receipt")
        with self._transaction() as (prepared, ledger, conn):
            prepared.pricing.can_start(now, prepared.timeout_seconds)
            state = self._state(conn, prepared, ledger)
            if state["state"] != "approved":
                raise Blocked("real_sandbox_requires_approval")
            if plan_sha != state["planSha256"] or now < state["approvedAt"]:
                raise Blocked("real_approval_mismatch")
            if state["sandbox"] is not None:
                raise Blocked("real_sandbox_already_recorded")
            conn.execute(SANDBOX_TABLE.replace("CREATE TABLE", "CREATE TABLE IF NOT EXISTS", 1))
            conn.execute("INSERT INTO real_sandbox VALUES(1,1,?,?,?,?,?,?,?)",
                         (plan_sha, manifest(dict(prepared.captured))["snapshotSha256"], image,
                          result["imageId"], timeout_seconds, SANDBOX_PROFILE, now))
        return self.sandbox(required=True)

    @contextmanager
    def _transaction(self):
        _, _, policy, prepared, _ = self._snapshot()
        ledger = BudgetLedger(self.database, policy)
        try:
            with ledger._transaction() as conn:
                yield prepared, ledger, conn
        except (BudgetBlocked, sqlite3.Error, OSError, ValueError, TypeError, RecursionError):
            raise Blocked("real_store_unavailable") from None

    def _state(self, conn, prepared, ledger):
        sizes = conn.execute("SELECT id,version,length(reference),length(raw),typeof(raw),length(report) FROM real_state").fetchall()
        if (len(sizes) != 1 or sizes[0][:2] != (1, 1)
                or (sizes[0][2] is not None and not 1 <= sizes[0][2] <= 256)
                or (sizes[0][3] is not None and (sizes[0][4] != "blob" or not 1 <= sizes[0][3] <= protocol.MAX_BODY_BYTES))
                or (sizes[0][5] is not None and not 1 <= sizes[0][5] <= 4096)):
            raise Blocked("real_invalid_state")
        plan_sha, reference, approved, token, state, raw, response_sha, report, updated = conn.execute(
            "SELECT plan_sha,reference,approved_at,token,state,raw,response_sha,report,updated_at FROM real_state WHERE id=1").fetchone()
        if plan_sha != prepared.summary()["planSha256"] or type(updated) is not int:
            raise Blocked("real_state_binding_mismatch")
        budget = ledger._summary(conn)
        sandbox = self._sandbox(conn, prepared)
        receipt_exists = bool(conn.execute("SELECT name FROM sqlite_master WHERE name='receipt'").fetchone())
        parsed = None
        if state == "prepared":
            if (any(value is not None for value in (reference, approved, token, raw, response_sha, report))
                    or budget["attempts"] or receipt_exists or sandbox is not None):
                raise Blocked("real_invalid_state")
        else:
            if (not isinstance(reference, str) or not reference.strip() or len(reference.encode()) > 256
                    or any(ord(char) < 32 for char in reference) or type(approved) is not int or updated < approved):
                raise Blocked("real_invalid_approval")
            if state == "approved":
                if (any(value is not None for value in (token, raw, response_sha, report))
                        or budget["attempts"] or receipt_exists
                        or (sandbox is not None and sandbox["checkedAt"] < approved)):
                    raise Blocked("real_invalid_state")
            else:
                attempts = conn.execute("SELECT id,reserved,status,actual,receipt,reconciled FROM attempts").fetchall()
                if len(attempts) != 1 or attempts[0][:2] != (token, prepared.reserved_nano_usd) or attempts[0][5] != 0:
                    raise Blocked("real_attempt_mismatch")
                _, _, budget_state, accounted, budget_sha, _ = attempts[0]
                if state in ("running", "unknown"):
                    if (budget_state != ("reserved" if state == "running" else "unknown")
                            or any(value is not None for value in (raw, response_sha, report)) or receipt_exists):
                        raise Blocked("real_invalid_state")
                elif state in ("completed", "rejected", "usage_unknown"):
                    if raw is None or protocol.sha(raw) != response_sha:
                        raise Blocked("real_response_mismatch")
                    try:
                        parsed = protocol.parse_response(raw, prepared)
                    except Blocked:
                        if state != "usage_unknown" or budget_state != "unknown" or report is not None or receipt_exists:
                            raise Blocked("real_response_mismatch") from None
                    else:
                        expected_state = "completed" if parsed.proposal is not None else "rejected"
                        if (state != expected_state or budget_state != "settled" or accounted != parsed.estimated_nano_usd
                                or budget_sha != response_sha or report != encode(parsed.summary())):
                            raise Blocked("real_response_mismatch")
                        if state == "completed":
                            _, details = review(dict(prepared.captured), parsed.proposal, self.task_id, prepared.task_sha256)
                            EvidenceJournal(self.database, details)._read(conn)
                        elif receipt_exists:
                            raise Blocked("real_invalid_state")
                else:
                    raise Blocked("real_invalid_state")
        return {"state": state, "planSha256": plan_sha, "approvalReferenceSha256": protocol.sha(reference.encode()) if reference else None,
                "approvedAt": approved, "updatedAt": updated, "attemptId": token,
                "response": parsed.summary() if parsed else None, "responseSha256": response_sha,
                "budget": budget, "sandbox": sandbox,
                "accountingMode": "conservative_estimate_not_invoice", "paidCLIEnabled": True}

    def summary(self):
        with self._transaction() as (prepared, ledger, conn):
            return {"taskId": self.task_id, "provider": "deepseek", "model": prepared.pricing.model,
                    **self._state(conn, prepared, ledger)}

    def approve(self, plan_sha, reference, *, now=None):
        now = int(time.time()) if now is None else now
        if (not isinstance(reference, str) or not reference.strip() or len(reference.encode()) > 256
                or any(ord(char) < 32 for char in reference)):
            raise Blocked("real_invalid_approval")
        with self._transaction() as (prepared, ledger, conn):
            prepared.pricing.can_start(now, prepared.timeout_seconds)
            state = self._state(conn, prepared, ledger)
            if state["state"] != "prepared":
                raise Blocked("real_approval_already_recorded")
            if plan_sha != state["planSha256"] or now < state["updatedAt"]:
                raise Blocked("real_approval_mismatch")
            conn.execute("UPDATE real_state SET state='approved',reference=?,approved_at=?,updated_at=? WHERE id=1", (reference, now, now))

    def _begin(self, plan_sha, now):
        with self._transaction() as (prepared, ledger, conn):
            prepared.pricing.can_start(now, prepared.timeout_seconds)
            state = self._state(conn, prepared, ledger)
            if state["state"] != "approved":
                raise Blocked("real_not_approved_or_consumed")
            if plan_sha != state["planSha256"] or now < state["approvedAt"]:
                raise Blocked("real_approval_mismatch")
            if state["sandbox"] is None or state["sandbox"]["checkedAt"] < state["approvedAt"]:
                raise Blocked("real_sandbox_not_ready")
            token = ledger._reserve_in_transaction(conn, prepared.reserved_nano_usd)
            conn.execute("UPDATE real_state SET state='running',token=?,updated_at=? WHERE id=1", (token, now))
        return prepared, token

    def _unknown(self, token):
        with self._transaction() as (prepared, ledger, conn):
            state = self._state(conn, prepared, ledger)
            if state["state"] != "running" or state["attemptId"] != token:
                raise Blocked("real_invalid_transition")
            conn.execute("UPDATE attempts SET status='unknown' WHERE id=?", (token,))
            conn.execute("UPDATE real_state SET state='unknown',updated_at=? WHERE id=1", (max(int(time.time()), state["updatedAt"]),))

    def _save(self, token, response):
        if not isinstance(response, transport.HTTPResult) or response.status != 200 or type(response.body) is not bytes or not 1 <= len(response.body) <= protocol.MAX_BODY_BYTES:
            raise Blocked("real_invalid_transport_result")
        with self._transaction() as (prepared, ledger, conn):
            state = self._state(conn, prepared, ledger)
            if state["state"] != "running" or state["attemptId"] != token:
                raise Blocked("real_invalid_transition")
            raw = response.body
            digest = protocol.sha(raw)
            report = None
            try:
                parsed = protocol.parse_response(raw, prepared)
            except Blocked:
                outcome = "usage_unknown"
                conn.execute("UPDATE attempts SET status='unknown' WHERE id=?", (token,))
            else:
                outcome = "completed" if parsed.proposal is not None else "rejected"
                report = encode(parsed.summary())
                # This is conservative accounting at the reviewed rates, NOT a
                # verified invoice. Invalid proposals still consume known usage.
                ledger._settle_in_transaction(conn, token, parsed.estimated_nano_usd, digest)
                if parsed.proposal is not None:
                    _, details = review(dict(prepared.captured), parsed.proposal, self.task_id, prepared.task_sha256)
                    initialize_receipt(conn, details)
            conn.execute("UPDATE real_state SET state=?,raw=?,response_sha=?,report=?,updated_at=? WHERE id=1",
                         (outcome, raw, digest, report, max(int(time.time()), state["updatedAt"])))

    def execute(self, api_key, plan_sha):
        """Internal paid seam. No real calls are permitted in this module's tests."""
        transport.validate_key(api_key)
        prepared, token = self._begin(plan_sha, int(time.time()))
        try:
            response = transport.send_once(prepared, api_key)
        except BaseException as error:
            try:
                self._unknown(token)
            except Blocked:
                pass
            if isinstance(error, (KeyboardInterrupt, SystemExit)):
                error.model_call_attempted = True
                raise
            failure = Blocked("real_transport_unknown")
            failure.model_call_attempted = True
            if type(getattr(error, "worker_pid", None)) is int:
                failure.worker_pid = error.worker_pid
            raise failure from None
        try:
            self._save(token, response)
        except BaseException as error:
            try:
                self._unknown(token)
            except Blocked:
                pass
            if isinstance(error, (KeyboardInterrupt, SystemExit)):
                error.model_call_attempted = True
                raise
            failure = Blocked("real_result_not_saved")
            failure.model_call_attempted = True
            raise failure from None
        try:
            return self.summary()
        except BaseException as error:
            error.model_call_attempted = True
            raise

    def proposal(self, *, required=True):
        state = self.summary()
        if state["state"] != "completed":
            if not required:
                return None
            raise Blocked("real_proposal_unavailable")
        prepared = self.prepared()
        with self._connection() as conn:
            raw = conn.execute("SELECT raw FROM real_state WHERE id=1").fetchone()[0]
        parsed = protocol.parse_response(raw, prepared)
        candidate, details = review(dict(prepared.captured), parsed.proposal, self.task_id, prepared.task_sha256)
        return candidate, details, EvidenceJournal.open(self.database, details), {"origin": "deepseek_response", "rawSha256": protocol.sha(raw)}

    def propose(self, raw):
        raise Blocked("real_manual_import_disabled")
