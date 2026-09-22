# ForgeFlow 个人预览候选 smoke 手册

> 状态：PERSONAL-006 **实施就绪，待付费实测**。本文件和配套脚本只完成安全执行边界、严格 GO/NO-GO 判定与离线测试；本次 PR 不调用模型，也不能把 PERSONAL-006 标记为完成。

## 1. 目标与固定范围

候选 smoke 只回答一个问题：在相同的 `feature-01` Fixture 上，生产基线 `developer/v1` 和已审核候选 `developer/v4` 是否都能完成 Developer JSON 解码、变更集检查、diff 预检与应用、显式测试和私有隐藏测试。

范围固定为：

- 模式：`planner_developer`；
- 每个 Prompt 1 个 Case，共 2 个 Observation；
- 共享费用硬上限：USD 0.10；
- Fixture、Private Grader、ForgeFlow 都必须绑定干净的完整 Git SHA；
- 只允许已经审核的 Fixture 数据范围，不发送凭据、Private Grader、隐藏测试或其他仓库内容；
- smoke 不是 Promotion 证据，不授权部署、扩大用户范围或修改生产 Prompt。

历史 smoke 在安全 diff 规范化合并前曾于 `patch_check` 阶段失败，不得沿用为通过证据。必须从包含本工具的新 `main` 完整 SHA 创建新 Campaign。

## 2. 创建私有计划

合并本 PR 后，在干净的最新 `main` 执行：

```powershell
New-Item -ItemType Directory -Force .forgeflow/personal-preview/candidate-smoke | Out-Null
Copy-Item deploy/personal-preview/candidate-smoke-plan.example.json .forgeflow/personal-preview/candidate-smoke/plan.json
notepad .forgeflow/personal-preview/candidate-smoke/plan.json
```

只在私有计划里填写：

1. 当前 `git rev-parse HEAD` 的 40 位小写 SHA；
2. 新 Campaign ID，格式为 `personal-preview-YYYYMMDD-NN`；
3. 干净 Fixture 与 Private Grader 的路径和完整 SHA；
4. 密钥文件路径，不是密钥内容；
5. 执行当时核对的官方价格及有效时间窗口。

私有计划、原始 Evidence、模型输出、补丁、路径、Private Grader 和决策文件均在 `.forgeflow/` 下，不得提交 Git。示例中的价格占位符必须替换，不能复用历史价格窗口。

## 3. 无调用预检

```powershell
./scripts/personal-preview-candidate-smoke.ps1 -Plan .forgeflow/personal-preview/candidate-smoke/plan.json
```

预检验证计划、预算、Prompt、Fixture、Grader、价格窗口、当前完整 SHA 和干净工作区。没有 `-Execute` 时禁止模型请求，也不会读取或要求密钥文件存在；只有明确执行时才检查计划中的密钥文件路径。

任何 SHA、仓库状态、价格、路径或数据范围不明确都应停止，不要为了通过预检填写猜测值。

## 4. 单独授权后执行

只有仓库所有者在本次 Campaign 前再次确认数据范围和 USD 0.10 以内费用，才能手动执行：

```powershell
./scripts/personal-preview-candidate-smoke.ps1 `
  -Plan .forgeflow/personal-preview/candidate-smoke/plan.json `
  -Execute `
  -ConfirmApprovedDataScope `
  -ConfirmPaidSmoke
```

缺少任一确认开关都不会调用 Provider。脚本复用现有 Eval 执行器，生成私有 summary 后再执行严格判定；不会自动重试失败 Campaign，也不会运行 180 Observation 正式 Eval。

## 5. GO 与 NO-GO

仅当以下条件全部满足时，私有 `personal-decision.json` 才记录 `GO`：

- summary Schema、Campaign、Prompt、Case 数和模式完全匹配；
- 两个 Prompt 都是 1/1 通过；
- completion 与隐藏测试通过率均为 100%；
- regression 与人工介入率均为 0；
- 费用和延迟都有真实记录；
- 共享记录费用不超过计划上限。

任何 JSON、diff、apply、测试、超时、费用、基础设施或证据错误都记录或视为 `NO-GO`，脚本以失败退出。不得手工把失败 JSON 改成 GO，也不得使用同一 Campaign 覆盖原 Evidence。

GO 之后只能把经过脱敏的日期、完整候选 SHA、两侧通过数、总费用和人工结论回填到公开计划。不要提交原始 summary 或 decision；公开记录前再次扫描路径、任务内容、补丁、隐藏测试名称和凭据。
