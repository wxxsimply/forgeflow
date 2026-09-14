"""Offline demo patch review. No model, credentials, host commands or source writes.

Review first; --sandbox requires the exact --approve-sha256 from that review.
The approval flag is a local operator decision, not an identity signature.
"""

import argparse
import difflib
import hashlib
import json
import os
from pathlib import Path
import re
import stat

import preview_demo_preflight as preflight


EDITABLE = frozenset(("greeting.go", "greeting_test.go"))
MAX_PROPOSAL_BYTES = 1024 * 1024
SCHEMA = "forgeflow.demo.patch/v1"
KEYS = {"schemaVersion", "taskId", "taskSha256", "baseSnapshotSha256", "files"}
Blocked = preflight.Blocked


def is_digest(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def read_proposal(path):
    """Read one bounded regular file from an operator-controlled directory."""
    path = Path(path)
    if ".." in path.parts:
        raise Blocked("unsafe_proposal_path")
    path = path.absolute()
    try:
        for parent in (path, *path.parents):
            if preflight.linked(parent.lstat()):
                raise Blocked("unsafe_proposal_path")
        before = path.lstat()
        if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1:
            raise Blocked("unsafe_proposal_path")
        if before.st_size > MAX_PROPOSAL_BYTES:
            raise Blocked("proposal_too_large")
        flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_BINARY", 0)
        with os.fdopen(os.open(path, flags), "rb") as stream:
            opened = os.fstat(stream.fileno())
            raw = stream.read(MAX_PROPOSAL_BYTES + 1)
            after = os.fstat(stream.fileno())
        current = path.lstat()
        signature = lambda s: (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns)
        if (signature(before) != signature(opened) or signature(opened) != signature(after)
                or signature(after) != signature(current)
                or before.st_ctime_ns != current.st_ctime_ns or opened.st_ctime_ns != after.st_ctime_ns):
            raise Blocked("proposal_changed")
        if len(raw) > MAX_PROPOSAL_BYTES:
            raise Blocked("proposal_too_large")
        return raw
    except OSError:
        raise Blocked("proposal_unreadable") from None


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise Blocked("duplicate_json_key")
        result[key] = value
    return result


def validate_snapshot(captured):
    if not isinstance(captured, dict) or set(captured) != set(preflight.FILES):
        raise Blocked("invalid_snapshot")
    if any(type(value) is not bytes for value in captured.values()):
        raise Blocked("invalid_snapshot")
    if sum(map(len, captured.values())) > preflight.MAX_BYTES:
        raise Blocked("snapshot_too_large")
    for value in captured.values():
        try:
            value.decode("utf-8")
        except UnicodeDecodeError:
            raise Blocked("non_utf8_file") from None
        if b"\0" in value:
            raise Blocked("binary_file")
    if captured["go.mod"].decode().split() != ["module", "forgeflow-preview-demo", "go", "1.22"]:
        raise Blocked("module_contract_changed")


def review(captured, raw, task_id, task_sha256):
    """Validate whole-file replacements; return a copied candidate and review.

    Never interprets unified patches, paths, commands or approval from a model.
    The trusted caller supplies the task identity separately from the proposal.
    """
    validate_snapshot(captured)
    if (not isinstance(task_id, str) or not re.fullmatch(r"[0-9a-f]{32}", task_id)
            or not is_digest(task_sha256)):
        raise Blocked("invalid_task_binding")
    if type(raw) is not bytes or len(raw) > MAX_PROPOSAL_BYTES:
        raise Blocked("proposal_too_large")
    try:
        proposal = json.loads(raw.decode("utf-8"), object_pairs_hook=unique_object)
    except (UnicodeDecodeError, ValueError, RecursionError):
        raise Blocked("invalid_proposal_json") from None
    if not isinstance(proposal, dict) or set(proposal) != KEYS or proposal["schemaVersion"] != SCHEMA:
        raise Blocked("invalid_proposal_schema")
    base = preflight.manifest(captured)["snapshotSha256"]
    if proposal["taskId"] != task_id or proposal["taskSha256"] != task_sha256:
        raise Blocked("task_mismatch")
    if proposal["baseSnapshotSha256"] != base:
        raise Blocked("base_snapshot_mismatch")
    replacements = proposal["files"]
    if not isinstance(replacements, dict) or not replacements or not set(replacements) <= EDITABLE:
        raise Blocked("disallowed_files")
    candidate = dict(captured)
    for name, content in replacements.items():
        if not isinstance(content, str):
            raise Blocked("invalid_replacement")
        try:
            candidate[name] = content.encode("utf-8")
        except UnicodeEncodeError:
            raise Blocked("invalid_replacement") from None
        if not candidate[name].strip():
            raise Blocked("empty_replacement")
    validate_snapshot(candidate)
    changed = sorted(name for name in EDITABLE if captured[name] != candidate[name])
    if not changed:
        raise Blocked("no_changes")
    # Hash semantic content, not JSON whitespace. Bind even unchanged base files.
    binding = {"schemaVersion": SCHEMA, "taskId": task_id, "taskSha256": task_sha256,
               "baseSnapshotSha256": base,
               "candidateSnapshotSha256": preflight.manifest(candidate)["snapshotSha256"]}
    approval = hashlib.sha256(json.dumps(binding, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    diff = []
    for name in changed:
        for line in difflib.unified_diff(captured[name].decode().splitlines(keepends=True),
                                         candidate[name].decode().splitlines(keepends=True),
                                         fromfile="a/" + name, tofile="b/" + name):
            diff.append(line if line.endswith("\n") else line + "\n\\ No newline at end of file\n")
    return candidate, {**binding, "approvalSha256": approval, "changedFiles": changed,
                       "candidateBytes": sum(map(len, candidate.values())), "diff": "".join(diff)}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=preflight.DEFAULT_SOURCE)
    parser.add_argument("--proposal", type=Path, required=True)
    parser.add_argument("--task-id", required=True, help="operator-owned 32 lowercase hex task ID")
    parser.add_argument("--task-sha256", required=True, help="SHA-256 of the exact approved task bytes")
    parser.add_argument("--sandbox", action="store_true", help="explicitly test the approved copy, never the original")
    parser.add_argument("--approve-sha256", help="exact review digest after manual code review")
    parser.add_argument("--image", default="golang:1.22-alpine", help="trusted cached image; never pulled")
    parser.add_argument("--timeout-seconds", type=int, choices=range(1, 121), metavar="1..120", default=90)
    args = parser.parse_args(argv)
    report = {"schemaVersion": "forgeflow.demo.patch-review/v1", "modelCalls": 0,
              "paidExecutionEnabled": False, "readyForPaidExecution": False,
              "sandboxRequested": args.sandbox, "sandboxAttempted": False,
              "sandboxExecuted": False, "sourceModified": False}
    try:
        if args.sandbox != (args.approve_sha256 is not None):
            raise Blocked("explicit_patch_approval_required")
        captured = preflight.snapshot(args.source)
        candidate, details = review(captured, read_proposal(args.proposal), args.task_id, args.task_sha256)
        if args.sandbox:
            if not is_digest(args.approve_sha256) or args.approve_sha256 != details["approvalSha256"]:
                raise Blocked("approval_mismatch")
            # Recheck the original before execution. Only captured bytes are run;
            # later original-file changes cannot alter the approved candidate.
            if preflight.snapshot(args.source) != captured:
                raise Blocked("source_changed")
            # Preserve binding even on timeout/cleanup failure, without raw code.
            report.update({key: value for key, value in details.items() if key != "diff"})
            report["sandboxAttempted"] = True
            report["sandbox"] = preflight.sandbox(candidate, args.image, args.timeout_seconds)
            report["sandboxExecuted"] = True
            report["checksPassed"] = report["sandbox"]["passed"]
        else:
            report.update(details, checksPassed=True)
        report["limitations"] = "Local review/test only; not a signature, paid authorization, persistent evidence or feature acceptance."
    except Blocked as error:
        report.update(checksPassed=False, reason=str(error))
        if error.container_name:
            report["containerRequiringInspection"] = error.container_name
    except (OSError, ValueError):
        report.update(checksPassed=False, reason="local_patch_review_failed")
    except KeyboardInterrupt:
        report.update(checksPassed=False, reason="interrupted")
    # JSON escapes control characters from untrusted code, including terminal ESC.
    print(json.dumps(report, ensure_ascii=True, indent=2))
    return 0 if report["checksPassed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
