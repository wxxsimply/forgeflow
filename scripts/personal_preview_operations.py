#!/usr/bin/env python3
"""Sanitized observation snapshots and read-only rollback plans for personal preview."""

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import tempfile


SERVICES = ("api", "worker", "web", "caddy", "postgres")
APP_SERVICES = ("api", "worker")
SHA_PATTERN = re.compile(r"[0-9a-f]{40}")
RELEASE_PATTERN = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}")
ERROR_PATTERN = re.compile(r"(?:^|[\s\[\{])(error|fatal|panic)(?:[\s:=\]\},]|$)|level\s*[=:]\s*error", re.IGNORECASE)
FORBIDDEN_MODEL_KEYS = ("OPENAI_API_KEY", "DEEPSEEK_API_KEY")
DATABASE_SIZE_SQL = "SELECT pg_database_size(current_database());"


def parse_env(path):
    values = {}
    for number, source in enumerate(Path(path).read_text(encoding="utf-8").splitlines(), 1):
        line = source.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            raise ValueError(f"invalid environment line {number}")
        name, value = line.split("=", 1)
        name = name.strip()
        if not re.fullmatch(r"[A-Z][A-Z0-9_]*", name):
            raise ValueError(f"invalid environment name on line {number}")
        if name in values:
            raise ValueError(f"duplicate environment name: {name}")
        values[name] = value.strip().strip('"').strip("'")
    return values


def required_identity(values):
    release = values.get("FORGEFLOW_RELEASE", "")
    commit = values.get("FORGEFLOW_GIT_COMMIT", "").lower()
    if not RELEASE_PATTERN.fullmatch(release):
        raise ValueError("FORGEFLOW_RELEASE must be a stable release identifier")
    if not SHA_PATTERN.fullmatch(commit):
        raise ValueError("FORGEFLOW_GIT_COMMIT must be a full lowercase Git SHA")
    return release, commit


def validate_private_environment(values):
    if any(name in values for name in FORBIDDEN_MODEL_KEYS):
        raise ValueError("personal preview env must not contain model credentials")
    repository = values.get("FORGEFLOW_REPOSITORY_PATH", "")
    if repository not in ("/srv/forgeflow/repositories", "/srv/forgeflow/preview-repositories"):
        raise ValueError("personal preview repository path is outside the approved roots")
    return repository


def command_reader(root, timeout_seconds=20):
    def read(command):
        try:
            result = subprocess.run(
                command,
                cwd=root,
                capture_output=True,
                text=True,
                encoding="utf-8",
                timeout=timeout_seconds,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired):
            raise RuntimeError("required local command is unavailable or timed out") from None
        if result.returncode:
            # Compose errors and logs can contain paths or user data. Never echo raw output.
            raise RuntimeError("read-only operations command failed; inspect its raw output privately")
        return result.stdout.strip()

    return read


def compose_prefix(root, env_file):
    preview = Path(root) / "deploy" / "personal-preview"
    return [
        "docker", "compose", "--env-file", str(Path(env_file).resolve()),
        "-f", str(preview / "compose.yaml"),
        "-f", str(preview / "compose.public-ip.yaml"),
    ]


def parse_compose_ps(raw):
    if not raw.strip():
        return []
    try:
        value = json.loads(raw)
        return value if isinstance(value, list) else [value]
    except json.JSONDecodeError:
        rows = []
        for line in raw.splitlines():
            rows.append(json.loads(line))
        return rows


def service_status(rows):
    result = {}
    for row in rows:
        service = row.get("Service") or row.get("service")
        if service in SERVICES:
            result[service] = {
                "state": str(row.get("State") or row.get("state") or "unknown").lower(),
                "health": str(row.get("Health") or row.get("health") or "missing").lower(),
            }
    return result


def add_check(checks, name, passed, detail):
    checks.append({"name": name, "passed": bool(passed), "detail": detail})


def inspect_boundary(config, checks):
    services = config.get("services", {})
    safe = True
    for service_name in APP_SERVICES:
        environment = services.get(service_name, {}).get("environment", {})
        service_safe = (
            environment.get("FORGEFLOW_WORKFLOW_MODE") == "planning"
            and environment.get("FORGEFLOW_PLANNER_MODE") == "mock"
            and not any(name in environment for name in FORBIDDEN_MODEL_KEYS)
        )
        if service_name == "worker":
            service_safe = service_safe and environment.get("FORGEFLOW_DOCKER_ENABLED") == "false"
        else:
            service_safe = (
                service_safe
                and environment.get("FORGEFLOW_HTTP_COOKIE_SECURE") == "true"
                and str(environment.get("FORGEFLOW_HTTP_ALLOWED_ORIGINS", "")).startswith("https://")
            )
        safe = safe and service_safe
    add_check(checks, "cost-and-execution-boundary", safe,
              "Planning/Mock、零模型凭据、关闭 Docker 执行并保持 HTTPS 会话边界。")
    return {
        "plannerMode": "mock" if safe else "unexpected",
        "externalModelCallsExpected": 0 if safe else None,
        "providerCostExpectedUSD": 0 if safe else None,
    }


def latest_backup(backup_directory, now):
    directory = Path(backup_directory)
    if not directory.is_dir():
        return {"count": 0, "latestAgeSeconds": None, "freshWithin26Hours": False}
    files = [item for item in directory.glob("forgeflow-preview-*.dump") if item.is_file()]
    if not files:
        return {"count": 0, "latestAgeSeconds": None, "freshWithin26Hours": False}
    latest = max(files, key=lambda item: item.stat().st_mtime)
    age = max(0, int(now.timestamp() - latest.stat().st_mtime))
    return {"count": len(files), "latestAgeSeconds": age, "freshWithin26Hours": age <= 26 * 60 * 60}


def observe(root, env_file, backup_directory, data_path, read, now=None, disk_usage=shutil.disk_usage):
    now = now or datetime.now(timezone.utc)
    if not private_file(env_file):
        raise ValueError("personal preview env must be a private regular file")
    values = parse_env(env_file)
    release, commit = required_identity(values)
    validate_private_environment(values)
    prefix = compose_prefix(root, env_file)
    checks = []

    source_commit = read(["git", "rev-parse", "HEAD"])
    source_dirty = read(["git", "status", "--porcelain", "--untracked-files=no"])
    add_check(checks, "source-version", source_commit == commit and not source_dirty,
              "服务器受跟踪源码必须干净且匹配私有部署记录。")

    config = json.loads(read(prefix + ["config", "--format", "json"]))
    cost = inspect_boundary(config, checks)

    statuses = service_status(parse_compose_ps(read(prefix + ["ps", "--all", "--format", "json"])))
    all_healthy = all(
        statuses.get(service, {}).get("state") == "running"
        and statuses.get(service, {}).get("health") == "healthy"
        for service in SERVICES
    )
    add_check(checks, "service-health", all_healthy, "五个长期服务必须同时 running/healthy。")

    api = json.loads(read(["curl", "--fail", "--silent", "--show-error", "--max-time", "10", "http://127.0.0.1:8080/healthz"]))
    web = json.loads(read(["curl", "--fail", "--silent", "--show-error", "--max-time", "10", "http://127.0.0.1:8080/release.json"]))
    worker = json.loads(read(prefix + ["exec", "-T", "worker", "wget", "-qO-", "http://127.0.0.1:9091/readyz"]))
    api_matches = api.get("status") == "ok" and api.get("serviceVersion") == release and api.get("gitCommit") == commit
    web_matches = web.get("serviceVersion") == release and web.get("gitCommit") == commit
    worker_matches = worker.get("status") == "ready" and worker.get("serviceVersion") == release and worker.get("gitCommit") == commit
    add_check(checks, "api-version", api_matches, "本机代理后的 API health 必须匹配私有部署记录。")
    add_check(checks, "web-version", web_matches, "本机代理后的 Web release 必须匹配私有部署记录。")
    add_check(checks, "worker-version", worker_matches, "Worker readiness 必须匹配私有部署记录。")

    log_text = read(prefix + ["logs", "--no-color", "--since", "15m", "--tail", "500", "api", "worker", "web", "caddy"])
    error_lines = sum(1 for line in log_text.splitlines() if ERROR_PATTERN.search(line))
    add_check(checks, "recent-errors", error_lines == 0,
              "只记录最近 15 分钟错误行数量；原始日志不得进入快照或公开渠道。")

    database_text = read(prefix + [
        "exec", "-T", "postgres", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1",
        "-U", "forgeflow", "-d", "forgeflow", "-c", DATABASE_SIZE_SQL,
    ])
    if not re.fullmatch(r"[0-9]+", database_text):
        raise ValueError("database size output is invalid")
    database_bytes = int(database_text)
    add_check(checks, "database-capacity", database_bytes > 0, "仅记录数据库聚合字节数，不读取业务行。")

    disk = disk_usage(data_path)
    disk_percent = round((disk.used / disk.total) * 100, 2) if disk.total else 100.0
    add_check(checks, "disk-capacity", disk_percent < 90, "数据盘使用率必须低于 90%。")

    backup = latest_backup(backup_directory, now)
    add_check(checks, "backup-freshness", backup["freshWithin26Hours"],
              "需要最近 26 小时内的个人预览备份；缺失时保持 PERSONAL-004/005 未完成。")

    return {
        "schemaVersion": "forgeflow.personal-preview-observation/v1",
        "observedAt": now.astimezone(timezone.utc).isoformat().replace("+00:00", "Z"),
        "release": release,
        "gitCommit": commit,
        "checksPassed": all(item["passed"] for item in checks),
        "attentionRequired": [item["name"] for item in checks if not item["passed"]],
        "checks": checks,
        "services": statuses,
        "recentErrorLines": error_lines,
        "databaseBytes": database_bytes,
        "diskUsedPercent": disk_percent,
        "backup": backup,
        "costBoundary": cost,
        "containsRawLogs": False,
        "executedMutation": False,
    }


def private_file(path):
    try:
        metadata = Path(path).lstat()
    except OSError:
        return False
    if not stat.S_ISREG(metadata.st_mode):
        return False
    return os.name == "nt" or (metadata.st_mode & 0o077) == 0


def rollback_plan(root, current_env, target_env, read):
    if not private_file(current_env) or not private_file(target_env):
        raise ValueError("current and target env files must be private regular files, not symlinks")
    current = parse_env(current_env)
    target = parse_env(target_env)
    current_release, current_commit = required_identity(current)
    target_release, target_commit = required_identity(target)
    current_repository = validate_private_environment(current)
    target_repository = validate_private_environment(target)
    if (current_release, current_commit) == (target_release, target_commit):
        raise ValueError("rollback target is already current")
    if current_repository != target_repository:
        raise ValueError("rollback must not change the repository mount")

    checks = []
    target_prefix = compose_prefix(root, target_env)
    config = json.loads(read(target_prefix + ["config", "--format", "json"]))
    inspect_boundary(config, checks)

    image_ids = {}
    for service in ("api", "worker", "web"):
        reference = f"forgeflow-{service}:{target_release}"
        rendered_reference = config.get("services", {}).get(service, {}).get("image")
        add_check(checks, f"{service}-rendered-image", rendered_reference == reference,
                  "目标 Compose 必须引用与私有回滚记录同名的本地镜像标签。")
        metadata = json.loads(read(["docker", "image", "inspect", reference]))
        if not isinstance(metadata, list) or len(metadata) != 1:
            raise ValueError("rollback image metadata is invalid")
        item = metadata[0]
        labels = item.get("Config", {}).get("Labels", {}) or {}
        valid = (
            labels.get("org.opencontainers.image.version") == target_release
            and labels.get("org.opencontainers.image.revision") == target_commit
            and re.fullmatch(r"sha256:[0-9a-f]{64}", str(item.get("Id", ""))) is not None
        )
        add_check(checks, f"{service}-image", valid, "目标本地镜像的版本和 revision 标签必须匹配回滚记录。")
        image_ids[service] = item.get("Id") if valid else None

    return {
        "schemaVersion": "forgeflow.personal-preview-rollback-plan/v1",
        "current": {"release": current_release, "gitCommit": current_commit},
        "target": {"release": target_release, "gitCommit": target_commit},
        "checksPassed": all(item["passed"] for item in checks),
        "checks": checks,
        "imageIds": image_ids,
        "services": ["api", "worker", "web"],
        "databaseAction": "compatibility-check-only-before-restart",
        "downMigration": False,
        "volumesRemoved": False,
        "executed": False,
    }


def write_report(report, output_directory):
    directory = Path(output_directory)
    if directory.exists() and directory.is_symlink():
        raise ValueError("output directory must not be a symlink")
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    if os.name != "nt":
        directory.chmod(0o700)
    observed = datetime.fromisoformat(report["observedAt"].replace("Z", "+00:00"))
    timestamp = observed.astimezone(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    target = directory / f"observation-{timestamp}.json"
    handle, temporary_name = tempfile.mkstemp(prefix=".observation-", suffix=".tmp", dir=directory)
    try:
        with os.fdopen(handle, "w", encoding="utf-8", newline="\n") as stream:
            json.dump(report, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
        os.chmod(temporary_name, 0o600)
        os.replace(temporary_name, target)
    finally:
        if os.path.exists(temporary_name):
            os.unlink(temporary_name)
    return target


def main():
    root = Path(__file__).resolve().parent.parent
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    observe_parser = subparsers.add_parser("observe", help="collect a sanitized read-only snapshot")
    observe_parser.add_argument("--env-file", default="deploy/personal-preview/preview.env")
    observe_parser.add_argument("--backup-directory", default="/srv/forgeflow/backups")
    observe_parser.add_argument("--data-path", default="/srv/forgeflow")
    observe_parser.add_argument("--output-directory")

    rollback_parser = subparsers.add_parser("rollback-plan", help="validate a previous local image set without changing services")
    rollback_parser.add_argument("--current-env", default="deploy/personal-preview/preview.env")
    rollback_parser.add_argument("--target-env", required=True)

    args = parser.parse_args()
    read = command_reader(root)
    try:
        if args.command == "observe":
            report = observe(root, root / args.env_file, args.backup_directory, args.data_path, read)
            if args.output_directory:
                path = write_report(report, args.output_directory)
                report["privateRecord"] = str(path)
            print(json.dumps(report, ensure_ascii=False, indent=2))
            return 0 if report["checksPassed"] else 1
        plan = rollback_plan(root, root / args.current_env, root / args.target_env, read)
        print(json.dumps(plan, ensure_ascii=False, indent=2))
        return 0 if plan["checksPassed"] else 1
    except (OSError, ValueError, KeyError, TypeError, json.JSONDecodeError, RuntimeError):
        print(json.dumps({
            "schemaVersion": "forgeflow.personal-preview-operations-error/v1",
            "status": "blocked",
            "detail": "无法安全取得或验证脱敏证据；请在服务器私下检查命令、权限和配置。",
        }, ensure_ascii=False))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
