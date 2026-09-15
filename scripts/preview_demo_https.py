"""Internal single-POST HTTPS transport used by the governed real-task CLI.

Future callers MUST commit real authorization and budget before send_once.
This module itself does not authorize, reserve, settle, retry, or load files.
"""

import base64
from dataclasses import dataclass, field
import http.client
import json
import os
from pathlib import Path
import re
import ssl
import subprocess
import sys
import time

# The isolated child ignores PYTHONPATH/user site; allow only this trusted
# repository's sibling modules in addition to the interpreter standard paths.
if __name__ == "__main__":
    sys.path.insert(0, str(Path(__file__).resolve().parent))

import preview_demo_deepseek as protocol
from preview_demo_evidence import encode
from preview_demo_patch import validate_snapshot
from preview_demo_preflight import Blocked, FILES, manifest


HOST = "api.deepseek.com"
PATH = "/chat/completions"
MAX_ENVELOPE = 4 * 1024 * 1024
CLEANUP_SECONDS = 2
WORKER_ERRORS = frozenset(("https_http_outcome_unknown", "https_response_encoding", "https_response_type",
                           "https_response_size", "https_response_length", "https_transport_unknown", "https_invalid_worker_input"))


def validate_key(key):
    if not isinstance(key, str) or not re.fullmatch(r"[a-zA-Z0-9_.-]{16,512}", key):
        raise Blocked("https_invalid_key")


def validate_body(raw):
    """Reject alternative endpoints/tools/message lists even in the worker."""
    data = protocol.strict_json(raw)
    keys = {"model", "stream", "thinking", "max_tokens", "response_format", "messages"}
    if (not isinstance(data, dict) or set(data) != keys or not isinstance(data["model"], str)
            or data["model"] not in protocol.MODELS or data["stream"] is not False
            or data["thinking"] != {"type": "disabled"} or data["response_format"] != {"type": "json_object"}):
        raise Blocked("https_invalid_body")
    protocol.integer(data["max_tokens"], 1, protocol.MAX_OUTPUT_TOKENS, "https_invalid_body")
    messages = data["messages"]
    if (not isinstance(messages, list) or len(messages) != 2
            or messages[0] != {"role": "system", "content": protocol.SYSTEM}
            or not isinstance(messages[1], dict) or set(messages[1]) != {"role", "content"}
            or messages[1]["role"] != "user" or not isinstance(messages[1]["content"], str)):
        raise Blocked("https_invalid_body")
    user = protocol.strict_json(messages[1]["content"].encode())
    if not isinstance(user, dict) or set(user) != {"taskId", "taskSha256", "baseSnapshotSha256", "task", "files"}:
        raise Blocked("https_invalid_body")
    if (not isinstance(user["taskId"], str) or not re.fullmatch(r"[0-9a-f]{32}", user["taskId"])
            or not isinstance(user["task"], str)):
        raise Blocked("https_invalid_body")
    task = user["task"].encode()
    if not task.strip() or not 1 <= len(task) <= 20_000 or b"\0" in task or protocol.sha(task) != user["taskSha256"]:
        raise Blocked("https_invalid_body")
    if (not isinstance(user["files"], dict) or set(user["files"]) != set(FILES)
            or any(not isinstance(value, str) for value in user["files"].values())):
        raise Blocked("https_invalid_body")
    captured = {name: value.encode() for name, value in user["files"].items()}
    validate_snapshot(captured)
    if manifest(captured)["snapshotSha256"] != user["baseSnapshotSha256"] or encode(data).encode() != raw:
        raise Blocked("https_invalid_body")
    return data, user, captured


def validate_prepared(prepared, now):
    if not isinstance(prepared, protocol.PreparedRequest):
        raise Blocked("https_invalid_plan")
    prepared.pricing.can_start(now, prepared.timeout_seconds)
    data, user, captured = validate_body(prepared.body)
    if (data["model"] != prepared.pricing.model or data["max_tokens"] != prepared.max_output_tokens
            or user["taskId"] != prepared.task_id or user["taskSha256"] != prepared.task_sha256
            or tuple((name, captured[name]) for name in FILES) != prepared.captured
            or prepared.reserved_nano_usd != prepared.pricing.cost(protocol.INPUT_TOKEN_CEILING, 0, prepared.max_output_tokens)):
        raise Blocked("https_plan_mismatch")
    protocol.integer(prepared.limit_nano_usd, 1, 10**19, "https_invalid_plan")
    if prepared.reserved_nano_usd > prepared.limit_nano_usd:
        raise Blocked("https_budget_insufficient")


def _post_once(body, key, timeout):
    """Worker-only network operation; exactly one request, no redirect/retry."""
    connection = None
    try:
        context = ssl.create_default_context()
        context.check_hostname = True
        context.verify_mode = ssl.CERT_REQUIRED
        connection = http.client.HTTPSConnection(HOST, 443, timeout=timeout, context=context)
        connection.request("POST", PATH, body=body, headers={"Authorization": "Bearer " + key,
                           "Content-Type": "application/json", "Accept": "application/json",
                           "Accept-Encoding": "identity", "Connection": "close"})
        response = connection.getresponse()
        if response.status != 200:
            return {"error": "https_http_outcome_unknown"}
        if response.getheader("Content-Encoding", "identity").strip().lower() != "identity":
            return {"error": "https_response_encoding"}
        if response.getheader("Content-Type", "").split(";", 1)[0].strip().lower() != "application/json":
            return {"error": "https_response_type"}
        size = response.getheader("Content-Length")
        if size is not None:
            if not re.fullmatch(r"[0-9]{1,10}", size):
                return {"error": "https_response_length"}
            if int(size) > protocol.MAX_BODY_BYTES:
                return {"error": "https_response_size"}
        raw = response.read(protocol.MAX_BODY_BYTES + 1)
        if not raw or len(raw) > protocol.MAX_BODY_BYTES:
            return {"error": "https_response_size"}
        if size is not None and len(raw) != int(size):
            return {"error": "https_response_length"}
        return {"body": base64.b64encode(raw).decode("ascii")}
    except Exception:
        # Never include provider errors, URLs, headers or credentials in output.
        return {"error": "https_transport_unknown"}
    finally:
        if connection is not None:
            try:
                connection.close()
            except Exception:
                pass


def worker(raw):
    """Bounded private pipe input/output; not a supported user-facing command."""
    try:
        if type(raw) is not bytes or not 1 <= len(raw) <= MAX_ENVELOPE:
            raise ValueError()
        data = json.loads(raw, object_pairs_hook=protocol.unique_object)
        if not isinstance(data, dict) or set(data) != {"body", "key", "timeout"}:
            raise ValueError()
        validate_key(data["key"])
        protocol.integer(data["timeout"], 1, 120, "https_invalid_worker_input")
        body = base64.b64decode(data["body"], validate=True)
        validate_body(body)
    except Exception:
        return {"error": "https_invalid_worker_input"}
    return _post_once(body, data["key"], data["timeout"])


@dataclass(frozen=True)
class HTTPResult:
    body: bytes = field(repr=False)
    status: int = 200


def _stop(process):
    """Reap this exact child only. Never kill a process tree or unrelated PID."""
    if process.poll() is None:
        try:
            process.kill()
        except OSError:
            pass
    try:
        process.communicate(timeout=CLEANUP_SECONDS)
    except (subprocess.TimeoutExpired, OSError):
        error = Blocked("https_cleanup_unconfirmed")
        error.worker_pid = process.pid
        raise error from None
    if process.poll() is None:
        error = Blocked("https_cleanup_unconfirmed")
        error.worker_pid = process.pid
        raise error


def send_once(prepared, api_key):
    """Internal transport, NOT authorization or a budget ledger boundary.

    Must only be called by a future trusted orchestrator AFTER durable paid
    consent and reservation. No current demo CLI calls this function.
    """
    started = time.monotonic()
    try:
        validate_prepared(prepared, int(time.time()))
        validate_key(api_key)
    except (UnicodeError, ValueError, TypeError, RecursionError):
        raise Blocked("https_invalid_plan") from None
    deadline = started + prepared.timeout_seconds
    envelope = encode({"body": base64.b64encode(prepared.body).decode("ascii"),
                       "key": api_key, "timeout": prepared.timeout_seconds}).encode()
    process = None
    try:
        # Secrets are in a private stdin pipe, never argv, environment or files.
        environment = {name: os.environ[name] for name in ("SystemRoot", "WINDIR", "SYSTEMDRIVE", "TEMP", "TMP") if name in os.environ}
        process = subprocess.Popen([sys.executable, "-I", "-B", str(Path(__file__).resolve()), "--internal-worker"],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                   shell=False, close_fds=True, env=environment,
                                   creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise subprocess.TimeoutExpired("private-worker", prepared.timeout_seconds)
        output, _ = process.communicate(input=envelope, timeout=remaining)
        if time.monotonic() >= deadline:
            raise subprocess.TimeoutExpired("private-worker", prepared.timeout_seconds)
        if process.returncode != 0 or not 1 <= len(output) <= MAX_ENVELOPE:
            raise Blocked("https_worker_failed")
        data = json.loads(output)
        if isinstance(data, dict) and set(data) == {"error"} and data["error"] in WORKER_ERRORS:
            raise Blocked(data["error"])
        if not isinstance(data, dict) or set(data) != {"body"}:
            raise Blocked("https_worker_failed")
        body = base64.b64decode(data["body"], validate=True)
        if not 1 <= len(body) <= protocol.MAX_BODY_BYTES:
            raise Blocked("https_response_size")
        return HTTPResult(body)
    except subprocess.TimeoutExpired:
        raise Blocked("https_total_timeout") from None
    except (OSError, ValueError, TypeError, RecursionError):
        raise Blocked("https_transport_unknown") from None
    finally:
        if process is not None:
            _stop(process)


if __name__ == "__main__":
    if sys.argv[1:] != ["--internal-worker"]:
        raise SystemExit("Internal transport only; no paid task CLI is enabled.")
    result = worker(sys.stdin.buffer.read(MAX_ENVELOPE + 1))
    sys.stdout.buffer.write(encode(result).encode())
