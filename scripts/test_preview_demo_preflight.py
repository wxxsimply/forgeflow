"""No network, Docker daemon, credentials or model required."""

import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import stat
import tempfile
import types
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("demo_preflight", Path(__file__).with_name("preview_demo_preflight.py"))
demo = importlib.util.module_from_spec(spec)
spec.loader.exec_module(demo)
IMAGE = "sha256:" + "a" * 64


class FakeDocker:
    def __init__(self, *, exit_code=0, fail_at=None, owner_matches=True, running=False):
        self.calls = []
        self.exit_code = exit_code
        self.fail_at = fail_at
        self.owner_matches = owner_matches
        self.running = running
        self.owner = None
        self.copied = None

    def call(self, args, timeout):
        self.calls.append(args)
        assert 0 < timeout <= 5
        if args[0] == "create":
            self.owner = args[args.index("--label") + 1].split("=", 1)[1]
            mount = args[args.index("--mount") + 1]
            self.copied = Path(mount.split("source=", 1)[1].split(",target=", 1)[0])
            assert {p.name for p in self.copied.iterdir()} == set(demo.FILES)
        if args[0] == self.fail_at:
            raise demo.Blocked("docker_command_timeout")
        if args[0] == "image":
            return IMAGE
        if args[0] == "inspect" and "Labels" in args[2]:
            return self.owner if self.owner_matches else "someone-else"
        if args[0] == "inspect":
            return json.dumps({"Status": "running" if self.running else "exited", "ExitCode": self.exit_code, "OOMKilled": False})
        return "container"


class DemoPreflightTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "source"
        shutil.copytree(demo.DEFAULT_SOURCE, self.root)

    def test_default_is_read_only_without_docker_or_source_output(self):
        before = demo.snapshot(self.root)
        output = io.StringIO()
        with patch.object(demo, "Docker", side_effect=AssertionError("must not access Docker")), contextlib.redirect_stdout(output):
            self.assertEqual(demo.main(["--source", str(self.root)]), 0)
        result = json.loads(output.getvalue())
        self.assertTrue(result["checksPassed"])
        self.assertFalse(result["readyForPaidExecution"])
        self.assertFalse(result["paidExecutionEnabled"])
        self.assertEqual(result["modelCalls"], 0)
        self.assertNotIn("func Greet", output.getvalue())
        self.assertNotIn(str(self.root), output.getvalue())
        self.assertEqual(before, demo.snapshot(self.root))

    def test_manifest_stable_and_content_bound(self):
        first = demo.snapshot(self.root)
        self.assertEqual(demo.manifest(first), demo.manifest(dict(reversed(list(first.items())))))
        changed = dict(first)
        changed["greeting.go"] += b"\n"
        self.assertNotEqual(demo.manifest(first)["snapshotSha256"], demo.manifest(changed)["snapshotSha256"])

    def test_missing_extra_directory_and_git_rejected(self):
        for name in (".env", ".git", "unknown.go"):
            with self.subTest(name=name):
                extra = self.root / name
                extra.mkdir()
                with self.assertRaisesRegex(demo.Blocked, "unexpected_files"):
                    demo.snapshot(self.root)
                extra.rmdir()
        (self.root / "README.md").unlink()
        with self.assertRaisesRegex(demo.Blocked, "unexpected_files"):
            demo.snapshot(self.root)

    def test_size_limit_boundary(self):
        values = demo.snapshot(self.root)
        fixed = sum(len(value) for name, value in values.items() if name != "README.md")
        (self.root / "README.md").write_bytes(b"x" * (demo.MAX_BYTES - fixed))
        self.assertEqual(demo.manifest(demo.snapshot(self.root))["sourceBytes"], demo.MAX_BYTES)
        with (self.root / "README.md").open("ab") as stream:
            stream.write(b"x")
        with self.assertRaisesRegex(demo.Blocked, "snapshot_too_large"):
            demo.snapshot(self.root)

    def test_binary_utf8_and_module_changes_rejected(self):
        for content, reason in ((b"\x00", "binary_file"), (b"\xff", "non_utf8_file")):
            (self.root / "README.md").write_bytes(content)
            with self.assertRaisesRegex(demo.Blocked, reason):
                demo.snapshot(self.root)
        (self.root / "README.md").write_text("public", encoding="utf-8")
        (self.root / "go.mod").write_text("module other\ngo 1.22\n", encoding="utf-8")
        with self.assertRaisesRegex(demo.Blocked, "module_contract_changed"):
            demo.snapshot(self.root)

    def test_traversal_rejected(self):
        with self.assertRaisesRegex(demo.Blocked, "source_traversal"):
            demo.snapshot(self.root / ".." / "source")

    def test_reparse_point_and_symlink_detection(self):
        self.assertTrue(demo.linked(types.SimpleNamespace(st_mode=stat.S_IFREG, st_file_attributes=0x400)))
        self.assertTrue(demo.linked(types.SimpleNamespace(st_mode=stat.S_IFLNK)))
        original = Path.lstat
        def linked_root(path, *args, **kwargs):
            if path == self.root:
                return types.SimpleNamespace(st_mode=stat.S_IFDIR, st_file_attributes=0x400)
            return original(path, *args, **kwargs)
        with patch.object(Path, "lstat", linked_root):
            with self.assertRaisesRegex(demo.Blocked, "linked_source"):
                demo.snapshot(self.root)

    def test_hard_link_rejected(self):
        target = self.root / "README.md"
        outside = Path(self.temp.name) / "outside"
        os.link(target, outside)
        with self.assertRaisesRegex(demo.Blocked, "non_regular_file"):
            demo.snapshot(self.root)

    def test_errors_do_not_print_raw_secret_paths(self):
        output = io.StringIO()
        with patch.object(Path, "lstat", side_effect=OSError("secret-token")), contextlib.redirect_stdout(output):
            self.assertEqual(demo.main(["--source", str(self.root)]), 1)
        self.assertNotIn("secret-token", output.getvalue())
        self.assertEqual(json.loads(output.getvalue())["reason"], "source_unreadable")

    def test_changed_file_during_read_is_rejected(self):
        original = os.fstat
        calls = 0
        def changed(fd):
            nonlocal calls
            calls += 1
            actual = original(fd)
            if calls == 2:
                return types.SimpleNamespace(st_dev=actual.st_dev, st_ino=actual.st_ino,
                    st_size=actual.st_size, st_mtime_ns=actual.st_mtime_ns + 1, st_ctime_ns=actual.st_ctime_ns)
            return actual
        with patch.object(os, "fstat", changed):
            with self.assertRaisesRegex(demo.Blocked, "source_changed"):
                demo.snapshot(self.root)

    def test_sandbox_contract_copy_and_cleanup(self):
        docker = FakeDocker()
        source = demo.snapshot(self.root)
        result = demo.sandbox(source, "golang:1.22-alpine", 90, docker)
        self.assertTrue(result["passed"])
        create = next(args for args in docker.calls if args[0] == "create")
        for option, value in (("--network", "none"), ("--user", "10001:10001"), ("--pull", "never"),
                              ("--cap-drop", "ALL"), ("--log-driver", "none"), ("--entrypoint", "go")):
            self.assertEqual(create[create.index(option) + 1], value)
        self.assertIn("--read-only", create)
        self.assertIn("/tmp:rw,exec,nosuid,nodev,size=128m", create)
        self.assertEqual(create.count("--mount"), 1)
        self.assertIn(IMAGE, create)
        self.assertNotIn(str(self.root), " ".join(create))
        self.assertNotIn("docker.sock", " ".join(create))
        self.assertFalse(docker.copied.exists())
        self.assertEqual(source, demo.snapshot(self.root))
        self.assertEqual(docker.calls[-1][0:2], ["rm", "--force"])

    def test_nonzero_test_exit_is_not_success(self):
        self.assertFalse(demo.sandbox(demo.snapshot(self.root), "golang:1.22-alpine", 90, FakeDocker(exit_code=1))["passed"])

    def test_cached_image_check_accepts_only_the_exact_pinned_id(self):
        docker = FakeDocker()
        self.assertEqual(demo.verify_cached_image(IMAGE, docker), IMAGE)
        self.assertEqual(docker.calls, [["image", "inspect", "--format", "{{.Id}}", IMAGE]])
        with self.assertRaisesRegex(demo.Blocked, "image_not_pinned"):
            demo.verify_cached_image("golang:1.22", docker)
        other = FakeDocker()
        with patch.object(other, "call", return_value="sha256:" + "b" * 64):
            with self.assertRaisesRegex(demo.Blocked, "image_not_pinned"):
                demo.verify_cached_image(IMAGE, other)

    def test_start_and_ambiguous_create_failures_cleanup_owned_container(self):
        for step in ("create", "start"):
            with self.subTest(step=step):
                docker = FakeDocker(fail_at=step)
                with self.assertRaisesRegex(demo.Blocked, "docker_command_timeout"):
                    demo.sandbox(demo.snapshot(self.root), "golang:1.22-alpine", 90, docker)
                self.assertEqual(docker.calls[-1][0], "rm")

    def test_no_cleanup_of_unowned_container(self):
        docker = FakeDocker(owner_matches=False)
        with self.assertRaisesRegex(demo.Blocked, "cleanup_unconfirmed"):
            demo.sandbox(demo.snapshot(self.root), "golang:1.22-alpine", 90, docker)
        self.assertFalse(any(args[0] == "rm" for args in docker.calls))

    def test_total_deadline_includes_container_runtime(self):
        docker = FakeDocker(running=True)
        clock = iter(i * 0.1 for i in range(100))
        with patch.object(demo.time, "monotonic", side_effect=lambda: next(clock)), patch.object(demo.time, "sleep"):
            with self.assertRaisesRegex(demo.Blocked, "sandbox_timeout"):
                demo.sandbox(demo.snapshot(self.root), "golang:1.22-alpine", 1, docker)
        self.assertEqual(docker.calls[-1][0], "rm")

    def test_invalid_image_and_timeout_do_not_invoke_docker(self):
        docker = FakeDocker()
        for image, seconds in (("--privileged", 90), ("golang:1.22-alpine", 0), ("golang:1.22-alpine", 121)):
            with self.assertRaises(demo.Blocked):
                demo.sandbox(demo.snapshot(self.root), image, seconds, docker)
        self.assertEqual(docker.calls, [])


if __name__ == "__main__":
    unittest.main()
