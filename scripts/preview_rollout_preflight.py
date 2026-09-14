#!/usr/bin/env python3
"""Read-only personal-preview checks. Never edits env, starts services or deploys."""

import argparse
import json
from pathlib import Path
import re
import subprocess
import time


PROJECT = "forgeflow-personal-preview"
SERVICES = ("api", "worker", "web", "caddy", "postgres")
REPOSITORY_ROOT = "/srv/forgeflow/preview-repositories"
INSPECT = '''{"running":{{json .State.Running}},"health":{{if .State.Health}}{{json .State.Health.Status}}{{else}}"missing"{{end}},"imageId":{{json .Image}},"imageName":{{json .Config.Image}},"user":{{json .Config.User}},"project":{{json (index .Config.Labels "com.docker.compose.project")}},"configFiles":{{json (index .Config.Labels "com.docker.compose.project.config_files")}},"revision":{{json (index .Config.Labels "org.opencontainers.image.revision")}},"mounts":{{json .Mounts}},"ports":{{json .NetworkSettings.Ports}}}'''
ENV_KEYS = (
    "FORGEFLOW_WORKFLOW_MODE", "FORGEFLOW_PLANNER_MODE", "FORGEFLOW_DOCKER_ENABLED",
    "FORGEFLOW_HTTP_COOKIE_SECURE", "FORGEFLOW_HTTP_ALLOWED_ORIGINS",
)
ENV_INSPECT = "{{range .Config.Env}}{{if or " + " ".join(
    '(eq (index (split . "=") 0) "' + key + '")' for key in ENV_KEYS
) + "}}{{println .}}{{end}}{{end}}"
COUNTS_SQL = """BEGIN READ ONLY;
SELECT json_build_object(
  'legacyPending', (SELECT count(*) FROM idempotency_keys WHERE status='pending'),
  'nonterminalRuns', (SELECT count(*) FROM runs WHERE status NOT IN ('completed','failed','cancelled')),
  'unfinishedJobs', (SELECT count(*) FROM jobs WHERE status IN ('queued','leased','retry')),
  'unpublishedOutbox', (SELECT count(*) FROM outbox WHERE published_at IS NULL));
COMMIT;"""


def command_reader(root):
    deadline = time.monotonic() + 90

    def read(command):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise RuntimeError("preflight exceeded its 90-second limit")
        try:
            result = subprocess.run(command, cwd=root, capture_output=True, text=True,
                                    encoding="utf-8", timeout=min(8, remaining), check=False)
        except (OSError, subprocess.TimeoutExpired):
            raise RuntimeError("required command unavailable or timed out") from None
        if result.returncode:
            # Raw stderr/stdout could include connection strings. Never echo it.
            raise RuntimeError("read-only command failed; inspect locally without sharing raw logs")
        return result.stdout.strip()

    return read


def audit(read, expected_commit, phase):
    checks = []
    snapshot = {}

    def check(name, passed, detail):
        checks.append({"name": name, "passed": bool(passed), "detail": detail})

    def container(service):
        return PROJECT + "-" + service + "-1"

    def mount(info, destination):
        return next((item for item in info["mounts"] if item.get("Destination") == destination), {})

    try:
        source = read(["git", "rev-parse", "HEAD"])
        dirty = read(["git", "status", "--porcelain"])
        snapshot["sourceCommit"] = source
        check("source", source == expected_commit and not dirty,
              "工作区须干净且等于本轮人工确认的完整提交编号；不输出未提交文件名。")
        infos = {}
        for service in SERVICES:
            info = json.loads(read(["docker", "inspect", "--type", "container", "--format", INSPECT, container(service)]))
            infos[service] = info
            snapshot[service] = {key: info.get(key) for key in ("imageId", "imageName", "revision", "health", "user")}
            check(service + ".health", info["running"] and info["health"] == "healthy" and info["project"] == PROJECT,
                  "只检查 ForgeFlow 命名容器，必须运行且健康。")
            files = set(filter(None, (info.get("configFiles") or "").split(",")))
            base = "/srv/forgeflow/app/deploy/personal-preview/compose.yaml"
            public = "/srv/forgeflow/app/deploy/personal-preview/compose.public-ip.yaml"
            valid_files = files == {base, public} or (service not in ("api", "caddy") and files == {base})
            check(service + ".overlays", valid_files,
                  "API/Caddy 须保留 HTTPS overlay；其他未重建服务可仅有基础配置，未知配置或 Bootstrap 须人工核对。")
            if service != "caddy":
                check(service + ".private", not any(info["ports"].values()),
                      "API、Worker、Web、数据库不得直接发布宿主机端口。")

        for service in ("api", "worker"):
            values = dict(line.split("=", 1) for line in read([
                "docker", "inspect", "--format", ENV_INSPECT, container(service)
            ]).splitlines() if "=" in line)
            check(service + ".uid", infos[service]["user"] == "10001:10001", "应用必须保持非 root 的 10001:10001 用户。")
            safe = values.get("FORGEFLOW_WORKFLOW_MODE") == "planning" and values.get("FORGEFLOW_PLANNER_MODE") == "mock"
            if service == "worker":
                safe = safe and values.get("FORGEFLOW_DOCKER_ENABLED") == "false"
            else:
                safe = safe and values.get("FORGEFLOW_HTTP_COOKIE_SECURE") == "true" and values.get("FORGEFLOW_HTTP_ALLOWED_ORIGINS") == "https://39.102.136.31"
            check(service + ".mode", safe, "保持模拟模式、关闭真实执行，API 仅允许当前 HTTPS Origin 并使用 Secure Cookie。")

        api_root = mount(infos["api"], "/repositories")
        worker_root = mount(infos["worker"], "/repositories")
        snapshot["repositorySource"] = api_root.get("Source")
        check("repository.mounts", api_root.get("Type") == worker_root.get("Type") == "bind"
              and api_root.get("Source") == worker_root.get("Source")
              and api_root.get("Source") in ("/srv/forgeflow/repositories", REPOSITORY_ROOT)
              and api_root.get("RW") is False and worker_root.get("RW") is True,
              "API 只读、Worker 可写且来源一致；更新后只能使用专用演示根。")
        ports = infos["caddy"]["ports"]
        bindings = [(port, item) for port, items in ports.items() for item in (items or [])]
        safe_ports = bool(ports.get("443/tcp")) and bool(ports.get("8080/tcp")) and all(
            (port == "443/tcp" and item["HostPort"] == "443") or
            (port == "8080/tcp" and item["HostPort"] == "8080" and item["HostIp"] == "127.0.0.1")
            for port, item in bindings
        )
        check("ingress", safe_ports, "只发布 HTTPS 443 和回环 8080，不占用 TTS 的 80 端口。")
        snapshot["certificateVolumes"] = {path: mount(infos["caddy"], path).get("Name") for path in ("/data", "/config")}
        check("certificates", all(mount(infos["caddy"], path).get("Type") == "volume" and mount(infos["caddy"], path).get("Name") for path in ("/data", "/config")),
              "Caddy 证书和配置必须使用持久卷，禁止清卷。")
        counts = json.loads(read(["docker", "exec", container("postgres"), "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "forgeflow", "-d", "forgeflow", "-c", COUNTS_SQL]))
        snapshot["counts"] = counts
        expected_counts = ("legacyPending", "nonterminalRuns", "unfinishedJobs", "unpublishedOutbox")
        check("quiescence", all(type(counts.get(key)) is int and counts[key] == 0 for key in expected_counts),
              "仅汇总计数；旧 pending 或未结束任务/队列须人工处理，不能删表或自动取消。")

        if phase == "after":
            check("repository.switched", api_root.get("Source") == REPOSITORY_ROOT, "更新后挂载必须切换到专用演示根。")
            demo_commit = read(["docker", "exec", container("api"), "git", "-C", "/repositories/demo", "rev-parse", "HEAD"])
            check("repository.readable", re.fullmatch(r"[0-9a-f]{40}", demo_commit), "API 默认用户实际读取演示仓库，不修改文件。")
            for service, endpoint in (("api", "http://127.0.0.1:8080/healthz"), ("web", "http://127.0.0.1:8080/release.json")):
                build = json.loads(read(["docker", "exec", container(service), "wget", "-qO-", endpoint]))
                check(service + ".commit", build.get("gitCommit") == expected_commit,
                      "读取实际运行服务的版本，不能只看源码 checkout。")
            check("worker.commit", infos["worker"].get("revision") == expected_commit,
                  "Worker 容器必须使用本轮构建标识；仍需浏览器人工流程验收。")
    except (RuntimeError, ValueError, KeyError, TypeError, AttributeError):
        check("collection", False, "无法完整取得或解析只读证据；禁止据此继续部署。请在服务器私下检查命令、权限和工具版本。")

    return {"phase": phase, "checksPassed": bool(checks) and all(item["passed"] for item in checks),
            "manualApprovalRequired": True, "deployed": False, "checks": checks, "snapshot": snapshot,
            "limits": "仅为瞬时本机检查，不验证云安全组、外部证书或浏览器流程，也不能阻止检查后新任务进入。"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-commit", required=True, help="人工核对的 40 位 Git commit")
    parser.add_argument("--phase", choices=("before", "after"), default="before")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.expected_commit):
        parser.error("expected-commit must be a full lowercase 40-character Git SHA")
    report = audit(command_reader(Path(__file__).resolve().parent.parent), args.expected_commit, args.phase)
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report["checksPassed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
