# ForgeFlow 阶段 4：Developer Prompt 候选 Eval 操作手册

> 状态：v1/v2 正式付费 Eval 已于 2026-09-03 UTC 完成，自动 Gate 阻断 v2。脚本现支持通过 `-CandidatePromptVersion` 运行后续不可变候选；正式运行必须使用包含对应候选和全部门禁的干净、可追溯合并 SHA。

## 1. 目的与边界

本手册用于在完全相同的代码、Fixture、Private Grader、模型、Reasoning、价格窗口和总预算下，对照当前 `developer/v1` 与指定的不可变候选。每个版本运行 `single_agent`、`planner_developer`、`forgeflow` 三种模式和 `software/v1` 全部 30 个 Fixture。默认候选仍为 `developer/v2`；后续运行必须显式传入实际候选，例如 `-CandidatePromptVersion developer/v3`。

- 原始 Evidence、Private Grader、隐藏测试和凭据不得上传 GitHub。
- 只允许把人工脱敏后的汇总结果提交到 `release-reports`。
- Eval 结果不自动批准 Promotion；最终决定和 rollback 演练必须由 Admin 人工签署。
- `scripts/stage-4-developer-prompt-eval.ps1` 默认不发起付费调用，只有显式传入 `-ConfirmPaidEval` 才会联网。
- 两套运行共享同一个 USD 硬上限；候选运行会把当前版已消费费用作为 `prior-cost-usd` 扣除。

## 2. 为什么必须先合并脚本

Eval CLI 会拒绝脏工作区，并把 ForgeFlow、Fixture 和 Private Grader 的真实 `HEAD` 写入 Evidence。若在脚本尚未提交时运行，Evidence 无法对应一个可复现的 ForgeFlow commit。

因此顺序必须是：

1. 人工审核并合并本脚本 PR。
2. 更新本地 `main`，确认三个仓库均干净。
3. 记录三个仓库完整的 40 位 SHA。
4. 先运行 `-PreflightOnly`。
5. 复核价格、有效期、输出路径和预算后，再增加 `-ConfirmPaidEval`。

## 3. 价格窗口

价格会变化，每次执行前必须重新查看 DeepSeek 官方价格页，并把当次价格、来源、生效时间和有效截止时间写进命令。不要从旧 Evidence 或本文复制过期价格。

截至 2026-09-03，官方页面显示 `deepseek-v4-flash` 的 cache-hit、cache-miss 和 output 价格分峰值与低谷两档；工作日 UTC 01:00–04:00 和 06:00–10:00 为峰值，其余时段为低谷。该信息只是编写手册时的快照，执行者仍须在运行当天复核：

`https://api-docs.deepseek.com/quick_start/pricing/`

脚本拒绝尚未开始或已经结束的价格窗口，并默认要求窗口至少还剩 240 分钟，以降低 v1 和 v2 落入不同价格档的风险。若窗口无效或时间不足，脚本会在读取 Key 内容和任何 Provider 请求前停止。

## 4. 预检

以下示例价格仅展示参数格式，执行时必须替换为官网当时有效的值和窗口开始/结束时间。`ExpectedGitCommit` 必须是包含本脚本的合并 commit，不能继续使用 PR #21 的旧 SHA。

```powershell
./scripts/stage-4-developer-prompt-eval.ps1 `
  -ExpectedGitCommit <40位ForgeFlow合并SHA> `
  -ExpectedFixtureCommit 6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f `
  -ExpectedGraderCommit 5942ec84d403e37385203b4c7851d1b92573548a `
  -CampaignId stage4-<UTC日期>-offpeak `
  -CandidatePromptVersion developer/v3 `
  -InputUSDPerMillion <cache-miss价格> `
  -CachedInputUSDPerMillion <cache-hit价格> `
  -OutputUSDPerMillion <output价格> `
  -PricingValidFrom <RFC3339窗口开始时间> `
  -PricingValidUntil <RFC3339窗口结束时间> `
  -MaxCampaignUSD 1.00 `
  -PreflightOnly
```

`1.00 USD` 是低于既有 10 RMB 授权的保守硬上限，不代表汇率换算。脚本拒绝更高值；若确需调整，必须取得新的明确授权并通过代码审查修改上限，不能只改运行参数。

预检必须确认：

- 三个仓库的实际 `HEAD` 与参数完全一致，且工作区干净。
- Key 文件存在，但脚本不会显示 Key 内容。
- `-CandidatePromptVersion` 不是当前 `developer/v1`，且对应的 `system.txt` 和 `user.tmpl` 均存在于该精确提交。
- 当前版/候选版 Evidence 和报告路径不同，候选版本会进入文件名，且不会意外覆盖旧文件。
- 模型和 Reasoning 显示为预期值。
- 当前时间已经进入所声明的价格窗口，且有效期至少剩余 240 分钟。

## 5. 正式运行与恢复

人工复核预检输出后，使用完全相同的参数移除 `-PreflightOnly`，增加：

```powershell
-ConfirmPaidEval
```

若网络、终端或 Provider 中断，保留私有 Evidence，使用完全相同的参数增加：

```powershell
-Resume -ConfirmPaidEval
```

不要修改 Campaign ID、SHA、Prompt、模型、Reasoning、价格、预算或仓库路径。Eval 自身会再次校验现有 Evidence 配置，拒绝预算或版本漂移。不要手动拼接、复制或编辑原始 Evidence。

早期 Evidence 没有 `pricingValidFrom` 字段，仍可离线读取和审核，但不能续写为正式候选 Evidence；当前版和候选版必须使用包含该字段的新路径共同运行。

## 6. 完成后的人工门禁

脚本只有在 v1 和候选版都得到三种模式各 30 个完整 Observation 后，才生成两份私有三模式报告，以及 JSON/Markdown 两种格式的候选差异报告。差异报告会拒绝模型、Reasoning、代码、Fixture、Grader、Policy、Tool、执行环境、价格或预算漂移，只允许 Developer Prompt version 和累计 campaign cost 不同；它会列出三种模式的指标增量和现有 ForgeFlow Promotion Gate 结果，但不会代替人工批准。后续使用三模式报告调用 Promotion CLI 时会再次执行同一可比性校验，不能绕过。完成后：

1. 人工核对两份报告的 Git、Fixture、Grader、模型、Reasoning、Prompt、Policy、Tool 和价格记录一致，只有 Developer Prompt version/SHA 不同。
2. 核对完成率、隐藏测试通过率、回归率、人工介入率、成本和 P95 延迟；不得排除失败样本。
3. 参考 `release-reports/stage-4-developer-v2-review-template.md` 为实际候选生成新的人工脱敏审核记录，不复制任务正文、源码、模型原始输出、隐藏测试或原始 Evidence。
4. Admin 明确选择 `REJECTED`、`APPROVED AS CANDIDATE` 或 `APPROVED FOR PROMOTION`，记录原因和 Eval Run ID。
5. 只有 `APPROVED FOR PROMOTION` 才进入 drain、双版本镜像、Readiness、Promotion 和 rollback 演练。

脚本完成不等于阶段 4 完成；阶段 4 退出门槛仍以人工 Promotion/rollback 审计记录和真实双版本镜像验收为准。
