# 阶段 7 运维与安全演练准备审计

> 状态：待集中验收  
> 工程准备日期：2026-09-08  
> 范围：快速、非投递、非破坏性检查；不代表真实 Staging 演练通过

## 1. 已固化的工程边界

- 告警：Prometheus 的 9 条规则与合成目录一一对应；dry-run 验证 severity、脱敏 summary 和 Runbook 链接，真实投递必须显式使用 `-ConfirmNotification`。
- 备份：custom-format archive 通过可读性检查后生成 SHA-256 和 `forgeflow.backup/v1` manifest，记录 UTC、Migration 版本和字节数；保留期限制为 1～3650 天。
- 恢复：只接受 `/backups/forgeflow-<UTC>.dump` 和 `forgeflow_restore_*` 隔离数据库；要求显式确认并核对 checksum、大小、manifest 与恢复后的 Migration 版本。
- 安全：dry-run 固定 Repository、Tool、Policy、Sandbox、Security、Governance 和 Config 边界测试；真实 DIND/mTLS、网络和 Secret 检查使用单独确认开关。
- 回滚：只接受 `forgeflow.release/v2` 不可变清单；渲染的 API/Worker/Web/Caddy/Sandbox 必须与 digest 清单一致。真实执行只运行 `db check`，不执行 Down Migration，并写入新的 `forgeflow.deployment/v2` 记录。
- Demo：固定 HTTPS Origin、fixture 根路径、任务大小/凭据检测、Release/Git SHA、最新审批 ETag、允许的审批类型、最多 4 个审批和约 4 分钟轮询上限；仅保存脱敏摘要。

## 2. 快速验证命令

以下命令预计在 10 分钟内完成，不发送告警、不创建/恢复数据库、不切换版本、不访问公网 Staging：

```powershell
./scripts/validate-operations-assets.ps1
./scripts/staging-alert-test.ps1 -DryRun
./scripts/staging-security-drill.ps1 -DryRun
./scripts/staging-backup.ps1 -EnvFile <non-secret-validation-env> -DryRun
./scripts/staging-restore-drill.ps1 -EnvFile <non-secret-validation-env> -BackupFile /backups/forgeflow-20260908T000000Z.dump -ConfirmRestore -DryRun
```

`deployment-assets` 还会运行两个 POSIX 脚本的 `bash -n`、Compose 隔离渲染和前三个非投递运维 dry-run。Go 主 CI 继续覆盖上述安全包测试。

## 3. 阶段 9 阻断清单

以下项目没有在阶段 7 执行，任何一项缺少真实、脱敏、可追溯证据都阻止 Production：

1. 向真实主/备值班渠道投递并解决全部 9 个合成告警，记录接收者、时间和 Runbook 跳转结果。
2. 创建真实备份、加密异地复制，并在隔离数据库恢复；验证登录→Run→审批→报告，记录 RPO、RTO 和耗时。
3. 使用真实 Worker/Sandbox task container 验证 mTLS DIND、无公网、无 Docker Socket、资源限制、路径/命令拒绝，以及模型上下文和日志脱敏。
4. 在已 drain Worker、已人工恢复目标 Prompt/model Active Release 的前提下，用旧 digest 镜像完成应用回滚；核对 Schema 兼容、Readiness、公网版本及新审计记录。
5. 由另一名 Operator 按 `docs/demo.md` 完成 3～5 分钟演示，复核 fixture checkout 不变并保存脱敏证据。

## 4. 审计结论

阶段 7 的脚本、参数保护、静态契约、CI 接入和 Runbook 已具备集中验收条件，状态可记为 `待集中验收`。本结论不声称真实告警、恢复、安全、回滚或 Demo 已成功；这些项目必须在阶段 9 回填证据后才能改为 `已完成`。
