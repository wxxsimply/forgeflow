# ForgeFlow 3～5 分钟演示

本 Runbook 供另一名 Operator 在阶段 9 的真实 Staging 验收窗口执行。阶段 7 只校验脚本和边界，不联系 Staging，也不产生模型费用。

## 演示前检查

1. 使用只含演示代码的 Git fixture；容器路径必须是 `/repositories/<名称>`，并确认它有可解析的 `HEAD`。
2. 从当前 `.forgeflow/deploy/current.json` 记录版本与 40 位 Git SHA；确认健康检查、备份和告警负责人已就绪。
3. 准备可审批的专用 Admin/Operator 账号。密码仅通过 `Read-Host -AsSecureString` 输入，不写入命令、文件或聊天。
4. 人工阅读本次 `Task`，确认不含凭据、真实私有仓库数据或外部副作用，再传入 `-ConfirmApprovals`。
5. 浏览器打开 HTTPS 登录页、Run 列表、Approvals 和只读监控视图。另一名观察者记录开始时间。

## 3～5 分钟讲解顺序

**0:00–0:40 安全边界。** 展示 HTTPS、登录和角色；说明 API 没有模型 Key/Docker，Worker 与数据库不暴露公网，修改只发生在隔离 worktree。

**0:40–1:30 创建 Run。** 登记 fixture，创建一个小任务；展示 Idempotency-Key、预算、固定 Release/Git SHA 和计划审批状态。

**1:30–2:30 人工审批。** 依次核对计划、`apply_patch` 和可能出现的 `judge_security` 审批。脚本最多接受 4 个审批，只接受受支持动作，并用最新 ETag 提交；遇到未知工具或越界 scope 会停止。

**2:30–3:50 受控执行。** 展示 Graph、Node Trace、Token/成本、Tool allow/deny、测试退出码及独立 Reviewer/Security 结果。指出 Sandbox 默认无公网且有 CPU、内存、PID 和超时上限。

**3:50–5:00 交付与运维。** 展示 Diff、Patch SHA-256、测试 Evidence、最终报告、告警 Runbook、备份 manifest，以及回滚只运行 Schema 兼容检查而不执行 Down Migration 的约束。

## 可重复 API 验证

```powershell
$password = Read-Host "Demo password" -AsSecureString
./scripts/demo-staging.ps1 `
  -BaseUri https://forgeflow-staging.example.com `
  -Email demo@example.com `
  -Password $password `
  -RepositoryPath /repositories/demo `
  -ExpectedRelease 0.12.0-rc.1 `
  -ExpectedGitCommit <approved-40-character-sha> `
  -ConfirmApprovals
```

脚本最长轮询约 4 分钟，依次验证健康、登录、Repository、Run、每次审批 ETag、终态和报告。它只向 `.forgeflow/staging/demo/<run-id>.json` 写入脱敏证据：Run 状态、Release、Git SHA、耗时、仓库路径 SHA-256 和审批摘要；不写任务正文、密码、原始报告、源代码或仓库原路径。

## 失败处置

- 未完成、未知审批、scope 越界、版本不一致或超时：停止演示，保留 Run ID，不改为真实仓库重试。
- ETag 冲突：回到网页重新阅读最新审批，不复用旧批准。
- 运行结束后确认原 fixture checkout 不变，并由观察者记录脱敏证据路径与结果。
- 真实 Demo、模型费用和人工签署仅在阶段 9 执行；阶段 7 不据此声称演示已通过。
