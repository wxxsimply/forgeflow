# ForgeFlow Staging Operations

## 1. 上线边界

本 Compose 仅用于单机 Staging。公网只开放 Caddy 的 `80/443`，其中 80 自动跳转 HTTPS。PostgreSQL、API、Worker、Prometheus、Alertmanager 和 OTLP 均不发布宿主端口。Production 还必须完成真实 Eval Baseline、专用 Worker 主机、SBOM/漏洞扫描、备份异地复制、负载与入侵演练。

值班前必须填写并保存在团队私有运维系统：`PRIMARY_ONCALL`、`SECONDARY_ONCALL`、安全联系人、数据负责人、云厂商升级路径。仓库不保存私人联系方式。

## 2. 首次准备

服务器要求 Docker Engine + Compose v2、可解析到服务器的域名、开放 80/443、至少一个已有 Git commit 的只读测试仓库。部署账号独占 `deploy/staging/secrets`，Secret 文件权限设为 `0600`。

```powershell
Copy-Item deploy/staging/staging.env.example deploy/staging/staging.env
# 创建 postgres_password、postgres_dsn、alert_webhook_url
./scripts/staging-preflight.ps1
```

`postgres_dsn` 中密码必须 URL encode。生产 Promotion 时执行 `-RequireDigests`，所有第三方镜像必须使用 `image@sha256:digest`。

## 3. 首次管理员与部署

Bootstrap 只运行一次：创建 `bootstrap_admin_password` Secret，在 `staging.env` 或当前终端设置管理员邮箱，然后执行：

```powershell
./scripts/staging-release.ps1 -Release 0.11.0 -IncludeBootstrap -ConfirmDeploy
```

确认登录后立即删除 `bootstrap_admin_password`，以后部署不得再传 `-IncludeBootstrap`。真实模型只给 Worker：

```powershell
./scripts/staging-release.ps1 -Release 0.11.1 -IncludeOpenAI -ConfirmDeploy
```

部署顺序固定为：构建不可变版本镜像 → PostgreSQL 健康 → 显式 Migration → API/Worker/Web 健康 → Caddy。Release manifest 写入 `.forgeflow/deploy/releases`，包含前一版本、Git SHA 和 Compose 镜像清单。

Prompt Promotion 不会对运行中的 Worker 做隐式热替换。候选镜像必须同时保留可回滚的旧 Prompt，并按“drain Worker → 部署候选 API（Worker 暂停）→ 导入真实 Eval → Admin Promotion → 使用与 Active Release 一致的 Prompt 环境重启 Worker”的顺序发布。Promotion/rollback 表是治理记录，不等同于镜像发布；版本、SHA 和 Worker 配置不一致时禁止恢复流量。

## 4. 日常检查

```powershell
docker compose --env-file deploy/staging/staging.env -f deploy/staging/compose.yaml ps
Invoke-WebRequest https://<domain>/healthz
docker compose --env-file deploy/staging/staging.env -f deploy/staging/compose.yaml logs --since 30m api worker
```

Prometheus/Alertmanager 不公开。使用 SSH 本地端口转发或临时 `docker compose port` 诊断，不允许长期发布管理端口。日志禁止包含 Cookie、密码、API Key、任务正文和完整仓库文件。

## 5. Worker drain 与维护

完整 Development Graph 使用 `compose.openai.yaml` 中的专用 `sandbox-engine`。Worker 仅通过内部 `worker-execution` 网络和 dind 自动生成的双向 TLS 客户端证书访问它，不挂载宿主 `/var/run/docker.sock`；证书 volume 在 Worker 中只读。sandbox engine 的特权边界只用于创建受限任务容器，不接受公网流量。任务容器仍由 Worker 强制设置无网络、非 root、只读根文件系统、零 capabilities、PID/CPU/内存/超时限制。升级 Docker daemon 镜像前必须重新运行安全演练。

宿主机的 `FORGEFLOW_REPOSITORY_PATH` 必须允许容器 UID/GID `10001:10001` 创建和清理 Git worktree 元数据；不要使用 `chmod 777`。API 只读挂载 workspace volume，用于恢复前兼容性校验，不持有 Docker endpoint 或模型密钥。

1. 暂停创建新 Run 或在入口返回维护状态。
2. 观察 `forgeflow_queue_depth`，等待活动 Job 完成。
3. `docker compose stop -t 90 worker`；未完成 Job 的租约到期后可恢复。
4. 完成维护后启动 Worker，检查 lease lost、queue depth 和失败 Run。

不要强制删除 workspaces；必须先按 Run/Checkpoint 判断其是否仍被引用。

## 6. 备份与恢复

每日执行并将 `.dump`、`.sha256`、manifest 加密复制到异地介质：

```powershell
./scripts/staging-backup.ps1
```

每月至少一次恢复到隔离数据库，脚本只允许 `forgeflow_restore_*`，不会覆盖在线 `forgeflow`：

```powershell
./scripts/staging-restore-drill.ps1 -BackupFile /backups/forgeflow-20260811T120000Z.dump -ConfirmRestore
```

恢复成功标准：SHA-256 与 archive list 通过、`schema_migrations` 可读、API 使用恢复库启动并通过登录→Run→审批→报告 E2E。建议 Staging RPO 24 小时、RTO 4 小时；Production 指标必须由数据负责人确认。Artifact Volume 需独立快照并与数据库时间点对齐。

## 7. 发布与回滚

应用回滚不自动执行 Down Migration：

```powershell
./scripts/staging-rollback.ps1 -Manifest .forgeflow/deploy/releases/0.11.0.json -ConfirmRollback
```

回滚前使用目标版本 CLI 检查当前 Schema；不兼容会直接阻止。数据库回滚只能按事故流程恢复到新数据库，再完成一致性检查并切换 DSN，禁止覆盖在线 Production。

## 8. Secret 轮换

- OpenAI Key：创建新 Key → 更新 Worker Secret → 重建 Worker → 验证模型调用 → 撤销旧 Key。
- PostgreSQL：进入维护窗口 → 创建新角色/密码 → 更新 DSN 和 DB Secret → 重启迁移/API/Worker并验证 → 删除旧角色。
- Admin 密码/Session：修改密码并撤销其他 Session；疑似泄露时撤销全部 Session。
- Alert webhook：更新 Secret，重启 Alertmanager，发送测试告警后撤销旧 webhook。

轮换中不得把 Secret 写入命令历史、Issue、聊天或普通日志。

## 9. 告警 Runbook

### API or worker down

检查容器状态、最近日志、OOM/磁盘、PostgreSQL 和 Migration。API Down 时停止入口流量；Worker Down 时暂停新 Run，确认租约过期后再扩容。连续崩溃优先回滚，不循环重启掩盖故障。

### High API error rate

按 `status_class`、route 和 request ID 关联 Trace；检查数据库连接、Schema、限速与最近 Release。不得通过关闭认证/CSRF缓解。

### Queue backlog

检查 Worker 健康、租约丢失、单 Run 长耗时和模型限流。确认幂等后再增加 Worker；不要直接修改 jobs 状态。

### Budget exhaustion spike

检查 Prompt/模型版本、任务类别和重试。保留预算门禁，暂停候选 Prompt Promotion，不临时提高全局预算。

### Tool policy denial spike

视为潜在 Prompt Injection 或配置回归。保留审计 evidence，隔离相关 Run；禁止把 Policy 改成 allow 作为快速修复。

### Approval wait

确认值班审批人和通知通道，不能由系统自动批准。过期审批应取消或重新生成绑定证据。

### Login failure spike

检查来源、账号枚举和凭据填充迹象；保持限速，必要时封禁入口来源并撤销目标账号 Session。

### Rate limit spike

区分攻击、失控客户端和容量不足。先限制来源/客户端退避，再评估容量；不得直接移除限速。

## 10. 事故流程

P0/P1：指定 Incident Commander → 冻结发布 → 保存日志/Trace/审计/Release manifest → 隔离受影响 Worker/Key → 恢复服务 → 数据与租户影响评估 → 使用 `incident-review-template.md` 复盘。临时权限和绕过必须在恢复后撤销。
