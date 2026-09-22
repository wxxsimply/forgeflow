#!/usr/bin/env python3
"""Create a sanitized GO/NO-GO decision from a private candidate smoke summary."""

from __future__ import annotations

import argparse
import json
import os
import re
import stat
import sys
import tempfile
from decimal import Decimal, InvalidOperation
from pathlib import Path
from typing import Any

MAX_SUMMARY_BYTES = 1024 * 1024
ALLOWED_FAILURES = {
    "patch_check",
    "patch_apply",
    "timeout",
    "model_output_invalid",
    "other",
}


class ReviewError(ValueError):
    """Raised when private evidence does not satisfy the public review contract."""


def _unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ReviewError("duplicate_json_key")
        result[key] = value
    return result


def _decimal(value: Any, label: str) -> Decimal:
    if isinstance(value, bool):
        raise ReviewError(f"invalid_{label}")
    try:
        parsed = Decimal(str(value))
    except (InvalidOperation, ValueError) as exc:
        raise ReviewError(f"invalid_{label}") from exc
    if not parsed.is_finite():
        raise ReviewError(f"invalid_{label}")
    return parsed


def _load_summary(path: Path) -> dict[str, Any]:
    if path.is_symlink() or not path.is_file():
        raise ReviewError("invalid_summary_path")
    size = path.stat().st_size
    if size <= 0 or size > MAX_SUMMARY_BYTES:
        raise ReviewError("invalid_summary_size")
    try:
        value = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=_unique_object)
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise ReviewError("invalid_summary_json") from exc
    if not isinstance(value, dict):
        raise ReviewError("invalid_summary_shape")
    return value


def _metric_is(result: dict[str, Any], name: str, expected: Decimal) -> bool:
    try:
        return _decimal(result.get(name), name) == expected
    except ReviewError:
        return False


def _metric_nonnegative(result: dict[str, Any], name: str) -> bool:
    try:
        return _decimal(result.get(name), name) >= 0
    except ReviewError:
        return False


def _exact_int(value: Any, expected: int) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value == expected


def review_summary(
    summary: dict[str, Any],
    *,
    campaign_id: str,
    candidate_git_commit: str,
    max_campaign_usd: Decimal,
) -> tuple[dict[str, Any], bool]:
    reasons: list[str] = []
    if summary.get("schemaVersion") != "forgeflow.eval.smoke-campaign/v1":
        reasons.append("invalid_summary")
    if summary.get("promotionEligible") is not False:
        reasons.append("invalid_summary")
    if summary.get("campaignId") != campaign_id:
        reasons.append("campaign_mismatch")
    if summary.get("currentPromptVersion") != "developer/v1" or summary.get("candidatePromptVersion") != "developer/v4":
        reasons.append("prompt_coverage_mismatch")
    if (
        not _exact_int(summary.get("casesPerPrompt"), 1)
        or summary.get("mode") != "planner_developer"
        or not _exact_int(summary.get("observations"), 2)
    ):
        reasons.append("observation_count_mismatch")

    try:
        campaign_cost = _decimal(summary.get("campaignCostUsd"), "campaign_cost")
    except ReviewError:
        campaign_cost = Decimal("-1")
        reasons.append("invalid_summary")
    if campaign_cost < 0 or campaign_cost > max_campaign_usd:
        reasons.append("budget_exceeded")

    results = summary.get("results")
    by_prompt: dict[str, dict[str, Any]] = {}
    if isinstance(results, list) and len(results) == 2:
        for item in results:
            if isinstance(item, dict) and isinstance(item.get("promptVersion"), str):
                by_prompt[item["promptVersion"]] = item
    if set(by_prompt) != {"developer/v1", "developer/v4"}:
        reasons.append("prompt_coverage_mismatch")

    fixed_failures = {name: 0 for name in sorted(ALLOWED_FAILURES)}
    for prompt, reason in (("developer/v1", "baseline_failed"), ("developer/v4", "candidate_failed")):
        result = by_prompt.get(prompt)
        if result is None:
            continue
        failures = result.get("failures")
        if not isinstance(failures, dict) or any(name not in ALLOWED_FAILURES for name in failures):
            reasons.append("invalid_summary")
            failures = {}
        for name, count in failures.items():
            if isinstance(count, int) and not isinstance(count, bool) and count >= 0:
                fixed_failures[name] += count
            else:
                reasons.append("invalid_summary")

        prompt_failures = sum(
            count
            for count in failures.values()
            if isinstance(count, int) and not isinstance(count, bool) and count >= 0
        )
        passed = _exact_int(result.get("cases"), 1) and _exact_int(result.get("passed"), 1) and prompt_failures == 0
        metrics_passed = (
            _metric_is(result, "completionRate", Decimal("1"))
            and _metric_is(result, "hiddenTestPassRate", Decimal("1"))
            and _metric_is(result, "regressionRate", Decimal("0"))
            and _metric_is(result, "humanInterventionRate", Decimal("0"))
            and _metric_nonnegative(result, "averageCostUsd")
            and _metric_nonnegative(result, "p95LatencyMs")
        )
        if not passed:
            reasons.append(reason)
        if not metrics_passed:
            reasons.append("metrics_failed")

    reasons = sorted(set(reasons))
    go = not reasons
    decision = {
        "schemaVersion": "forgeflow.personal-candidate-smoke-decision/v1",
        "campaignId": campaign_id,
        "candidateGitCommit": candidate_git_commit,
        "baselinePromptVersion": "developer/v1",
        "candidatePromptVersion": "developer/v4",
        "caseId": "feature-01",
        "mode": "planner_developer",
        "observations": 2,
        "maxCampaignUsd": str(max_campaign_usd),
        "campaignCostUsd": str(campaign_cost) if campaign_cost >= 0 else None,
        "result": "GO" if go else "NO-GO",
        "reasons": reasons,
        "failureCounts": fixed_failures,
        "promotionEligible": False,
    }
    return decision, go


def execution_error_decision(
    *, campaign_id: str, candidate_git_commit: str, max_campaign_usd: Decimal
) -> dict[str, Any]:
    return {
        "schemaVersion": "forgeflow.personal-candidate-smoke-decision/v1",
        "campaignId": campaign_id,
        "candidateGitCommit": candidate_git_commit,
        "baselinePromptVersion": "developer/v1",
        "candidatePromptVersion": "developer/v4",
        "caseId": "feature-01",
        "mode": "planner_developer",
        "observations": 2,
        "maxCampaignUsd": str(max_campaign_usd),
        "campaignCostUsd": None,
        "result": "NO-GO",
        "reasons": ["execution_error"],
        "failureCounts": {name: 0 for name in sorted(ALLOWED_FAILURES)},
        "promotionEligible": False,
    }


def _write_private_json(path: Path, value: dict[str, Any]) -> None:
    if path.exists() or path.is_symlink():
        raise ReviewError("decision_output_exists")
    parent = path.parent
    parent.mkdir(parents=True, exist_ok=True)
    if parent.is_symlink():
        raise ReviewError("invalid_decision_directory")
    descriptor, temporary_name = tempfile.mkstemp(prefix=".decision-", suffix=".tmp", dir=parent)
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8", newline="\n") as handle:
            json.dump(value, handle, ensure_ascii=True, indent=2, sort_keys=True)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, stat.S_IRUSR | stat.S_IWUSR)
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--summary", required=True, type=Path)
    parser.add_argument("--decision-output", required=True, type=Path)
    parser.add_argument("--campaign-id", required=True)
    parser.add_argument("--candidate-git-commit", required=True)
    parser.add_argument("--max-campaign-usd", required=True)
    parser.add_argument("--execution-error", action="store_true")
    return parser


def main(argv: list[str] | None = None) -> int:
    arguments = _parser().parse_args(argv)
    if (
        re.fullmatch(r"personal-preview-[0-9]{8}-[0-9]{2}", arguments.campaign_id) is None
        or re.fullmatch(r"[0-9a-f]{40}", arguments.candidate_git_commit) is None
    ):
        print("candidate smoke review blocked: invalid expected identity", file=sys.stderr)
        return 1
    try:
        maximum = _decimal(arguments.max_campaign_usd, "maximum_cost")
        if maximum <= 0 or maximum > Decimal("0.10"):
            raise ReviewError("invalid_maximum_cost")
        if arguments.execution_error:
            decision = execution_error_decision(
                campaign_id=arguments.campaign_id,
                candidate_git_commit=arguments.candidate_git_commit,
                max_campaign_usd=maximum,
            )
            go = False
        else:
            summary = _load_summary(arguments.summary)
            decision, go = review_summary(
                summary,
                campaign_id=arguments.campaign_id,
                candidate_git_commit=arguments.candidate_git_commit,
                max_campaign_usd=maximum,
            )
        _write_private_json(arguments.decision_output, decision)
    except ReviewError as exc:
        print(f"candidate smoke review blocked: {exc}", file=sys.stderr)
        return 1
    print(f"candidate smoke decision: {decision['result']}")
    print(f"private decision: {arguments.decision_output}")
    return 0 if go else 2


if __name__ == "__main__":
    raise SystemExit(main())
