"""Offline tests: no Docker, SSH, database, credentials or network required."""

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import unittest
import uuid
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("preflight", Path(__file__).with_name("preview_rollout_preflight.py"))
preflight = importlib.util.module_from_spec(spec)
spec.loader.exec_module(preflight)
SHA = "a" * 40


def fixture():
    base = "/srv/forgeflow/app/deploy/personal-preview/compose.yaml"
    public = "/srv/forgeflow/app/deploy/personal-preview/compose.public-ip.yaml"
    infos = {service: {
        "running": True, "health": "healthy", "imageId": "sha256:" + "b" * 64,
        "imageName": "test-only:" + service, "user": "10001:10001",
        "project": preflight.PROJECT, "configFiles": base + "," + public,
        "revision": SHA, "mounts": [], "ports": {},
    } for service in preflight.SERVICES}
    infos["postgres"]["configFiles"] = base
    for service in ("api", "worker"):
        infos[service]["mounts"] = [{"Destination": "/repositories", "Source": preflight.REPOSITORY_ROOT,
                                     "Type": "bind", "RW": service == "worker"}]
    infos["caddy"]["mounts"] = [{"Destination": path, "Type": "volume", "Name": "test-" + path[1:]} for path in ("/data", "/config")]
    infos["caddy"]["ports"] = {
        "443/tcp": [{"HostIp": "0.0.0.0", "HostPort": "443"}],
        "8080/tcp": [{"HostIp": "127.0.0.1", "HostPort": "8080"}],
    }
    return infos


class Reader:
    def __init__(self):
        self.infos = fixture()
        self.calls = []
        self.dirty = ""
        self.source = SHA
        self.fail = False
        self.real_mode = False
        self.counts = dict.fromkeys(("legacyPending", "nonterminalRuns", "unfinishedJobs", "unpublishedOutbox"), 0)

    def __call__(self, command):
        self.calls.append(command)
        if self.fail:
            raise RuntimeError("postgres://SECRET_SHOULD_NOT_LEAK@private")
        if command == ["git", "rev-parse", "HEAD"]:
            return self.source
        if command == ["git", "status", "--porcelain"]:
            return self.dirty
        if command[:2] == ["docker", "inspect"]:
            if preflight.INSPECT in command:
                service = command[-1].removeprefix(preflight.PROJECT + "-").removesuffix("-1")
                return json.dumps(self.infos[service])
            assert preflight.ENV_INSPECT in command
            return "\n".join([
                "FORGEFLOW_WORKFLOW_MODE=" + ("development" if self.real_mode else "planning"),
                "FORGEFLOW_PLANNER_MODE=mock", "FORGEFLOW_DOCKER_ENABLED=false",
                "FORGEFLOW_HTTP_COOKIE_SECURE=true", "FORGEFLOW_HTTP_ALLOWED_ORIGINS=https://39.102.136.31",
            ])
        if command[:2] == ["docker", "exec"]:
            if "psql" in command:
                assert "BEGIN READ ONLY" in command[-1]
                return json.dumps(self.counts)
            if "wget" in command:
                return json.dumps({"gitCommit": SHA})
            if "git" in command:
                return "c" * 40
        raise AssertionError("unexpected or non-read-only command: " + repr(command))


class PreflightTests(unittest.TestCase):
    def test_healthy_before_and_after_are_read_only_and_still_require_approval(self):
        for phase in ("before", "after"):
            reader = Reader()
            report = preflight.audit(reader, SHA, phase)
            self.assertTrue(report["checksPassed"], report["checks"])
            self.assertTrue(report["manualApprovalRequired"])
            self.assertFalse(report["deployed"])
            self.assertTrue(all(command[:2] in (["git", "rev-parse"], ["git", "status"], ["docker", "inspect"], ["docker", "exec"]) for command in reader.calls))

    def test_dirty_and_wrong_source_are_blocked_without_listing_private_files(self):
        reader = Reader()
        reader.dirty = "?? SECRET_FILENAME"
        reader.source = "d" * 40
        report = preflight.audit(reader, SHA, "before")
        self.assertFalse(report["checksPassed"])
        self.assertNotIn("SECRET_FILENAME", json.dumps(report))

    def test_legacy_requests_and_busy_runs_jobs_outbox_each_block(self):
        for key in Reader().counts:
            reader = Reader()
            reader.counts[key] = 1
            self.assertFalse(preflight.audit(reader, SHA, "before")["checksPassed"], key)

    def test_unsafe_runtime_configurations_block(self):
        for change in (
            lambda r: r.infos["api"].update(user="0:0"),
            lambda r: r.infos["worker"].update(health="unhealthy"),
            lambda r: r.infos["caddy"]["ports"]["8080/tcp"][0].update(HostIp="0.0.0.0"),
            lambda r: r.infos["postgres"]["ports"].update({"5432/tcp": [{"HostIp": "0.0.0.0", "HostPort": "5432"}]}),
            lambda r: r.infos["caddy"]["mounts"][0].update(Type="tmpfs"),
            lambda r: r.infos["api"]["mounts"][0].update(RW=True),
            lambda r: r.infos["api"].update(configFiles="compose.bootstrap.yaml"),
            lambda r: setattr(r, "real_mode", True),
        ):
            reader = Reader()
            change(reader)
            self.assertFalse(preflight.audit(reader, SHA, "before")["checksPassed"])

    def test_after_requires_new_repository_and_worker_revision(self):
        reader = Reader()
        for service in ("api", "worker"):
            reader.infos[service]["mounts"][0]["Source"] = "/srv/forgeflow/repositories"
        self.assertTrue(preflight.audit(reader, SHA, "before")["checksPassed"])
        self.assertFalse(preflight.audit(reader, SHA, "after")["checksPassed"])
        reader = Reader()
        reader.infos["worker"]["revision"] = "old"
        self.assertFalse(preflight.audit(reader, SHA, "after")["checksPassed"])

    def test_unrestricted_host_mount_is_never_accepted(self):
        reader = Reader()
        for service in ("api", "worker"):
            reader.infos[service]["mounts"][0]["Source"] = "/"
        self.assertFalse(preflight.audit(reader, SHA, "before")["checksPassed"])

    def test_collection_failure_never_leaks_command_output(self):
        reader = Reader()
        reader.fail = True
        report = preflight.audit(reader, SHA, "before")
        self.assertFalse(report["checksPassed"])
        self.assertNotIn("SECRET_SHOULD_NOT_LEAK", json.dumps(report))

    def test_command_reader_hides_stderr_and_timeouts(self):
        for outcome in (subprocess.CompletedProcess([], 1, "SECRET", "SECRET"), subprocess.TimeoutExpired([], 1)):
            with patch.object(preflight.subprocess, "run", side_effect=outcome if isinstance(outcome, Exception) else None, return_value=outcome):
                with self.assertRaises(RuntimeError) as raised:
                    preflight.command_reader(Path.cwd())(["git", "status", "--porcelain"])
                self.assertNotIn("SECRET", str(raised.exception))

    @unittest.skipUnless(os.environ.get("FORGEFLOW_PREFLIGHT_TEST_IMAGE"), "requires an explicitly selected local Docker image")
    def test_real_docker_templates(self):
        # Never start the container, publish ports or mount local files.
        name = "forgeflow-preflight-test-" + uuid.uuid4().hex
        image = os.environ["FORGEFLOW_PREFLIGHT_TEST_IMAGE"]
        def docker(*args):
            return subprocess.check_output(["docker", *args], text=True, timeout=20).strip()
        docker("image", "inspect", image)  # Fail rather than implicitly pulling.
        container_id = docker("create", "--name", name, "--label", "forgeflow.test=preflight", "--network", "none", "--read-only",
                              "--env", "FORGEFLOW_PLANNER_MODE=mock", "--env", "PRIVATE_SENTINEL=not-a-real-secret",
                              "--entrypoint", "/bin/true", image)
        try:
            info = json.loads(docker("inspect", "--format", preflight.INSPECT, container_id))
            filtered = docker("inspect", "--format", preflight.ENV_INSPECT, container_id)
            self.assertEqual(filtered, "FORGEFLOW_PLANNER_MODE=mock")
            self.assertNotIn("PRIVATE_SENTINEL", json.dumps(info))
        finally:
            docker("rm", "-v", container_id)  # Only this container and its own anonymous volumes.


if __name__ == "__main__":
    unittest.main()
