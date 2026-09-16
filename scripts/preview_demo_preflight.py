"""Offline-only public demo preflight. Never loads keys or calls a model."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tempfile
import time
import uuid


FILES = ("README.md", "go.mod", "greeting.go", "greeting_test.go")
MAX_BYTES = 128 * 1024
OWNER_LABEL = "forgeflow.demo.preflight"
SANDBOX_PROFILE = "forgeflow.demo.sandbox/v1"
DEFAULT_SOURCE = Path(__file__).resolve().parent.parent / "examples" / "preview-demo"


class Blocked(Exception):
    """Only fixed, non-sensitive reason codes may reach the report."""

    def __init__(self, reason, container_name=None):
        super().__init__(reason)
        self.container_name = container_name


def linked(info):
    return stat.S_ISLNK(info.st_mode) or bool(
        getattr(info, "st_file_attributes", 0) & getattr(stat, "FILE_ATTRIBUTE_REPARSE_POINT", 0x400)
    )


def snapshot(source):
    """Read a bounded flat template; callers must own and not concurrently edit it."""
    source = Path(source)
    if ".." in source.parts:
        raise Blocked("source_traversal")
    root = source.absolute()
    try:
        for parent in (root, *root.parents):
            if linked(parent.lstat()):
                raise Blocked("linked_source")
        if not root.is_dir() or {p.name for p in root.iterdir()} != set(FILES):
            raise Blocked("unexpected_files")
        captured = {}
        total = 0
        for name in FILES:
            path = root / name
            before = path.lstat()
            if linked(before) or not stat.S_ISREG(before.st_mode) or before.st_nlink != 1:
                raise Blocked("non_regular_file")
            if before.st_size > MAX_BYTES - total:
                raise Blocked("snapshot_too_large")
            fd = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_BINARY", 0))
            with os.fdopen(fd, "rb") as stream:
                opened = os.fstat(stream.fileno())
                if (opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino):
                    raise Blocked("source_changed")
                data = stream.read(MAX_BYTES - total + 1)
                after = os.fstat(stream.fileno())
            current = path.lstat()
            # Windows path stat and CRT fstat can disagree about ctime semantics.
            # Compare ctime only within the same API; identity/size/mtime across both.
            signature = lambda s: (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns)
            if (signature(before) != signature(opened) or signature(opened) != signature(after)
                    or signature(after) != signature(current)
                    or before.st_ctime_ns != current.st_ctime_ns or opened.st_ctime_ns != after.st_ctime_ns):
                raise Blocked("source_changed")
            total += len(data)
            if total > MAX_BYTES:
                raise Blocked("snapshot_too_large")
            try:
                data.decode("utf-8")
            except UnicodeDecodeError:
                raise Blocked("non_utf8_file") from None
            if b"\0" in data:
                raise Blocked("binary_file")
            captured[name] = data
        if captured["go.mod"].decode().split() != ["module", "forgeflow-preview-demo", "go", "1.22"]:
            raise Blocked("module_contract_changed")
        if {p.name for p in root.iterdir()} != set(FILES):
            raise Blocked("source_changed")
        return captured
    except OSError:
        raise Blocked("source_unreadable") from None


def manifest(captured):
    rows = [{"name": name, "bytes": len(captured[name]), "sha256": hashlib.sha256(captured[name]).hexdigest()}
            for name in FILES]
    encoded = json.dumps(rows, sort_keys=True, separators=(",", ":")).encode()
    return {"files": rows, "sourceBytes": sum(row["bytes"] for row in rows),
            "snapshotSha256": hashlib.sha256(encoded).hexdigest()}


class Docker:
    def call(self, args, timeout):
        try:
            result = subprocess.run(["docker", *args], capture_output=True, timeout=max(0.01, timeout), check=False)
        except subprocess.TimeoutExpired:
            raise Blocked("docker_command_timeout") from None
        except OSError:
            raise Blocked("docker_unavailable") from None
        if result.returncode or len(result.stdout) > 8192:
            raise Blocked("docker_command_failed")
        try:
            return result.stdout.decode("utf-8").strip()
        except UnicodeDecodeError:
            raise Blocked("docker_invalid_response") from None


def container_args(name, owner, image, directory):
    # Only the copied, already validated four-file snapshot is mounted. No socket,
    # home directory, provider environment, original source or host cache enters.
    return ["create", "--name", name, "--label", f"{OWNER_LABEL}={owner}",
            "--pull", "never", "--network", "none", "--read-only", "--user", "10001:10001",
            "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "128",
            "--memory", "384m", "--memory-swap", "384m", "--cpus", "1", "--log-driver", "none",
            "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=128m",
            "--mount", f"type=bind,source={directory},target=/workspace,readonly", "--workdir", "/workspace",
            "--env", "GOCACHE=/tmp/go-build", "--env", "GOMODCACHE=/tmp/go-mod",
            "--env", "GOPROXY=off", "--env", "GOSUMDB=off", "--env", "GOTOOLCHAIN=local",
            "--entrypoint", "go", image, "test", "-mod=readonly", "-count=1", "-timeout", "30s", "./..."]


def valid_image_reference(image):
    return isinstance(image, str) and bool(re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9._/@:-]{0,255}", image))


def verify_cached_image(image_id, docker=None):
    if not isinstance(image_id, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id):
        raise Blocked("image_not_pinned")
    docker = docker or Docker()
    pinned = docker.call(["image", "inspect", "--format", "{{.Id}}", image_id], 5)
    if pinned != image_id:
        raise Blocked("image_not_pinned")
    return pinned


def sandbox(captured, image, seconds, docker=None):
    if not valid_image_reference(image):
        raise Blocked("invalid_image")
    if not 1 <= seconds <= 120:
        raise Blocked("invalid_timeout")
    docker = docker or Docker()
    deadline = time.monotonic() + seconds
    owner = uuid.uuid4().hex
    name = "forgeflow-demo-preflight-" + owner
    attempted = False

    def call(args):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise Blocked("sandbox_timeout")
        return docker.call(args, min(5, remaining))

    with tempfile.TemporaryDirectory(prefix="forgeflow-demo-") as temp:
        directory = Path(temp) / "snapshot"
        directory.mkdir(mode=0o755)
        if "," in str(directory):
            raise Blocked("unsupported_temp_path")
        for filename in FILES:
            target = directory / filename
            target.write_bytes(captured[filename])
            target.chmod(0o444)
        try:
            pinned = call(["image", "inspect", "--format", "{{.Id}}", image])
            if not isinstance(pinned, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", pinned):
                raise Blocked("image_not_pinned")
            attempted = True  # A timed-out create may still have created a container.
            call(container_args(name, owner, pinned, directory))
            call(["start", name])
            while True:
                try:
                    state = json.loads(call(["inspect", "--format", "{{json .State}}", name]))
                except (ValueError, TypeError):
                    raise Blocked("docker_invalid_state") from None
                if not isinstance(state, dict):
                    raise Blocked("docker_invalid_state")
                status = state.get("Status")
                if status == "exited":
                    code = state.get("ExitCode")
                    if type(code) is not int or type(state.get("OOMKilled")) is not bool:
                        raise Blocked("docker_invalid_state")
                    return {"passed": code == 0 and not state["OOMKilled"], "exitCode": code,
                            "oomKilled": state["OOMKilled"], "imageId": pinned,
                            "logsCollected": False}
                if status not in ("running", "created"):
                    raise Blocked("sandbox_unexpected_state")
                time.sleep(min(0.2, max(0, deadline - time.monotonic())))
        finally:
            if attempted:
                # Never remove an unrelated container, including after create failure.
                try:
                    actual = docker.call(["inspect", "--format", '{{index .Config.Labels "' + OWNER_LABEL + '"}}', name], 5)
                    if actual != owner:
                        raise Blocked("cleanup_owner_mismatch")
                    docker.call(["rm", "--force", name], 5)
                except Blocked:
                    raise Blocked("cleanup_unconfirmed", name) from None


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=DEFAULT_SOURCE, help="flat public four-file template directory")
    parser.add_argument("--sandbox", action="store_true", help="explicitly run offline baseline tests in a local Docker image")
    parser.add_argument("--image", default="golang:1.22-alpine", help="trusted already-cached Go image; never pulled")
    parser.add_argument("--timeout-seconds", type=int, choices=range(1, 121), metavar="1..120", default=90)
    args = parser.parse_args(argv)
    report = {"schemaVersion": "forgeflow.demo.preflight/v1", "modelCalls": 0,
              "paidExecutionEnabled": False, "readyForPaidExecution": False,
              "sandboxRequested": args.sandbox}
    try:
        captured = snapshot(args.source)
        report.update(manifest(captured))
        if args.sandbox:
            report["sandbox"] = sandbox(captured, args.image, args.timeout_seconds)
            report["checksPassed"] = report["sandbox"]["passed"]
        else:
            report["checksPassed"] = True
        report["limitations"] = "Snapshot/baseline only; not model authorization, credential scanning or new-feature acceptance."
    except Blocked as error:
        report.update(checksPassed=False, reason=str(error))
        if error.container_name:
            report["containerRequiringInspection"] = error.container_name
    except (OSError, ValueError):
        report.update(checksPassed=False, reason="local_preflight_failed")
    except KeyboardInterrupt:
        report.update(checksPassed=False, reason="interrupted")
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report["checksPassed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
