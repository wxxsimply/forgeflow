# ForgeFlow 阶段 4：Developer v2 候选人工审核

> 状态：`DRAFT / NOT APPROVED`。本模板不包含真实成绩，也不构成 Promotion 授权。

## 1. 审核对象

| 项目 | 值 |
|---|---|
| 审核 UTC 时间 | `<待填写>` |
| 审核人 | `<待填写>` |
| ForgeFlow Git SHA | `<40位SHA>` |
| Fixture Git SHA | `<40位SHA>` |
| Private Grader Git SHA | `<40位SHA>` |
| 当前 Prompt | `developer/v1`，SHA-256 `<待填写>` |
| 候选 Prompt | `developer/v2`，SHA-256 `<待填写>` |
| Provider / 模型 / Reasoning | `<待填写>` |
| Policy / Tool 版本 | `<待填写>` |
| 官方价格来源和有效窗口 | `<待填写>` |
| 当前 Evidence SHA-256 | `<待填写；只记录摘要>` |
| 候选 Evidence SHA-256 | `<待填写；只记录摘要>` |
| 候选差异报告 SHA-256 | `<待填写>` |
| 共享 campaign cost USD | `<待填写>` |

## 2. Evidence 和可比性门禁

- [ ] v1 与 v2 均包含三种模式各 30 个终态 Observation。
- [ ] `forgeflow eval compare` 成功，除 Developer Prompt version/SHA 和累计 campaign cost 外没有配置漂移。
- [ ] 候选 `priorCostUsd` 等于当前运行的已有 prior 加三种模式实测总费用。
- [ ] 完成率、隐藏测试、回归、人工介入、成本和 P95 延迟均有真实测量值。
- [ ] 失败、拒绝、超时和人工介入样本没有从统计中排除。
- [ ] 原始 Evidence、任务正文、源码、模型原始输出、Private Grader、隐藏测试和凭据未进入 Git 候选修改。

## 3. 三模式结果

所有 delta 均为“候选减当前”。不得用结构校验或估算值替代真实模型成绩。

| Mode | 当前 Passed | 候选 Passed | Completion Δ | Hidden tests Δ | Regression Δ | Human intervention Δ | Avg cost USD Δ | P95 latency ms Δ |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `single_agent` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` |
| `planner_developer` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` |
| `forgeflow` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` | `<待填写>` |

## 4. 自动 Promotion Gate

| 项目 | 值 |
|---|---|
| Gate allowed | `<true/false>` |
| Gate reasons | `<逐项填写>` |
| Completion 最大下降 | `2%` |
| Hidden Test 最大下降 | `1%` |
| Regression 最大增加 | `0` |
| 平均成本最大增加 | `10%` |
| P95 延迟最大增加 | `15%` |

自动 Gate 通过不等于人工批准；自动 Gate 阻断时不得选择 `APPROVED FOR PROMOTION`。

## 5. 人工结论

仅选择一项：

- [ ] `REJECTED / RERUN REQUIRED`：证据、可比性或指标不满足要求。
- [ ] `APPROVED AS CANDIDATE ONLY`：保留候选，但不允许 Promotion。
- [ ] `APPROVED FOR PROMOTION`：允许进入 drain、双版本镜像、Readiness 和 rollback 演练；不等于 Production 发布批准。

结论理由：`<待填写>`

签署记录：`<GitHub PR/Issue 或其他不可变人工记录>`

## 6. 后续演练记录

| 项目 | 值 |
|---|---|
| Eval Run ID | `<待填写>` |
| Promotion Release ID | `<待填写>` |
| Rollback Release ID | `<待填写>` |
| 双版本镜像 Git SHA / digest | `<待填写>` |
| Worker drain 时间 | `<待填写>` |
| Promotion 后 Readiness | `<待填写>` |
| Rollback 后 Readiness | `<待填写>` |
| 已运行 Run 版本未混用 | `<待填写>` |

在真实演练完成并由 Admin 签署前，阶段 4 必须保持“进行中”。
