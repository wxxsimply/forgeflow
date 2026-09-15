"""Credential-loader checks use synthetic temporary strings only."""

import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import preview_demo_credentials as credentials


KEY = b"synthetic-key-not-a-credential"


class CredentialTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.root.chmod(0o700)
        self.path = self.root / "key"
        self.path.write_bytes(KEY + b"\n")
        self.path.chmod(0o600)

    def acl(self):
        return patch.object(credentials, "_windows_acl",
                            return_value=("owner", (("owner", 0, 1),)))

    def load(self):
        if os.name == "nt":
            with self.acl():
                return credentials.load(self.path)
        return credentials.load(self.path)

    def test_private_stable_file_loads_one_optional_newline(self):
        self.assertEqual(self.load(), KEY.decode())
        self.path.write_bytes(KEY + b"\r\n")
        self.path.chmod(0o600)
        self.assertEqual(self.load(), KEY.decode())

    def test_invalid_content_size_path_and_hardlink_fail_closed(self):
        for raw in (b"", b"short", KEY + b"\nextra", b"x" * 515, b"\xff" * 20):
            self.path.write_bytes(raw)
            self.path.chmod(0o600)
            with self.assertRaises(credentials.Blocked):
                self.load()
        self.path.write_bytes(KEY)
        self.path.chmod(0o600)
        linked = self.root / "linked"
        os.link(self.path, linked)
        with self.assertRaisesRegex(credentials.Blocked, "credential_unsafe_file"):
            self.load()
        with self.assertRaisesRegex(credentials.Blocked, "credential_unsafe_path"):
            credentials.load(Path("safe") / ".." / "key")

    def test_forbidden_root_is_checked_before_reading(self):
        with self.assertRaisesRegex(credentials.Blocked, "credential_unsafe_path"):
            credentials.load(self.path, forbidden_roots=(self.root,))

    def test_private_directory_must_be_outside_repository_and_owner_only(self):
        if os.name == "nt":
            with self.acl():
                self.assertEqual(credentials.private_directory(self.root), self.root.absolute())
            with patch.object(credentials, "_windows_acl",
                              side_effect=credentials.Blocked("credential_insecure_permissions")):
                with self.assertRaises(credentials.Blocked):
                    credentials.private_directory(self.root)
        else:
            self.assertEqual(credentials.private_directory(self.root), self.root.absolute())
            self.root.chmod(0o750)
            with self.assertRaisesRegex(credentials.Blocked, "private_root_insecure_permissions"):
                credentials.private_directory(self.root)
        with self.assertRaisesRegex(credentials.Blocked, "private_root_unsafe"):
            credentials.private_directory(credentials.PROJECT_ROOT)

    @unittest.skipIf(os.name == "nt", "POSIX permission bits are tested on CI")
    def test_posix_group_or_other_access_is_rejected(self):
        self.path.chmod(0o640)
        with self.assertRaisesRegex(credentials.Blocked, "credential_insecure_permissions"):
            credentials.load(self.path)

    def test_windows_acl_accepts_only_owner_system_and_administrators(self):
        allowed = {"owner": "S-1-user", "current": "S-1-user", "rules": [
            {"sid": "S-1-user", "kind": 0, "rights": 1},
            {"sid": "S-1-5-18", "kind": 0, "rights": 2032127},
            {"sid": "S-1-5-32-544", "kind": 0, "rights": 1},
        ]}
        broad = {"owner": "S-1-user", "current": "S-1-user", "rules": allowed["rules"] + [
            {"sid": "S-1-1-0", "kind": 0, "rights": 1},
        ]}

        def result(value):
            return SimpleNamespace(returncode=0, stdout=json.dumps(value), stderr="")

        with patch.dict(credentials.os.environ, {"SystemRoot": "C:/Windows"}, clear=True), \
                patch.object(credentials.subprocess, "run", return_value=result(allowed)):
            self.assertEqual(credentials._windows_acl(self.path)[0], "S-1-user")
        with patch.dict(credentials.os.environ, {"SystemRoot": "C:/Windows"}, clear=True), \
                patch.object(credentials.subprocess, "run", return_value=result(broad)):
            with self.assertRaisesRegex(credentials.Blocked, "credential_insecure_permissions"):
                credentials._windows_acl(self.path)
        foreign_owner = {"owner": "S-1-other", "current": "S-1-user",
                         "rules": [{"sid": "S-1-other", "kind": 0, "rights": 1}]}
        with patch.dict(credentials.os.environ, {"SystemRoot": "C:/Windows"}, clear=True), \
                patch.object(credentials.subprocess, "run", return_value=result(foreign_owner)):
            with self.assertRaisesRegex(credentials.Blocked, "credential_insecure_permissions"):
                credentials._windows_acl(self.path)
        broad["owner"] = "S-1-1-0"
        broad["current"] = "S-1-1-0"
        broad["rules"] = [{"sid": "S-1-1-0", "kind": 0, "rights": 1}]
        with patch.dict(credentials.os.environ, {"SystemRoot": "C:/Windows"}, clear=True), \
                patch.object(credentials.subprocess, "run", return_value=result(broad)):
            with self.assertRaisesRegex(credentials.Blocked, "credential_insecure_permissions"):
                credentials._windows_acl(self.path)

    @unittest.skipUnless(os.name == "nt", "real Windows ACL query")
    def test_real_windows_acl_query_on_synthetic_temp_file_works(self):
        try:
            value = credentials._windows_acl(self.path)
            self.assertIsInstance(value, tuple)
        except credentials.Blocked as error:
            # A temp directory may inherit a broad read ACE. That is a valid
            # rejection; command/parsing failures are not.
            self.assertEqual(str(error), "credential_insecure_permissions")

    def test_acl_command_failure_is_sanitized(self):
        failure = SimpleNamespace(returncode=1, stdout="", stderr=KEY.decode())
        with patch.dict(credentials.os.environ, {"SystemRoot": "C:/Windows"}, clear=True), \
                patch.object(credentials.subprocess, "run", return_value=failure):
            with self.assertRaisesRegex(credentials.Blocked, "^credential_acl_unavailable$") as caught:
                credentials._windows_acl(self.path)
        self.assertNotIn(KEY.decode(), str(caught.exception))


if __name__ == "__main__":
    unittest.main()
