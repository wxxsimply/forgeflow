"""Bounded local credential-file loader; never logs or stores the key."""

import json
import os
from pathlib import Path
import stat
import subprocess

import preview_demo_https as transport
from preview_demo_preflight import Blocked, linked


MAX_KEY_BYTES = 514  # 512 ASCII key bytes plus one optional CRLF.
PROJECT_ROOT = Path(__file__).resolve().parent.parent
_WINDOWS_ALLOWED_READERS = frozenset(("S-1-5-18", "S-1-5-32-544"))  # SYSTEM, Administrators
_WINDOWS_BROAD_IDENTITIES = frozenset((
    "S-1-1-0", "S-1-5-11", "S-1-5-32-545", "S-1-5-32-546",
))
_WINDOWS_SENSITIVE_RIGHTS = 1 | 2 | 4 | 64 | 0x10000 | 0x40000 | 0x80000
_WINDOWS_ACL_SCRIPT = r"""
$ErrorActionPreference = 'Stop'
$OutputEncoding = [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$acl = Get-Acl -LiteralPath $env:FORGEFLOW_CREDENTIAL_ACL_PATH
$owner = $acl.GetOwner([Security.Principal.SecurityIdentifier]).Value
$current = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$rules = @($acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier]) | ForEach-Object {
    [ordered]@{sid=$_.IdentityReference.Value; kind=[int]$_.AccessControlType; rights=[int64]$_.FileSystemRights}
})
[ordered]@{owner=$owner; current=$current; rules=$rules} | ConvertTo-Json -Compress -Depth 3
"""


def _windows_acl(path):
    """Return a normalized ACL snapshot without reading the file contents."""
    system_root = os.environ.get("SystemRoot") or os.environ.get("WINDIR")
    if not system_root:
        raise Blocked("credential_acl_unavailable")
    executable = Path(system_root) / "System32" / "WindowsPowerShell" / "v1.0" / "powershell.exe"
    environment = {key: os.environ[key] for key in ("SystemRoot", "WINDIR", "TEMP", "TMP") if key in os.environ}
    environment["FORGEFLOW_CREDENTIAL_ACL_PATH"] = str(path)
    try:
        result = subprocess.run(
            [str(executable), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", _WINDOWS_ACL_SCRIPT],
            stdin=subprocess.DEVNULL, capture_output=True, text=True, encoding="utf-8", errors="strict",
            timeout=5, check=False, env=environment,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
        )
        if result.returncode != 0 or len(result.stdout) > 64 * 1024:
            raise ValueError
        value = json.loads(result.stdout)
        if not isinstance(value, dict) or set(value) != {"owner", "current", "rules"}:
            raise ValueError
        owner, current, rules = value["owner"], value["current"], value["rules"]
        if not isinstance(owner, str) or not isinstance(current, str) or not isinstance(rules, list):
            raise ValueError
        normalized = []
        privileged = set()
        for rule in rules:
            if (not isinstance(rule, dict) or set(rule) != {"sid", "kind", "rights"}
                    or not isinstance(rule["sid"], str) or type(rule["kind"]) is not int
                    or type(rule["rights"]) is not int):
                raise ValueError
            item = (rule["sid"], rule["kind"], rule["rights"])
            normalized.append(item)
            # Reject broad read/write/delete/ACL rights. A deny entry does not
            # make an unexpected allow entry acceptable for this local guard.
            if rule["kind"] == 0 and rule["rights"] & _WINDOWS_SENSITIVE_RIGHTS:
                privileged.add(rule["sid"])
        allowed = set(_WINDOWS_ALLOWED_READERS) | {current}
        if owner != current or owner in _WINDOWS_BROAD_IDENTITIES or not privileged or not privileged <= allowed:
            raise Blocked("credential_insecure_permissions")
        return owner, tuple(sorted(normalized))
    except Blocked:
        raise
    except (OSError, subprocess.SubprocessError, UnicodeError, ValueError, TypeError, RecursionError):
        raise Blocked("credential_acl_unavailable") from None


def _signature(info):
    value = (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns,
             stat.S_IMODE(info.st_mode), info.st_nlink,
             getattr(info, "st_uid", None), getattr(info, "st_gid", None))
    # Windows path stat and CRT fstat can represent ctime differently. ACL is
    # checked separately before and after reading; retain ctime on POSIX.
    return value if os.name == "nt" else value + (info.st_ctime_ns,)


def private_directory(path):
    """Require an existing non-repository directory private to its owner."""
    original = Path(path)
    if ".." in original.parts:
        raise Blocked("private_root_unsafe")
    path = original.absolute()
    if path == PROJECT_ROOT or PROJECT_ROOT in path.parents:
        raise Blocked("private_root_unsafe")
    try:
        for item in (path, *path.parents):
            if linked(item.lstat()):
                raise Blocked("private_root_unsafe")
        info = path.lstat()
        if not stat.S_ISDIR(info.st_mode):
            raise Blocked("private_root_unsafe")
        if os.name == "nt":
            _windows_acl(path)
        elif info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) & 0o077:
            raise Blocked("private_root_insecure_permissions")
        return path
    except Blocked:
        raise
    except (OSError, UnicodeError, ValueError, TypeError, RecursionError):
        raise Blocked("private_root_unavailable") from None


def load(path, *, forbidden_roots=()):
    """Read one stable private key file and return its validated ASCII value."""
    original = Path(path)
    if ".." in original.parts:
        raise Blocked("credential_unsafe_path")
    path = original.absolute()
    roots = (PROJECT_ROOT, *(Path(root).absolute() for root in forbidden_roots))
    if any(path == root or root in path.parents for root in roots):
        raise Blocked("credential_unsafe_path")
    try:
        for item in (path, *path.parents):
            if linked(item.lstat()):
                raise Blocked("credential_unsafe_path")
        before = path.lstat()
        if (not stat.S_ISREG(before.st_mode) or before.st_nlink != 1
                or not 1 <= before.st_size <= MAX_KEY_BYTES):
            raise Blocked("credential_unsafe_file")
        acl_before = None
        if os.name == "nt":
            acl_before = _windows_acl(path)
        else:
            if before.st_uid != os.geteuid() or stat.S_IMODE(before.st_mode) & 0o077:
                raise Blocked("credential_insecure_permissions")
        flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_BINARY", 0)
        with os.fdopen(os.open(path, flags), "rb") as stream:
            opened = os.fstat(stream.fileno())
            raw = stream.read(MAX_KEY_BYTES + 1)
            after = os.fstat(stream.fileno())
        current = path.lstat()
        if (_signature(before) != _signature(opened) or _signature(opened) != _signature(after)
                or _signature(after) != _signature(current)):
            raise Blocked("credential_changed")
        if os.name == "nt" and acl_before != _windows_acl(path):
            raise Blocked("credential_changed")
        if raw.endswith(b"\r\n"):
            raw = raw[:-2]
        elif raw.endswith(b"\n"):
            raw = raw[:-1]
        try:
            key = raw.decode("ascii")
        except UnicodeDecodeError:
            raise Blocked("credential_invalid") from None
        try:
            transport.validate_key(key)
        except Blocked:
            raise Blocked("credential_invalid") from None
        return key
    except Blocked:
        raise
    except (OSError, UnicodeError, ValueError, TypeError, RecursionError):
        raise Blocked("credential_unavailable") from None
