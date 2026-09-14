"""Offline budget regression tests: temporary SQLite databases and fake requests."""

from concurrent.futures import ThreadPoolExecutor
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import threading
import types
import unittest
from unittest.mock import patch

from preview_demo_budget import BudgetBlocked, BudgetLedger, MAX_AMOUNT


def policy():
    return {"snapshot_sha256": "a" * 64, "task_sha256": "b" * 64, "pricing_sha256": "c" * 64,
            "provider": "fake", "model": "fake-offline", "max_calls": 3, "max_cost_nano_usd": 1000}


RECEIPT = hashlib.sha256(b"synthetic offline receipt, not real billing").hexdigest()


class BudgetTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "budget.sqlite3"
        self.policy = policy()
        self.ledger = BudgetLedger.create(self.path, self.policy)

    def reopen(self):
        return BudgetLedger.open(self.path, self.policy)

    def test_reserve_is_durable_before_fake_request(self):
        def fake():
            state = self.reopen().summary()
            self.assertEqual((state["attempts"], state["held_nano_usd"]), (1, 500))
            return 100, RECEIPT
        state = self.ledger.invoke_once(500, fake)
        self.assertEqual((state["spent_nano_usd"], state["held_nano_usd"], state["remaining_nano_usd"]), (100, 0, 900))
        self.assertEqual(state, self.reopen().summary())

    def test_three_requests_include_zero_cost_attempts_and_restart(self):
        calls = []
        for _ in range(3):
            self.reopen().invoke_once(10, lambda: (calls.append("fake") or 0, RECEIPT))
        with self.assertRaisesRegex(BudgetBlocked, "call_limit"):
            self.reopen().invoke_once(10, lambda: (calls.append("unexpected") or 0, RECEIPT))
        self.assertEqual(calls, ["fake"] * 3)

    def test_cost_rejected_before_sending_and_boundary_allowed(self):
        self.ledger.invoke_once(600, lambda: (600, RECEIPT))
        calls = []
        with self.assertRaisesRegex(BudgetBlocked, "cost_limit"):
            self.reopen().invoke_once(401, lambda: (calls.append(True) or 1, RECEIPT))
        self.assertEqual(calls, [])
        self.assertEqual(self.ledger.invoke_once(400, lambda: (400, RECEIPT))["remaining_nano_usd"], 0)

    def test_timeout_retains_full_reservation_and_redacts_error(self):
        secret = "private-api-key-and-full-prompt"
        def fake():
            raise TimeoutError(secret)
        with self.assertRaisesRegex(BudgetBlocked, "^outcome_unknown$") as caught:
            self.ledger.invoke_once(800, fake)
        self.assertNotIn(secret, str(caught.exception))
        state = self.reopen().summary()
        self.assertEqual((state["attempts"], state["held_nano_usd"], state["remaining_nano_usd"]), (1, 800, 200))
        with self.assertRaisesRegex(BudgetBlocked, "unresolved_attempt"):
            self.reopen().reserve(1)
        self.assertNotIn(secret.encode(), self.path.read_bytes())

    def test_invalid_or_missing_usage_never_refunds(self):
        for result in (None, (None, RECEIPT), (True, RECEIPT), (-1, RECEIPT), (1.5, RECEIPT), (0, "")):
            with self.subTest(result=result):
                path = Path(self.temp.name) / ("case-" + str(len(list(Path(self.temp.name).iterdir()))) + ".sqlite3")
                ledger = BudgetLedger.create(path, self.policy)
                with self.assertRaisesRegex(BudgetBlocked, "outcome_unknown"):
                    ledger.invoke_once(500, lambda: result)
                self.assertEqual(BudgetLedger.open(path, self.policy).summary()["held_nano_usd"], 500)

    def test_crash_after_reservation_blocks_reopened_ledger(self):
        self.ledger.reserve(500)
        with self.assertRaisesRegex(BudgetBlocked, "unresolved_attempt"):
            self.reopen().reserve(1)
        self.assertEqual(self.reopen().summary()["attempts"], 1)

    def test_abrupt_child_process_exit_preserves_reservation(self):
        code = (
            "import sys,os,json; sys.path.insert(0,sys.argv[1]); "
            "from preview_demo_budget import BudgetLedger; "
            "ledger=BudgetLedger.open(sys.argv[2],json.loads(sys.argv[3])); "
            "ledger.reserve(700); os._exit(0)"
        )
        result = subprocess.run([sys.executable, "-B", "-c", code, str(Path(__file__).parent),
                                 str(self.path), json.dumps(self.policy)], capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0, "offline child failed")
        self.assertEqual(self.reopen().summary()["held_nano_usd"], 700)
        with self.assertRaisesRegex(BudgetBlocked, "unresolved_attempt"):
            self.reopen().reserve(1)

    def test_keyboard_interrupt_retains_reservation(self):
        def fake():
            raise KeyboardInterrupt()
        with self.assertRaises(KeyboardInterrupt):
            self.ledger.invoke_once(500, fake)
        self.assertTrue(self.reopen().summary()["unfinished"])

    def test_explicit_reconciliation_requires_evidence_and_preserves_count(self):
        token = self.ledger.reserve(500)
        self.ledger.mark_unknown(token)
        with self.assertRaisesRegex(BudgetBlocked, "attempt_not_pending"):
            self.ledger.settle(token, 100, RECEIPT)
        with self.assertRaisesRegex(BudgetBlocked, "invalid_receipt"):
            self.ledger.reconcile(token, 100, "no evidence")
        self.assertEqual(self.ledger.summary()["held_nano_usd"], 500)
        state = self.reopen().reconcile(token, 100, RECEIPT)
        self.assertEqual((state["attempts"], state["spent_nano_usd"], state["unfinished"]), (1, 100, False))
        with closing(sqlite3.connect(self.path)) as conn:
            self.assertEqual(conn.execute("SELECT reconciled,receipt FROM attempts").fetchone(), (1, RECEIPT))
        with self.assertRaisesRegex(BudgetBlocked, "attempt_not_pending"):
            self.ledger.reconcile(token, 0, RECEIPT)

    def test_actual_cost_over_estimate_is_saved_and_blocks_future_calls(self):
        with self.assertRaisesRegex(BudgetBlocked, "cost_estimate_exceeded"):
            self.ledger.invoke_once(500, lambda: (1200, RECEIPT))
        state = self.reopen().summary()
        self.assertEqual((state["spent_nano_usd"], state["held_nano_usd"], state["remaining_nano_usd"]), (1200, 0, 0))
        self.assertTrue(state["estimate_exceeded"])
        with self.assertRaisesRegex(BudgetBlocked, "cost_estimate_exceeded"):
            self.reopen().reserve(1)

    def test_duplicate_settlement_cannot_change_cost(self):
        token = self.ledger.reserve(500)
        self.ledger.settle(token, 300, RECEIPT)
        for operation in (lambda: self.ledger.settle(token, 0, RECEIPT), lambda: self.ledger.mark_unknown(token)):
            with self.assertRaisesRegex(BudgetBlocked, "attempt_not_pending"):
                operation()
        self.assertEqual(self.ledger.summary()["spent_nano_usd"], 300)

    def test_configuration_drift_rejected_without_reset(self):
        self.ledger.reserve(500)
        changes = {"snapshot_sha256": "d" * 64, "task_sha256": "e" * 64, "pricing_sha256": "f" * 64,
                   "provider": "deepseek", "model": "different-model", "max_calls": 2, "max_cost_nano_usd": 2000}
        for key, value in changes.items():
            with self.subTest(key=key), self.assertRaisesRegex(BudgetBlocked, "policy_mismatch"):
                BudgetLedger.open(self.path, self.policy | {key: value})
        self.assertEqual(self.ledger.summary()["held_nano_usd"], 500)

    def test_create_refuses_overwrite_missing_open_does_not_create(self):
        before = self.path.read_bytes()
        with self.assertRaisesRegex(BudgetBlocked, "ledger_already_exists"):
            BudgetLedger.create(self.path, self.policy)
        self.assertEqual(before, self.path.read_bytes())
        missing = Path(self.temp.name) / "missing.sqlite3"
        with self.assertRaisesRegex(BudgetBlocked, "ledger_unavailable"):
            BudgetLedger.open(missing, self.policy)
        self.assertFalse(missing.exists())

    def test_invalid_amounts_do_not_consume_calls(self):
        for value in (0, -1, True, 1.5, float("nan"), float("inf"), "100", MAX_AMOUNT + 1):
            with self.subTest(value=value), self.assertRaisesRegex(BudgetBlocked, "invalid_amount"):
                self.ledger.reserve(value)
        self.assertEqual(self.ledger.summary()["attempts"], 0)

    def test_invalid_policy_does_not_create_file(self):
        for change in ({"max_calls": True}, {"max_calls": 4}, {"task_sha256": "bad"}, {"max_cost_nano_usd": 0}):
            target = Path(self.temp.name) / "invalid.sqlite3"
            with self.assertRaises(BudgetBlocked):
                BudgetLedger.create(target, self.policy | change)
            self.assertFalse(target.exists())

    def test_two_connections_cannot_reserve_concurrently(self):
        barrier = threading.Barrier(2)
        def reserve():
            ledger = self.reopen()
            barrier.wait(timeout=5)
            try:
                return ledger.reserve(700)
            except BudgetBlocked as error:
                return str(error)
        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(lambda _: reserve(), range(2)))
        self.assertEqual(results.count("unresolved_attempt"), 1)
        self.assertEqual((self.ledger.summary()["attempts"], self.ledger.summary()["held_nano_usd"]), (1, 700))

    def test_storage_failure_before_send_prevents_fake_call(self):
        calls = []
        with patch("preview_demo_budget.sqlite3.connect", side_effect=sqlite3.OperationalError("secret-path")):
            with self.assertRaisesRegex(BudgetBlocked, "^ledger_unavailable$"):
                self.ledger.invoke_once(100, lambda: (calls.append(True) or 0, RECEIPT))
        self.assertEqual(calls, [])

    def test_reservation_commit_failure_rolls_back_without_sending(self):
        calls = []
        connection = sqlite3.connect(self.path, isolation_level=None)
        def fail_commit():
            raise sqlite3.OperationalError("sensitive-disk-error")
        proxy = types.SimpleNamespace(execute=connection.execute, commit=fail_commit, close=connection.close)
        with patch("preview_demo_budget.sqlite3.connect", return_value=proxy):
            with self.assertRaisesRegex(BudgetBlocked, "^ledger_unavailable$"):
                self.ledger.invoke_once(100, lambda: (calls.append(True) or 0, RECEIPT))
        self.assertEqual(calls, [])
        self.assertEqual(self.reopen().summary()["attempts"], 0)

    def test_settlement_failure_after_send_keeps_reservation(self):
        with patch.object(self.ledger, "settle", side_effect=OSError("secret-storage-error")):
            with self.assertRaisesRegex(BudgetBlocked, "outcome_unknown"):
                self.ledger.invoke_once(500, lambda: (100, RECEIPT))
        self.assertEqual(self.reopen().summary()["held_nano_usd"], 500)

    def test_corrupt_database_is_not_reinitialized(self):
        self.path.write_bytes(b"corrupt-ledger")
        with self.assertRaisesRegex(BudgetBlocked, "ledger_unavailable"):
            self.reopen()
        self.assertEqual(self.path.read_bytes(), b"corrupt-ledger")

    def test_hardlink_and_traversal_rejected(self):
        with self.assertRaisesRegex(BudgetBlocked, "unsafe_ledger_path"):
            BudgetLedger.open(self.path.parent / ".." / self.path.parent.name / self.path.name, self.policy)
        os.link(self.path, self.path.with_name("alias.sqlite3"))
        with self.assertRaisesRegex(BudgetBlocked, "unsafe_ledger_path"):
            self.reopen()


if __name__ == "__main__":
    unittest.main()
