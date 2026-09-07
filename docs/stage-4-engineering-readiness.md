# 阶段 4 工程准备与最终验收交接

> 2026-09-07：工程准备已完成本地核对，等待人工提交本批文档。真实候选质量与 Promotion 尚未通过。

## 工程准备依据

- PR #31 已合并，基线为 `d9f5cbc7c315aedd49ab913d3c1e96f9eef90cf5`；其中包含补丁失败阶段、超时错误链和脱敏分类统计。
- `Dockerfile` 的构建阶段复制整个 `internal` 目录，再构建 API、Worker 和 CLI；`.dockerignore` 未排除 Developer Prompt。
- `internal/developer/prompt.go` 使用 `go:embed prompts/*/*`，当前候选 `developer/v4` 与回滚版本 `developer/v1` 同时进入二进制。既有测试验证 v1～v4 的不可变摘要及治理目录中的旧版本解析。这是构建配置和嵌入资源验证，实际镜像 digest 验收仍在阶段 9。
- 正式对照入口为 `scripts/stage-4-developer-prompt-eval.ps1`；治理操作入口为 `scripts/stage-4-governance-drill.ps1`。两者的真实运行均后置到阶段 9。
- 当前候选通用审批表为 `release-reports/stage-4-candidate-review-template.md`；历史 v2 审核保留原文件。

## 快速检查入口

在仓库根目录执行以下离线检查，无须模型 Key、Fixture 或 Private Grader：

```powershell
go test ./internal/developer ./internal/governance ./internal/evalexec -run 'TestPromptLoaderKeepsImmutableDeveloperPromptVersions|TestCatalogResolvesPreviousDeveloperPromptForRollback|TestPatch' -count=1 -timeout=2m
git diff --check
```

2026-09-07：上述 Go 检查通过，命令墙钟耗时约 4.3 秒。覆盖嵌入版本及摘要、治理回滚版本解析、Git 补丁应用、损坏/不匹配补丁拒绝、超时和进程启动失败。没有新增模型调用。`-timeout=2m` 是每个测试包的限制；冷缓存编译耗时不计入该限制，准备阶段超过 10 分钟时应停止并记为未完成检查，不能记为通过。

## 已准备的正式执行参数

最终窗口开始时按下表填入或复核参数；本文件不保存可直接启动付费任务的默认授权。

| 参数 | 约束 |
|---|---|
| `ExpectedGitCommit` | 本批及后续候选修复通过 PR 后的干净合并 SHA；不可直接复用本文件的工程基线 |
| `ExpectedFixtureCommit` / `ExpectedGraderCommit` | 分别核对私有仓库的完整 SHA，使用已授权的 software/v1 版本 |
| `CandidatePromptVersion` | 当前为 developer/v4，但其失败尚未解除；版本变化后重新冻结全部运行配置 |
| 样本 | 正式对照保持 30 Case × 3 Mode × 2 Prompt，共 180 Observation |
| `CampaignId` | 新的唯一标识；只在恢复同一正式运行时复用 |
| `Model` / `ReasoningEffort` | 当前版和候选版显式使用同一配置 |
| 三项价格及有效窗口 | 最终执行当天核实官方价格，禁止复用过期窗口 |
| `MaxCampaignUSD` | 显式设置，脚本允许范围为 0.01～1.00 USD；同时核对既有授权的剩余额度，不能把换 Campaign 当作重置用户总预算 |
| `MinimumWindowMinutes` | 正式运行默认要求至少剩余 240 分钟 |
| `KeyFile` | 受控本地凭据文件，仅执行器读取，值不进入文档 |
| `EvidenceRoot` / `WorkspaceRoot` | 默认 `.forgeflow/evals` / `.forgeflow/eval-worktrees`，原始数据保持私有 |

完整参数示例见 `docs/stage-4-developer-v2-eval-runbook.md`（文件名保留以兼容既有链接）。最终窗口先执行 `-PreflightOnly`，核对后才使用已有授权范围内的 `-ConfirmPaidEval`。恢复使用相同参数及 `-Resume`；smoke 不支持恢复，必须使用新的 Campaign。

## 最终窗口的执行依赖

1. 先解除当前 v4 smoke 的补丁预检失败，保留旧失败记录。修复须有独立合成回归验证，并从新的干净 SHA 获得可接受的快速 smoke；出现确定性失败时停止正式 Eval。不能因为工程准备完成就跳过此项。
2. 正式 180 Observation 对照通过自动 Gate，由 Admin 填写通用审批表并签署 Promotion 决策。
3. 完成阶段 5 的镜像构建、扫描、签名和人工上传，记录 Git SHA 与 digest；候选及回滚 Prompt 均须可从实际镜像解析。
4. 在阶段 6 的隔离 Staging 中先部署 API，保持 Worker drained，再按治理手册导入批准的 Eval、建立初始 Release、执行候选 Promotion、启动匹配 Worker 并验证 Readiness。
5. 执行不匹配、Checkpoint 和 rollback 演练，记录旧 Release ID、新 Release ID、操作者、原因和实际镜像 digest；随后完成其余 Staging、运维与安全验收。

正式对照失败时返回候选修复；镜像或环境检查失败时停止治理变更。真实验收结束前，阶段 4 最多标记为“待集中验收”。阶段 5～8 的工程准备可以继续进行。

## 当前人工提交节点

分支：`codex/stage-4-final-validation-prep`。人工审核本批工程核对、通用审批表及路线图/Runbook 修订后提交并创建 PR。合并后可以开始阶段 5 的镜像资产准备。GitHub 提交、推送、PR 创建与合并均由仓库所有者手动完成。
