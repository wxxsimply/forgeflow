"""Offline transport tests: mock HTTPS and local non-network subprocesses only."""

import base64
from dataclasses import replace
import json
from pathlib import Path
import ssl
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import MagicMock, patch

import preview_demo_deepseek as protocol
import preview_demo_https as transport
import preview_demo_task as task


KEY = "synthetic-key-not-a-real-credential"


class HTTPSTransportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        now = int(time.time())
        price = protocol.PricingSnapshot("deepseek-flash", 1_000_000_000, 100_000_000,
                                         2_000_000_000, 7_200_000, now - 1, now + 3600)
        self.archive = task.TaskArchive.create(Path(self.temp.name), "6" * 32,
                                              task.preflight.snapshot(task.preflight.DEFAULT_SOURCE), b"Synthetic offline transport test.")
        self.prepared = protocol.prepare(self.archive, price, now=now, rmb_fen=10_000)

    def envelope(self, **changes):
        value = {"body": base64.b64encode(self.prepared.body).decode(), "key": KEY, "timeout": 60}
        return json.dumps({**value, **changes}).encode()

    def wire(self, *, status=200, body=b"{}", headers=None, read_error=None):
        response = MagicMock()
        response.status = status
        metadata = {"Content-Type": "application/json", "Content-Length": str(len(body))}
        metadata.update(headers or {})
        response.getheader.side_effect = lambda key, default=None: metadata.get(key, default)
        response.read.side_effect = read_error
        response.read.return_value = body
        connection = MagicMock()
        connection.getresponse.return_value = response
        with patch.object(transport.http.client, "HTTPSConnection", return_value=connection) as factory:
            result = transport._post_once(self.prepared.body, KEY, 60)
        return result, factory, connection, response

    def process(self, output=None):
        process = MagicMock()
        process.poll.return_value = 0
        process.returncode = 0
        if output is None:
            output = json.dumps({"body": base64.b64encode(b"{}").decode()}).encode()
        process.communicate.return_value = (output, None)
        return process

    def test_fixed_post_tls_verification_and_no_proxy_or_redirect_options(self):
        result, factory, connection, response = self.wire()
        self.assertEqual(result, {"body": base64.b64encode(b"{}").decode()})
        self.assertEqual(factory.call_count, 1)
        self.assertEqual(factory.call_args.args, ("api.deepseek.com", 443))
        context = factory.call_args.kwargs["context"]
        self.assertTrue(context.check_hostname)
        self.assertEqual(context.verify_mode, ssl.CERT_REQUIRED)
        connection.request.assert_called_once()
        self.assertEqual(connection.request.call_args.args, ("POST", "/chat/completions"))
        self.assertEqual(connection.request.call_args.kwargs["body"], self.prepared.body)
        self.assertEqual(connection.request.call_args.kwargs["headers"]["Authorization"], "Bearer " + KEY)
        self.assertEqual(connection.request.call_args.kwargs["headers"]["Accept-Encoding"], "identity")
        response.read.assert_called_once_with(protocol.MAX_BODY_BYTES + 1)
        connection.close.assert_called_once()

    def test_redirect_auth_limit_and_server_errors_do_not_retry_or_read_body(self):
        for status in (301, 302, 307, 308, 401, 429, 500, 503):
            result, factory, connection, response = self.wire(status=status, headers={"Location": "https://example.invalid/collect"})
            self.assertEqual(result["error"], "https_http_outcome_unknown")
            self.assertEqual(factory.call_count, 1)
            connection.request.assert_called_once()
            response.read.assert_not_called()
            connection.close.assert_called_once()

    def test_oversized_declared_length_stops_before_body_read(self):
        result, _, connection, response = self.wire(headers={"Content-Length": str(protocol.MAX_BODY_BYTES + 1)})
        self.assertEqual(result["error"], "https_response_size")
        response.read.assert_not_called()
        connection.close.assert_called_once()

    def test_unknown_length_body_is_bounded_and_empty_is_rejected(self):
        for body in (b"", b"x" * (protocol.MAX_BODY_BYTES + 1)):
            result, _, _, response = self.wire(body=body, headers={"Content-Length": None})
            self.assertEqual(result["error"], "https_response_size")
            response.read.assert_called_once_with(protocol.MAX_BODY_BYTES + 1)
        self.assertIn("body", self.wire(headers={"Content-Length": None})[0])

    def test_bad_lengths_compression_and_non_json_are_rejected(self):
        for headers, error in (({"Content-Length": "-1"}, "https_response_length"),
                               ({"Content-Length": "3"}, "https_response_length"),
                               ({"Content-Encoding": "gzip"}, "https_response_encoding"),
                               ({"Content-Type": "text/html"}, "https_response_type")):
            result, _, _, _ = self.wire(headers=headers)
            self.assertEqual(result["error"], error)

    def test_tls_dns_and_read_failure_are_redacted_without_retry(self):
        for error in (ssl.SSLCertVerificationError("synthetic-private-detail"), OSError(KEY), TimeoutError(KEY)):
            with patch.object(transport.http.client, "HTTPSConnection", side_effect=error) as factory:
                result = transport._post_once(self.prepared.body, KEY, 60)
            self.assertEqual(result, {"error": "https_transport_unknown"})
            self.assertEqual(factory.call_count, 1)
        result, _, connection, _ = self.wire(read_error=TimeoutError(KEY))
        self.assertEqual(result, {"error": "https_transport_unknown"})
        connection.close.assert_called_once()

    def test_worker_checks_envelope_and_key_before_any_connection(self):
        invalid = (b"", b"{}", b"x" * (transport.MAX_ENVELOPE + 1),
                   self.envelope(key=KEY + "\r\nInjected: header"), self.envelope(timeout=True),
                   self.envelope(body="not base64!"), b'{"key":"one","key":"two"}')
        with patch.object(transport, "_post_once") as post:
            for raw in invalid:
                self.assertEqual(transport.worker(raw), {"error": "https_invalid_worker_input"})
            post.assert_not_called()

    def test_worker_passes_only_validated_body_to_single_post(self):
        with patch.object(transport, "_post_once", return_value={"body": "e30="}) as post:
            self.assertEqual(transport.worker(self.envelope()), {"body": "e30="})
            post.assert_called_once_with(self.prepared.body, KEY, 60)

    def test_changed_body_options_or_identity_cannot_escape_worker(self):
        values = []
        for changes in ({"stream": True}, {"tools": []}, {"model": "other"}, {"thinking": {"type": "enabled"}}):
            values.append({**json.loads(self.prepared.body), **changes})
        changed = json.loads(self.prepared.body)
        data = json.loads(changed["messages"][1]["content"])
        data["taskId"] = "wrong"
        changed["messages"][1]["content"] = json.dumps(data)
        values.append(changed)
        with patch.object(transport, "_post_once") as post:
            for value in values:
                body = protocol.encode(value).encode()
                result = transport.worker(self.envelope(body=base64.b64encode(body).decode()))
                self.assertEqual(result["error"], "https_invalid_worker_input")
            post.assert_not_called()

    def test_parent_rechecks_price_budget_binding_and_key_before_spawn(self):
        variants = (replace(self.prepared, limit_nano_usd=1), replace(self.prepared, reserved_nano_usd=1),
                    replace(self.prepared, task_id="7" * 32), replace(self.prepared, captured=()),
                    replace(self.prepared, timeout_seconds=True),
                    replace(self.prepared, pricing=replace(self.prepared.pricing, valid_until=int(time.time()))))
        with patch.object(transport.subprocess, "Popen") as spawn:
            for prepared in variants:
                with self.assertRaises(task.Blocked):
                    transport.send_once(prepared, KEY)
            for key in (None, "", KEY + "\n"):
                with self.assertRaises(task.Blocked):
                    transport.send_once(self.prepared, key)
            spawn.assert_not_called()
        wire = json.loads(self.prepared.body)
        data = json.loads(wire["messages"][1]["content"])
        data["task"] = "\ud800synthetic-private-input"
        wire["messages"][1]["content"] = protocol.encode(data)
        malformed = replace(self.prepared, body=protocol.encode(wire).encode())
        with patch.object(transport.subprocess, "Popen") as spawn:
            with self.assertRaisesRegex(task.Blocked, "^https_invalid_plan$"):
                transport.send_once(malformed, KEY)
            spawn.assert_not_called()

    def test_private_pipes_hidden_process_and_minimal_environment(self):
        process = self.process()
        with patch.dict(transport.os.environ, {"HTTP_PROXY": "http://example.invalid", "SYNTHETIC_SECRET": KEY, "PYTHONPATH": "untrusted"}), \
                patch.object(transport.subprocess, "Popen", return_value=process) as spawn:
            result = transport.send_once(self.prepared, KEY)
        self.assertEqual(result.body, b"{}")
        self.assertNotIn("{}", repr(result))
        args = spawn.call_args.args[0]
        self.assertEqual(args[:3], [sys.executable, "-I", "-B"])
        self.assertNotIn(KEY, repr(args))
        options = spawn.call_args.kwargs
        self.assertIs(options["shell"], False)
        self.assertEqual(options["stderr"], subprocess.DEVNULL)
        self.assertEqual(options["creationflags"], getattr(subprocess, "CREATE_NO_WINDOW", 0))
        self.assertNotIn("HTTP_PROXY", options["env"])
        self.assertNotIn("PYTHONPATH", options["env"])
        self.assertNotIn(KEY, repr(options["env"]))
        sent = json.loads(process.communicate.call_args_list[0].kwargs["input"])
        self.assertEqual(sent["key"], KEY)
        self.assertEqual(base64.b64decode(sent["body"]), self.prepared.body)
        self.assertEqual(spawn.call_count, 1)
        process.kill.assert_not_called()
        self.assertEqual(self.archive.budget().summary()["attempts"], 0)  # Transport is not a ledger.

    def test_total_timeout_kills_only_child_and_no_retry(self):
        process = self.process()
        process.communicate.side_effect = [subprocess.TimeoutExpired("worker", 1, output=KEY.encode()), (b"", None)]
        process.poll.side_effect = [None, -9]
        with patch.object(transport.subprocess, "Popen", return_value=process) as spawn:
            with self.assertRaisesRegex(task.Blocked, "^https_total_timeout$"):
                transport.send_once(self.prepared, KEY)
        self.assertEqual(spawn.call_count, 1)
        process.kill.assert_called_once()

    def test_cleanup_unconfirmed_is_distinct_and_not_success(self):
        process = self.process()
        process.pid = 12345
        process.communicate.side_effect = subprocess.TimeoutExpired("worker", 1)
        process.poll.return_value = None
        with patch.object(transport.subprocess, "Popen", return_value=process):
            with self.assertRaisesRegex(task.Blocked, "https_cleanup_unconfirmed") as raised:
                transport.send_once(self.prepared, KEY)
        self.assertEqual(raised.exception.worker_pid, 12345)
        process.kill.assert_called_once()

    def test_interruption_also_stops_child(self):
        process = self.process()
        process.communicate.side_effect = [KeyboardInterrupt, (b"", None)]
        process.poll.side_effect = [None, -9]
        with patch.object(transport.subprocess, "Popen", return_value=process):
            with self.assertRaises(KeyboardInterrupt):
                transport.send_once(self.prepared, KEY)
        process.kill.assert_called_once()

    def test_failed_launch_is_redacted(self):
        with patch.object(transport.subprocess, "Popen", side_effect=OSError(KEY)):
            with self.assertRaisesRegex(task.Blocked, "^https_transport_unknown$"):
                transport.send_once(self.prepared, KEY)

    def test_worker_errors_and_corrupt_output_do_not_leak_or_retry(self):
        for output in (b"not JSON", json.dumps({"error": KEY}).encode(), b'{"body":"not base64!"}',
                       b" " * (transport.MAX_ENVELOPE + 1)):
            with patch.object(transport.subprocess, "Popen", return_value=self.process(output)) as spawn:
                with self.assertRaises(task.Blocked) as raised:
                    transport.send_once(self.prepared, KEY)
                self.assertNotIn(KEY, str(raised.exception))
                self.assertEqual(spawn.call_count, 1)
        output = json.dumps({"error": "https_http_outcome_unknown"}).encode()
        with patch.object(transport.subprocess, "Popen", return_value=self.process(output)):
            with self.assertRaisesRegex(task.Blocked, "https_http_outcome_unknown"):
                transport.send_once(self.prepared, KEY)

    def test_real_isolated_worker_rejects_invalid_input_without_network(self):
        result = subprocess.run([sys.executable, "-I", "-B", str(Path(transport.__file__).resolve()), "--internal-worker"],
                                input=b"{}", stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10,
                                creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        self.assertEqual(result.returncode, 0)
        self.assertEqual(json.loads(result.stdout), {"error": "https_invalid_worker_input"})
        self.assertEqual(result.stderr, b"")

    def test_real_hung_child_is_reaped_within_supervised_deadline(self):
        original = subprocess.Popen
        children = []
        def spawn(_args, **kwargs):
            # Replace only this test's child with a local sleep: never call HTTPS.
            process = original([sys.executable, "-I", "-B", "-c", "import time; time.sleep(30)"], **kwargs)
            children.append(process)
            return process
        started = time.monotonic()
        with patch.object(transport.subprocess, "Popen", side_effect=spawn):
            with self.assertRaisesRegex(task.Blocked, "https_total_timeout"):
                transport.send_once(replace(self.prepared, timeout_seconds=1), KEY)
        self.assertLess(time.monotonic() - started, 10)
        self.assertEqual(len(children), 1)
        self.assertIsNotNone(children[0].poll())


if __name__ == "__main__":
    unittest.main()
