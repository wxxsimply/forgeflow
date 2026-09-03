# ForgeFlow 阶段 4：Promotion 与 rollback 演练手册

> 状态：操作工具已就绪，尚未执行真实 Promotion/rollback，也未代替 Admin 人工批准或签署。

## 1. 安全边界

`scripts/stage-4-governance-drill.ps1` 默认执行只读的 `Inspect`。导入 Eval、Promotion 和 rollback 分别要求 `-ConfirmEvalImport`、`-ConfirmPromotion` 和 `-ConfirmRollback`，确认开关不能跨动作复用。

- 密码必须通过 `Read-Host -AsSecureString` 输入，禁止写入命令、脚本或仓库。
- HTTPS 是默认要求；只有显式传入 `-AllowInsecureLocalhost` 时才允许本机 HTTP 隔离环境。
- 私有操作记录写入 `.forgeflow/governance-drills`，该目录已被 Git 忽略。
- 记录只包含操作者 ID、Eval/Release ID、Prompt SHA、模型、时间和 Readiness 状态，不保存密码、CSRF、原始 Evidence、任务正文、源码、模型输出或隐藏测试。
- 脚本不负责启动、停止或 drain Worker，不自动决定 Promotion，也不生成或代签人工结论。

## 2. 前置条件

1. Developer v1/v2 正式 Eval 已完成，候选差异报告通过自动 Gate，并由 Admin 明确选择 `APPROVED FOR PROMOTION`。
2. 只使用已经合并、通过必需 CI 且同时嵌入当前 Prompt 与回滚 Prompt 的精确镜像 Git SHA。
3. 隔离数据库已执行 Migration 5，API 已启动，Worker 保持 drained。
4. API 的 Bootstrap Admin 凭据已从部署配置中移除；本次使用现有 Admin 登录。
5. 原始 Evidence 只存在于受控私有路径，Private Grader、隐藏测试和凭据不进入工作区或操作记录。

## 3. 只读预检

```powershell
$adminPassword = Read-Host 'ForgeFlow Admin password' -AsSecureString
./scripts/stage-4-governance-drill.ps1 `
  -BaseUri https://<staging-domain> `
  -Email <admin-email> `
  -Password $adminPassword `
  -Action Inspect
```

隔离本机环境可额外使用 `-AllowInsecureLocalhost`，但 `BaseUri` 必须是 `http://127.0.0.1:<port>`、`http://localhost:<port>` 或 IPv6 loopback。

## 4. 导入经批准的 Eval

每个 Evidence 文件必须包含同一 Prompt 版本下的三种模式。脚本只把文件发给指定的 ForgeFlow API，不向模型 Provider 发起调用；服务端重新构建报告并返回 Eval Run ID。

```powershell
./scripts/stage-4-governance-drill.ps1 `
  -BaseUri https://<staging-domain> `
  -Email <admin-email> `
  -Password $adminPassword `
  -Action ImportEval `
  -EvidencePath .forgeflow/evals/<approved-v2-evidence>.json `
  -ConfirmEvalImport
```

保存命令输出中的 `evalRunId`，并与人工批准记录逐字核对。不要把 `.forgeflow` 中的 JSON 上传 GitHub。

首次启用治理时，对 planner、developer、reviewer、security 分别使用与其 Prompt 绑定一致的已批准 Eval Run 创建初始 Release。每个动作使用独立、具体的 `Comment`。

## 5. Promotion

先确认 Worker 已 drained，再执行：

```powershell
./scripts/stage-4-governance-drill.ps1 `
  -BaseUri https://<staging-domain> `
  -Email <admin-email> `
  -Password $adminPassword `
  -Action Promote `
  -Agent developer `
  -PromptVersion developer/v2 `
  -EvalRunId <approved-v2-eval-run-id> `
  -Comment 'Admin-approved Developer v2 controlled promotion; see signed review record.' `
  -ConfirmPromotion
```

脚本会在变更前确认候选 Prompt 已嵌入运行中的 API 镜像，并在变更后确认 Release 历史恰好增加一条、该 Agent 只有一个 Active Release。它不会启动 Worker。

随后部署或启动配置为 `developer/v2`、且仍嵌入 `developer/v1` 的 Worker，并验证：

```powershell
./scripts/stage-4-governance-drill.ps1 `
  -BaseUri https://<staging-domain> `
  -Email <admin-email> `
  -Password $adminPassword `
  -Action Inspect `
  -WorkerReadinessUri https://<worker-readiness-endpoint>/readyz `
  -ExpectedReadinessStatus 200
```

如果 Worker Readiness 仅在容器网络内暴露，使用 `docker compose ... exec worker wget -qO- http://127.0.0.1:9091/readyz`，不要为了演练把 Worker 管理端口暴露到公网。

## 6. 不匹配与 checkpoint 验证

在隔离环境完成以下人工步骤并记录 UTC 时间：

1. 保存一个可暂停 Run 的 Prompt、模型、Policy、Tool 和 Git base 绑定。
2. 使用与 Active Release 不一致的 Worker 配置启动受控实例，确认 `/readyz` 为 503 且不领取新 Job。
3. 恢复匹配配置，确认 `/readyz` 为 200。
4. Promotion 前后重新读取已暂停 Run，确认 checkpoint 绑定逐字段未改变。

检查 503 时可把本机或安全内网 Readiness URI 传给脚本，并使用 `-ExpectedReadinessStatus 503`。脚本只验证状态，不改变 Worker 配置。

## 7. Rollback

`TargetReleaseId` 必须是 Promotion 前保存的旧 Developer Release ID，而不是刚创建的 v2 Release ID。先 drain Worker，再执行：

```powershell
./scripts/stage-4-governance-drill.ps1 `
  -BaseUri https://<staging-domain> `
  -Email <admin-email> `
  -Password $adminPassword `
  -Action Rollback `
  -Agent developer `
  -TargetReleaseId <old-developer-v1-release-id> `
  -Comment 'Admin-approved rollback drill to the previously active Developer v1 release.' `
  -ConfirmRollback
```

脚本会确认 rollback 新增一条不可变 Release、`rollbackOf` 指向 rollback 前的 Active Release，且最终只有新记录为 Active。随后使用目标版本配置重启 Worker并确认 `/readyz` 为 200。

## 8. 人工验收记录

演练完成后，由 Admin 人工向 `docs/stage-4-prompt-model-governance-audit.md` 追加：

- UTC 开始/结束时间和操作者。
- 已批准 Eval Run ID。
- Promotion、旧目标和 rollback Release ID。
- API/Worker 镜像 Git SHA 与镜像 digest。
- 匹配时 200、不匹配时 503、旧 Worker 不取 Job、checkpoint 未改写的结果。
- 最终版本和 rollback 后 Readiness。

只提交脱敏结论；不要复制 `.forgeflow/governance-drills`、原始 Evidence、日志中的凭据、Private Grader 或隐藏测试。
