import importlib.util
import json
import os
import stat
import tempfile
import unittest
from decimal import Decimal
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).with_name("personal_candidate_smoke.py")
SPEC = importlib.util.spec_from_file_location("personal_candidate_smoke", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


def passing_summary():
    result = {
        "mode": "planner_developer",
        "cases": 1,
        "passed": 1,
        "failures": {},
        "completionRate": 1,
        "hiddenTestPassRate": 1,
        "regressionRate": 0,
        "humanInterventionRate": 0,
        "averageCostUsd": 0.004,
        "p95LatencyMs": 2500,
    }
    baseline = dict(result, promptVersion="developer/v1")
    candidate = dict(result, promptVersion="developer/v4")
    return {
        "schemaVersion": "forgeflow.eval.smoke-campaign/v1",
        "promotionEligible": False,
        "campaignId": "personal-preview-20260923-01",
        "currentPromptVersion": "developer/v1",
        "candidatePromptVersion": "developer/v4",
        "casesPerPrompt": 1,
        "mode": "planner_developer",
        "observations": 2,
        "campaignCostUsd": 0.008,
        "results": [baseline, candidate],
    }


class PersonalCandidateSmokeTests(unittest.TestCase):
    def review(self, summary):
        return MODULE.review_summary(
            summary,
            campaign_id="personal-preview-20260923-01",
            candidate_git_commit="a" * 40,
            max_campaign_usd=Decimal("0.10"),
        )

    def test_passing_pair_is_go_and_not_promotion_evidence(self):
        decision, allowed = self.review(passing_summary())
        self.assertTrue(allowed)
        self.assertEqual(decision["result"], "GO")
        self.assertFalse(decision["promotionEligible"])
        self.assertEqual(decision["reasons"], [])

    def test_baseline_failure_is_no_go_with_fixed_reason(self):
        summary = passing_summary()
        summary["results"][0]["passed"] = 0
        summary["results"][0]["failures"] = {"patch_check": 1}
        decision, allowed = self.review(summary)
        self.assertFalse(allowed)
        self.assertIn("baseline_failed", decision["reasons"])
        self.assertEqual(decision["failureCounts"]["patch_check"], 1)

    def test_candidate_metric_failure_is_no_go(self):
        summary = passing_summary()
        summary["results"][1]["hiddenTestPassRate"] = 0
        decision, allowed = self.review(summary)
        self.assertFalse(allowed)
        self.assertIn("metrics_failed", decision["reasons"])

    def test_boolean_counts_and_non_numeric_metrics_are_no_go(self):
        summary = passing_summary()
        summary["casesPerPrompt"] = True
        summary["results"][1]["passed"] = True
        summary["results"][1]["p95LatencyMs"] = "private error text"
        decision, allowed = self.review(summary)
        self.assertFalse(allowed)
        self.assertIn("observation_count_mismatch", decision["reasons"])
        self.assertIn("candidate_failed", decision["reasons"])
        self.assertIn("metrics_failed", decision["reasons"])
        self.assertNotIn("private error text", json.dumps(decision))

    def test_budget_overrun_is_no_go(self):
        summary = passing_summary()
        summary["campaignCostUsd"] = 0.100001
        decision, allowed = self.review(summary)
        self.assertFalse(allowed)
        self.assertIn("budget_exceeded", decision["reasons"])

    def test_execution_error_decision_is_sanitized_no_go(self):
        decision = MODULE.execution_error_decision(
            campaign_id="personal-preview-20260923-01",
            candidate_git_commit="a" * 40,
            max_campaign_usd=Decimal("0.10"),
        )
        self.assertEqual(decision["result"], "NO-GO")
        self.assertEqual(decision["reasons"], ["execution_error"])
        self.assertIsNone(decision["campaignCostUsd"])
        self.assertFalse(decision["promotionEligible"])

    def test_unknown_failure_label_is_rejected_without_copying_text(self):
        summary = passing_summary()
        summary["results"][1]["failures"] = {"secret-provider-message": 1}
        decision, allowed = self.review(summary)
        self.assertFalse(allowed)
        serialized = json.dumps(decision)
        self.assertNotIn("secret-provider-message", serialized)
        self.assertIn("invalid_summary", decision["reasons"])

    def test_duplicate_json_key_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "summary.json")
            path.write_text('{"schemaVersion":"one","schemaVersion":"two"}', encoding="utf-8")
            with self.assertRaises(MODULE.ReviewError):
                MODULE._load_summary(path)

    def test_private_writer_refuses_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "decision.json")
            path.write_text("existing", encoding="utf-8")
            with self.assertRaises(MODULE.ReviewError):
                MODULE._write_private_json(path, {"result": "GO"})
            self.assertEqual(path.read_text(encoding="utf-8"), "existing")

    def test_private_writer_does_not_overwrite_concurrent_target(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "decision.json")
            original_link = os.link

            def create_target_before_link(source, destination):
                Path(destination).write_text("concurrent writer", encoding="utf-8")
                return original_link(source, destination)

            with mock.patch.object(MODULE.os, "link", side_effect=create_target_before_link):
                with self.assertRaisesRegex(MODULE.ReviewError, "decision_output_exists"):
                    MODULE._write_private_json(path, {"result": "GO"})
            self.assertEqual(path.read_text(encoding="utf-8"), "concurrent writer")
            self.assertEqual(list(Path(directory).glob(".decision-*.tmp")), [])

    def test_private_writer_is_atomic_and_owner_only_on_posix(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "decision.json")
            MODULE._write_private_json(path, {"result": "GO"})
            self.assertEqual(json.loads(path.read_text(encoding="utf-8"))["result"], "GO")
            if os.name == "posix":
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)


if __name__ == "__main__":
    unittest.main()
