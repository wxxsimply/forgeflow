# ForgeFlow Staging Operations

## 1. 上线边界

本 Compose 仅用于单机 Staging。公网只开放 Caddy 的 `80/443`，其中 80 自动跳转 HTTPS。PostgreSQL、API、Worker、Prometheus、Alertmanager 和 OTLP 均不发布宿主端口。Production 还必须完成真实 Eval Baseline、专用 Worker 主机、SBOM/漏洞扫描、备份异地复制、负载与入侵演练。

值班前必须填写并保存在团队私有运维系统：`PRIMARY_ONCALL`、`SECONDARY_ONCALL`、安全联系人、数据负责人、云厂商升级路径。仓库不保存私人联系方式。

## 2. 首次准备

服务器要求 Docker Engine + Compose v2、可解析到服务器的域名、开放 80/443、至少一个已有 Git commit 的测试仓库。完整要求见 `docs/stage-6-staging-infrastructure.md`。部署账号独占 `deploy/staging/secrets`，Secret 必须是该账号拥有的普通非符号链接文件，权限设为 `0600`。

```powershell
Copy-Item deploy/staging/staging.env.example deploy/staging/staging.env
# 填写批准的 40 位 Git SHA、域名、仓库路径和全部 image@sha256 digest。
# 首次 Bootstrap 时取消 FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL 的注释并填入管理员邮箱。
# 在主机上创建 postgres_password、postgres_dsn、alert_webhook_url。
./scripts/staging-preflight.ps1 `
  -EnvFile deploy/staging/staging.env `
  -Manifest .forgeflow/release/0.12.0-rc.1/release-manifest.json `
  -RequireDigests
```

`postgres_dsn` 中密码必须 URL encode。人工使用独立 read-only pull token 执行 Registry 登录，凭据保留在仓库外。正式部署始终使用 `-RequireDigests`；ForgeFlow 和第三方镜像都必须是 `image@sha256:<64位 digest>`。Preflight 还要求当前源码 HEAD、环境文件和 Release manifest 完全一致。

## 3. 首次管理员与部署

服务器不构建镜像。`staging-release.ps1` 只校验、拉取并按 digest 部署阶段 5 已审核的 Release manifest。Bootstrap 只运行一次：创建 `bootstrap_admin_password` Secret，在环境文件中设置管理员邮箱，然后执行：

```powershell
$manifest = '.forgeflow/release/0.12.0-rc.1/release-manifest.json'
./scripts/staging-release.ps1 `
  -Release 0.12.0-rc.1 `
  -Manifest $manifest `
  -EnvFile deploy/staging/staging.env `
  -IncludeBootstrap `
  -ConfirmDeploy
```

Bootstrap 部署故意不启动 Worker。确认管理员登录后，立即通过受控脚本删除一次性 Secret，并使用不含 Bootstrap 的基础 Compose 重建 API；随后从 `staging.env` 删除 `FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL`：

```powershell
./scripts/staging-bootstrap-cleanup.ps1 `
  -BaseUri https://<domain> `
  -Email <admin-email> `
  -Password (Read-Host -AsSecureString) `
  -ConfirmRemoval
```

随后创建 `openai_api_key`，用同一个 Release、同一个 manifest 启动完整 Worker/Sandbox；不要在 Bootstrap 和正常部署之间更换镜像：

```powershell
./scripts/staging-release.ps1 `
  -Release 0.12.0-rc.1 `
  -Manifest $manifest `
  -EnvFile deploy/staging/staging.env `
  -IncludeOpenAI `
  -ConfirmDeploy
```

部署顺序固定为：验证不可变 Release manifest → 拉取镜像 → PostgreSQL 健康 → 显式 Migration → API/Worker/Web 健康 → Caddy。OpenAI 模式还会把审核过的 Sandbox 镜像导入隔离 DIND。一次性 Bootstrap 记录写入 `<版本>-bootstrap-deployment.json` 且不覆盖当前正式版本；完整部署记录写入 `.forgeflow/deploy/releases/<版本>-deployment.json`，包含前一版本、Git SHA、manifest SHA、实际镜像和健康元数据。

Prompt Promotion 不会对运行中的 Worker 做隐式热替换。候选镜像必须同时保留可回滚的旧 Prompt，并按“drain Worker → 部署候选 API（Worker 暂停）→ 导入真实 Eval → Admin Promotion → 使用与 Active Release 一致的 Prompt/模型环境重启 Worker”的顺序发布。Promotion/rollback 表是治理记录，不等同于镜像发布；`FORGEFLOW_GOVERNANCE_ENFORCE_ACTIVE_RELEASES=true` 时，任一 Agent 的 Prompt version、Prompt SHA 或模型与 Active Release 不一致都会让 Worker 启动预检或 `/readyz` 失败，并在领取新 Job 前再次阻断。首次启用门禁时先保持 Worker 停止，只启动 Migration/API，完成四个 Agent 的初始 Promotion 后再启动 Worker。

## 4. 日常检查

```powershell
docker compose --env-file deploy/staging/staging.env -f deploy/staging/compose.yaml ps
Invoke-WebRequest https://<domain>/healthz
docker compose --env-file deploy/staging/staging.env -f deploy/staging/compose.yaml exec worker wget -qO- http://127.0.0.1:9091/readyz
docker compose --env-file deploy/staging/staging.env -f deploy/staging/compose.yaml logs --since 30m api worker
```

Prometheus/Alertmanager 不公开。使用 SSH 本地端口转发或临时 `docker compose port` 诊断，不允许长期发布管理端口。日志禁止包含 Cookie、密码、API Key、任务正文和完整仓库文件。

完整部署后在阶段 9 运行公网验收。脚本要求 HTTPS，核对 API/Worker/Web 的 Release 和 Git SHA、Prompt/model readiness、浏览器登录到报告链路，并确认测试仓库执行前后完全不变：

```powershell
./scripts/staging-acceptance.ps1 `
  -BaseUri https://<domain> `
  -ExpectedRelease 0.12.0-rc.1 `
  -ExpectedGitCommit <approved-40-character-sha> `
  -Manifest $manifest `
  -Email <staging-user> `
  -Password (Read-Host -AsSecureString) `
  -RepositoryHostPath <host-fixture-repository> `
  -RepositoryContainerPath /repositories/demo `
  -IncludeOpenAI
```

验收证据写入被忽略的 `.forgeflow/staging/acceptance`。提交或发布前必须脱敏；不得上传 Cookie、密码、API Key、原始 Evidence、Private Grader 或隐藏测试。

## 5. Worker drain 与维护

完整 Development Graph 使用 `compose.openai.yaml` 中的专用 `sandbox-engine`。Worker 仅通过内部 `worker-execution` 网络和 dind 自动生成的双向 TLS 客户端证书访问它，不挂载宿主 `/var/run/docker.sock`；证书 volume 在 Worker 中只读。sandbox engine 的特权边界只用于创建受限任务容器，不接受公网流量。任务容器仍由 Worker 强制设置无网络、非 root、只读根文件系统、零 capabilities、PID/CPU/内存/超时限制。升级 Docker daemon 镜像前必须重新运行安全演练。

宿主机的 `FORGEFLOW_REPOSITORY_PATH` 必须允许容器 UID/GID `10001:10001` 创建和清理 Git worktree 元数据；不要使用 `chmod 777`。API 只读挂载 workspace volume，用于恢复前兼容性校验，不持有 Docker endpoint 或模型密钥。

1. 暂停创建新 Run 或在入口返回维护状态。
2. 观察 `forgeflow_queue_depth`，等待活动 Job 完成。
3. `docker compose stop -t 90 worker`；未完成 Job 的租约到期后可恢复。
4. 完成维护后启动 Worker，检查 lease lost、queue depth 和失败 Run。

不要强制删除 workspaces；必须先按 Run/Checkpoint 判断其是否仍被引用。

## 6. 备份与恢复

阶段 7 先渲染 Compose，不创建备份：

```powershell
./scripts/staging-backup.ps1 -EnvFile deploy/staging/staging.env -RetentionDays 14 -DryRun
./scripts/staging-restore-drill.ps1 -EnvFile deploy/staging/staging.env -BackupFile /backups/forgeflow-20260908T000000Z.dump -ConfirmRestore -DryRun
```

阶段 9 再在真实 Staging 每日执行，并将 `.dump`、`.dump.sha256`、`.dump.manifest` 加密复制到异地介质：

```powershell
./scripts/staging-backup.ps1 -RetentionDays 14
```

manifest 固定记录 UTC 创建时间、Migration 版本、SHA-256 和字节数。每月至少一次恢复到隔离数据库；脚本只接受 `/backups/forgeflow-<UTC>.dump`、要求 checksum/manifest，并只允许 `forgeflow_restore_*`，不会覆盖在线 `forgeflow`：

```powershell
./scripts/staging-restore-drill.ps1 -BackupFile /backups/forgeflow-20260811T120000Z.dump -ConfirmRestore
```

恢复成功标准：SHA-256、大小与 manifest 相符，archive list 通过，恢复后的 Migration 版本等于备份记录，API 使用恢复库启动并通过登录→Run→审批→报告 E2E。建议 Staging RPO 24 小时、RTO 4 小时；Production 指标必须由数据负责人确认。Artifact Volume 需独立快照并与数据库时间点对齐。

## 7. 发布与回滚

阶段 7 只对旧的 v2 Release manifest、digest 镜像和 Compose 渲染做 dry-run：

```powershell
./scripts/staging-rollback.ps1 -Manifest .forgeflow/release/0.11.0/release-manifest.json -ConfirmRollback -DryRun
```

阶段 9 真实执行前先暂停新 Run、等待活动 Job 完成，并确认目标 Prompt/model Release 已按治理 API 恢复为 Active。去掉 `-DryRun` 后，脚本会核对当前部署记录与公网版本、停止 Worker、拉取旧 digest 镜像、使用目标 API 镜像运行 `db check`，再启动服务并验证 Worker Readiness。应用回滚绝不自动执行 Down Migration；不兼容会直接阻止。数据库回滚只能按事故流程恢复到新数据库，完成一致性检查后切换 DSN，禁止覆盖在线 Production。

## 8. Secret 轮换

- OpenAI Key：创建新 Key → 更新 Worker Secret → 重建 Worker → 验证模型调用 → 撤销旧 Key。
- PostgreSQL：进入维护窗口 → 创建新角色/密码 → 更新 DSN 和 DB Secret → 重启迁移/API/Worker并验证 → 删除旧角色。
- Admin 密码/Session：修改密码并撤销其他 Session；疑似泄露时撤销全部 Session。
- Alert webhook：更新 Secret，重启 Alertmanager，发送测试告警后撤销旧 webhook。

轮换中不得把 Secret 写入命令历史、Issue、聊天或普通日志。

## 9. 告警 Runbook

阶段 7 用全部九条合成载荷验证规则、Runbook 链接和脱敏，不联系 Alertmanager：

```powershell
./scripts/staging-alert-test.ps1 -DryRun
```

阶段 9 才允许向真实私有值班渠道投递。先通知值班人员，再执行并人工确认收到和恢复消息：

```powershell
./scripts/staging-alert-test.ps1 -EnvFile deploy/staging/staging.env -ConfirmNotification
```

消息中不得包含任务正文、源代码、Cookie、Key、密码或其他 Secret。未收到消息时检查 Alertmanager 的 `url_file` Secret、路由和网络，不把 webhook 写进配置或日志。

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
