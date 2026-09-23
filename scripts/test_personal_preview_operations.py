import json
import os
from pathlib import Path
import tempfile
import unittest
from datetime import datetime, timezone
from collections import namedtuple

import personal_preview_operations as operations


DiskUsage = namedtuple("DiskUsage", "total used free")
CURRENT_SHA = "1" * 40
TARGET_SHA = "2" * 40


def env_text(release="preview-current", commit=CURRENT_SHA):
    return "\n".join((
        f"FORGEFLOW_RELEASE={release}",
        f"FORGEFLOW_GIT_COMMIT={commit}",
        "FORGEFLOW_REPOSITORY_PATH=/srv/forgeflow/preview-repositories",
        "FORGEFLOW_PUBLIC_IP=39.102.136.31",
    )) + "\n"


def compose_config():
    common = {
        "FORGEFLOW_WORKFLOW_MODE": "planning",
        "FORGEFLOW_PLANNER_MODE": "mock",
    }
    return {
        "services": {
            "api": {"image": "forgeflow-api:preview-target", "environment": {**common, "FORGEFLOW_HTTP_COOKIE_SECURE": "true", "FORGEFLOW_HTTP_ALLOWED_ORIGINS": "https://39.102.136.31"}},
            "worker": {"image": "forgeflow-worker:preview-target", "environment": {**common, "FORGEFLOW_DOCKER_ENABLED": "false"}},
            "web": {"image": "forgeflow-web:preview-target", "environment": {}},
        }
    }


class FakeReader:
    def __init__(self, release="preview-current", commit=CURRENT_SHA):
        self.release = release
        self.commit = commit

    def __call__(self, command):
        joined = " ".join(str(item) for item in command)
        if command == ["git", "rev-parse", "HEAD"]:
            return self.commit
        if command == ["git", "status", "--porcelain", "--untracked-files=no"]:
            return ""
        if "config --format json" in joined:
            return json.dumps(compose_config())
        if "ps --all --format json" in joined:
            return "\n".join(json.dumps({"Service": name, "State": "running", "Health": "healthy"}) for name in operations.SERVICES)
        if joined.endswith("http://127.0.0.1:8080/healthz"):
            return json.dumps({"status": "ok", "serviceVersion": self.release, "gitCommit": self.commit})
        if joined.endswith("http://127.0.0.1:8080/release.json"):
            return json.dumps({"serviceVersion": self.release, "gitCommit": self.commit})
        if joined.endswith("http://127.0.0.1:9091/readyz"):
            return json.dumps({"status": "ready", "serviceVersion": self.release, "gitCommit": self.commit})
        if " logs " in f" {joined} ":
            return "level=info request completed"
        if "pg_database_size" in joined:
            return "1048576"
        if command[:3] == ["docker", "image", "inspect"]:
            service = command[3].split(":", 1)[0].removeprefix("forgeflow-")
            digest_character = {"api": "a", "worker": "b", "web": "c"}[service]
            return json.dumps([{
                "Id": f"sha256:{digest_character * 64}",
                "Config": {"Labels": {
                    "org.opencontainers.image.version": "preview-target",
                    "org.opencontainers.image.revision": TARGET_SHA,
                }},
            }])
        raise AssertionError(f"unexpected command: {command}")


class OperationsTests(unittest.TestCase):
    def write_env(self, directory, name, text):
        path = Path(directory) / name
        path.write_text(text, encoding="utf-8")
        os.chmod(path, 0o600)
        return path

    def test_observation_contains_only_sanitized_aggregates(self):
        with tempfile.TemporaryDirectory() as directory:
            env = self.write_env(directory, "preview.env", env_text())
            backup = Path(directory) / "backups"
            backup.mkdir()
            dump = backup / "forgeflow-preview-20260922T000000Z.dump"
            dump.write_bytes(b"not-real-backup-data")
            now = datetime.now(timezone.utc)
            os.utime(dump, (now.timestamp(), now.timestamp()))
            report = operations.observe(
                Path(directory), env, backup, directory, FakeReader(), now=now,
                disk_usage=lambda _: DiskUsage(1000, 500, 500),
            )
            self.assertTrue(report["checksPassed"])
            self.assertEqual(report["recentErrorLines"], 0)
            self.assertEqual(report["costBoundary"]["providerCostExpectedUSD"], 0)
            self.assertFalse(report["containsRawLogs"])
            serialized = json.dumps(report)
            self.assertNotIn("request completed", serialized)
            self.assertNotIn("not-real-backup-data", serialized)

    def test_observation_flags_errors_and_stale_backup(self):
        class ErrorReader(FakeReader):
            def __call__(self, command):
                if " logs " in f" {' '.join(str(item) for item in command)} ":
                    return "level=error private-task-text"
                return super().__call__(command)

        with tempfile.TemporaryDirectory() as directory:
            env = self.write_env(directory, "preview.env", env_text())
            report = operations.observe(
                Path(directory), env, Path(directory) / "missing", directory, ErrorReader(),
                disk_usage=lambda _: DiskUsage(1000, 500, 500),
            )
            self.assertFalse(report["checksPassed"])
            self.assertEqual(report["attentionRequired"], ["recent-errors", "backup-freshness"])
            self.assertNotIn("private-task-text", json.dumps(report))

    def test_model_key_or_real_planner_fails_boundary(self):
        config = compose_config()
        config["services"]["worker"]["environment"]["OPENAI_API_KEY"] = "do-not-leak"
        checks = []
        summary = operations.inspect_boundary(config, checks)
        self.assertFalse(checks[0]["passed"])
        self.assertIsNone(summary["providerCostExpectedUSD"])
        self.assertNotIn("do-not-leak", json.dumps(checks))

    def test_observation_rejects_model_key_in_private_env(self):
        with tempfile.TemporaryDirectory() as directory:
            env = self.write_env(directory, "preview.env", env_text() + "OPENAI_API_KEY=do-not-leak\n")
            with self.assertRaisesRegex(ValueError, "must not contain"):
                operations.observe(
                    Path(directory), env, Path(directory) / "backups", directory, FakeReader(),
                    disk_usage=lambda _: DiskUsage(1000, 500, 500),
                )

    def test_observation_rejects_non_private_env_file(self):
        if os.name == "nt":
            self.skipTest("POSIX permission bits are not enforceable on Windows")
        with tempfile.TemporaryDirectory() as directory:
            env = self.write_env(directory, "preview.env", env_text())
            os.chmod(env, 0o644)
            with self.assertRaisesRegex(ValueError, "private regular file"):
                operations.observe(
                    Path(directory), env, Path(directory) / "backups", directory, FakeReader(),
                    disk_usage=lambda _: DiskUsage(1000, 500, 500),
                )

    def test_observation_rejects_symlinked_env_file(self):
        if os.name == "nt":
            self.skipTest("Symlink creation requires elevated Windows permissions")
        with tempfile.TemporaryDirectory() as directory:
            env = self.write_env(directory, "preview.env", env_text())
            linked_env = Path(directory) / "preview-link.env"
            linked_env.symlink_to(env)
            with self.assertRaisesRegex(ValueError, "private regular file"):
                operations.observe(
                    Path(directory), linked_env, Path(directory) / "backups", directory, FakeReader(),
                    disk_usage=lambda _: DiskUsage(1000, 500, 500),
                )

    def test_rollback_plan_verifies_old_image_labels_without_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            current = self.write_env(directory, "current.env", env_text())
            target = self.write_env(directory, "target.env", env_text("preview-target", TARGET_SHA))
            plan = operations.rollback_plan(Path(directory), current, target, FakeReader())
            self.assertTrue(plan["checksPassed"])
            self.assertFalse(plan["executed"])
            self.assertFalse(plan["downMigration"])
            self.assertFalse(plan["volumesRemoved"])
            self.assertEqual(plan["databaseAction"], "compatibility-check-only-before-restart")

    def test_rollback_rejects_same_version_or_mount_change(self):
        with tempfile.TemporaryDirectory() as directory:
            current = self.write_env(directory, "current.env", env_text())
            same = self.write_env(directory, "same.env", env_text())
            with self.assertRaisesRegex(ValueError, "already current"):
                operations.rollback_plan(Path(directory), current, same, FakeReader())
            changed = self.write_env(directory, "changed.env", env_text("preview-target", TARGET_SHA).replace(
                "/srv/forgeflow/preview-repositories", "/tmp/untrusted"))
            with self.assertRaisesRegex(ValueError, "outside the approved roots"):
                operations.rollback_plan(Path(directory), current, changed, FakeReader())

    def test_rollback_rejects_symlinked_env_file(self):
        if os.name == "nt":
            self.skipTest("Symlink creation requires elevated Windows permissions")
        with tempfile.TemporaryDirectory() as directory:
            current = self.write_env(directory, "current.env", env_text())
            target = self.write_env(directory, "target.env", env_text("preview-target", TARGET_SHA))
            linked_current = Path(directory) / "current-link.env"
            linked_current.symlink_to(current)
            with self.assertRaisesRegex(ValueError, "private regular files, not symlinks"):
                operations.rollback_plan(Path(directory), linked_current, target, FakeReader())

    def test_private_report_is_atomic_and_does_not_follow_directory_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            report = {"observedAt": "2026-09-22T15:00:00Z", "checksPassed": True}
            target = operations.write_report(report, Path(directory) / "records")
            self.assertEqual(json.loads(target.read_text(encoding="utf-8")), report)
            if os.name != "nt":
                self.assertEqual(target.stat().st_mode & 0o077, 0)
                link = Path(directory) / "linked"
                link.symlink_to(Path(directory) / "records", target_is_directory=True)
                with self.assertRaisesRegex(ValueError, "symlink"):
                    operations.write_report(report, link)

    def test_env_parser_rejects_duplicates(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self.write_env(directory, "duplicate.env", "FORGEFLOW_RELEASE=a\nFORGEFLOW_RELEASE=b\n")
            with self.assertRaisesRegex(ValueError, "duplicate"):
                operations.parse_env(path)


if __name__ == "__main__":
    unittest.main()
