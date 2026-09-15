"""Offline operator CLI for real-task preparation, approval and candidate review.

No send command, API-key input, credential loading or model calls. The test
command requires a separate candidate approval and uses the offline sandbox.
"""

import argparse
import json
from pathlib import Path
import time

from preview_demo_patch import read_proposal
from preview_demo_real_task import RealTask, protocol
from preview_demo_task import directory
import preview_demo_preflight as preflight


Blocked = preflight.Blocked


class Parser(argparse.ArgumentParser):
    def error(self, message):
        # argparse normally echoes unknown arguments (possibly secrets/paths).
        raise Blocked("real_cli_arguments")


def read_price(path):
    raw = read_proposal(path)
    if len(raw) > 4096:
        raise Blocked("real_cli_price_size")
    data = protocol.strict_json(raw)
    keys = {"model", "input_nano_usd_per_million", "cached_nano_usd_per_million",
            "output_nano_usd_per_million", "fx_micro_cny_per_usd", "valid_from", "valid_until", "source"}
    if not isinstance(data, dict) or set(data) != keys:
        raise Blocked("real_cli_price_schema")
    price = protocol.PricingSnapshot(**data)
    price.validate()
    return price


def read_reference(path):
    raw = read_proposal(path)
    if len(raw) > 258:
        raise Blocked("real_cli_reference_size")
    # Accept a text editor's final line ending, not embedded control characters.
    return raw.decode("utf-8").removesuffix("\n").removesuffix("\r")


def make_parser():
    parser = Parser(description=__doc__, allow_abbrev=False)
    commands = parser.add_subparsers(dest="action", required=True)
    for action in ("prepare", "inspect", "plan", "approve", "review", "test"):
        command = commands.add_parser(action, allow_abbrev=False)
        command.add_argument("--root", required=True, type=Path, help="existing operator-controlled private root")
        command.add_argument("--task-id", required=True, help="32 lowercase hex characters; never reuse Fake IDs")
        if action == "prepare":
            command.add_argument("--source", type=Path, default=preflight.DEFAULT_SOURCE)
            command.add_argument("--task-file", type=Path, required=True)
            command.add_argument("--price-file", type=Path, required=True, help="explicit reviewed JSON, never a live price lookup")
            command.add_argument("--rmb-fen", type=int, required=True, help="integer budget in fen; no implicit paid consent")
            command.add_argument("--max-output-tokens", type=int, default=4096)
            command.add_argument("--timeout", type=int, default=60)
        elif action == "plan":
            command.add_argument("--show-payload", action="store_true", help="private exact request; do not publish output")
        elif action == "approve":
            command.add_argument("--approve-plan-sha256", required=True)
            command.add_argument("--reference-file", type=Path, required=True, help="private confirmation reference, not a key")
        elif action == "test":
            command.add_argument("--approve-candidate-sha256", required=True)
            command.add_argument("--image", default="golang:1.22", help="trusted locally cached image; never pulled")
            command.add_argument("--timeout", type=int, choices=range(1, 121), default=90)
    return parser


def status(archive):
    summary = archive.summary()
    proposal = archive.proposal(required=False)
    candidate = None
    if proposal is not None:
        _, _, journal, metadata = proposal
        candidate = {**metadata, "evidence": journal.summary()}
    return {**summary, "candidate": candidate}


def main(argv=None):
    report = {"schemaVersion": "forgeflow.demo.real-cli/v1", "modelCalls": 0,
              "paidExecutionEnabled": False, "readyForPaidExecution": False,
              "sandboxAttempted": False, "sandboxExecuted": False}
    archive = None
    try:
        args = make_parser().parse_args(argv)
        if args.action == "prepare":
            # Validate root/ID first; never create parents or overwrite archives.
            RealTask(args.root, args.task_id)
            source, root = directory(args.source), directory(args.root)
            if root == source or source in root.parents:
                raise Blocked("task_root_inside_source")
            captured = preflight.snapshot(source)
            text = read_proposal(args.task_file)
            price = read_price(args.price_file)
            archive = RealTask.create(root, args.task_id, captured, text, price,
                                      rmb_fen=args.rmb_fen, max_output_tokens=args.max_output_tokens,
                                      timeout_seconds=args.timeout)
        else:
            archive = RealTask.open(args.root, args.task_id)
        if args.action == "approve":
            archive.approve(args.approve_plan_sha256, read_reference(args.reference_file))
        elif args.action == "review":
            report["review"] = archive.proposal()[1]
        elif args.action == "test":
            candidate, _, journal, _ = archive.proposal()
            def run():
                report["sandboxAttempted"] = True
                result = preflight.sandbox(candidate, args.image, args.timeout)
                report["sandboxExecuted"] = True
                return result
            report["execution"] = journal.run_once(args.approve_candidate_sha256, args.image, args.timeout, run)
        report.update(status(archive), checksPassed=True)
        if args.action in ("prepare", "plan"):
            report["plan"] = archive.plan()
            prepared = archive.prepared()
            try:
                prepared.pricing.can_start(int(time.time()), prepared.timeout_seconds)
                report["priceWindowValid"] = True
            except Blocked:
                report["priceWindowValid"] = False
            if args.action == "plan" and args.show_payload:
                if protocol.sha(prepared.body) != report["plan"]["bodySha256"]:
                    raise Blocked("real_plan_binding_mismatch")
                report["payload"] = protocol.strict_json(prepared.body)
        if args.action == "test":
            report["checksPassed"] = report["execution"]["passed"]
    except Blocked as error:
        report.update(checksPassed=False, reason=str(error))
        if error.container_name:
            report["cleanupContainerName"] = error.container_name
    except (OSError, ValueError, TypeError, RecursionError):
        report.update(checksPassed=False, reason="real_cli_failed")
    except KeyboardInterrupt:
        report.update(checksPassed=False, reason="interrupted")
    if not report["checksPassed"] and archive is not None:
        try:
            report.update(status(archive))
        except Blocked:
            report["archiveUnavailable"] = True
    print(json.dumps(report, ensure_ascii=True, indent=2))
    return 0 if report["checksPassed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
