# ForgeFlow 阶段 4：Developer v2 候选审核记录

> 状态：`AUTOMATIC GATE BLOCKED / AWAITING OWNER SIGNATURE`。本记录只包含脱敏聚合指标和不可逆摘要，不包含原始 Evidence、任务正文、源码、模型原始输出、Private Grader、隐藏测试或凭据。

## 1. 审核对象

| 项目 | 值 |
|---|---|
| Eval 完成 UTC 时间 | `2026-09-03T18:18:56Z` |
| 审核人 | `<待仓库所有者填写>` |
| Campaign / Eval Run ID | `stage4-dev-v2-20260904-main63a3117` |
| ForgeFlow Git SHA | `63a311779bd20109a0e640367e9898d2e22cb683` |
| Fixture Git SHA | `6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f` |
| Private Grader Git SHA | `5942ec84d403e37385203b4c7851d1b92573548a` |
| 当前 Prompt | `developer/v1`，SHA-256 `2df88ab928a6d596d6719c377bf112f9996b08419bd5ef311faad897b5a66cc9` |
| 候选 Prompt | `developer/v2`，SHA-256 `4b64cdc12b07b6f6ba1c12e56d912f1ae0fa2e0d5b9ad1e395bb0dd7c91da1c7` |
| Provider / 模型 / Reasoning | `deepseek / deepseek-v4-flash / low` |
| 执行环境 | `windows/amd64 go1.26.6` |
| Policy / Tool 版本 | `eval-policy/v1`；`eval-tools/v1` |
| 官方价格来源 | `https://api-docs.deepseek.com/quick_start/pricing/` |
| 价格有效窗口 | `2026-09-03T10:00:00Z` 至 `2026-09-04T01:00:00Z` |
| 每百万 Token 价格 | cache hit `$0.007`；cache miss `$0.22`；output `$0.66` |
| 当前 Evidence SHA-256 | `61ca437a02122aaea7084fc40d1879bd58029197b4a95569d35cb3455b60a6a1` |
| 候选 Evidence SHA-256 | `404ad203e7b62659fa21028c2a959932df3ed152463e81bdde279b4421d6e5c3` |
| 候选差异报告 SHA-256 | `00912d554aa63545b40e19b2ca5da89b6cae77c7e838a6ac382977de0f309856` |
| 当前 / 候选实测成本 USD | `$0.378068088` / `$0.376751668` |
| 共享 campaign cost USD | `$0.754819756`，硬上限 `$1.00` |

## 2. Evidence 和可比性门禁

- [x] v1 与 v2 均包含三种模式各 30 个终态 Observation，共 180 条。
- [x] `forgeflow eval compare` 成功，除 Developer Prompt version/SHA 和累计 campaign cost 外没有配置漂移。
- [x] 候选 `priorCostUsd` 为 `$0.378068088`，等于当前运行实测总费用。
- [x] 完成率、隐藏测试、回归、人工介入、成本和 P95 延迟均有真实测量值。
- [x] 失败、拒绝、超时和人工介入样本没有从统计中排除。
- [x] Git 候选修改不包含原始 Evidence、任务正文、源码快照、模型原始输出、Private Grader、隐藏测试或凭据。

## 3. 三模式结果

所有 delta 均为“候选减当前”。

| Mode | 当前 Passed | 候选 Passed | Completion Δ | Hidden tests Δ | Regression Δ | Human intervention Δ | Avg cost USD Δ | P95 latency ms Δ |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `single_agent` | `14/30` | `12/30` | `-6.67%` | `-10.00%` | `-10.00%` | `+0.00%` | `-0.0005` | `-6723` |
| `planner_developer` | `13/30` | `7/30` | `-20.00%` | `-20.00%` | `+0.00%` | `-3.33%` | `+0.0008` | `+13394` |
| `forgeflow` | `9/30` | `6/30` | `-10.00%` | `-10.00%` | `+3.33%` | `+3.33%` | `-0.0004` | `-44480` |

## 4. 自动 Promotion Gate

| 项目 | 值 |
|---|---|
| Gate allowed | `false` |
| Gate reasons | `completion_regression`、`hidden_test_regression`、`regression_rate_increase`、`deterministic_failure:bugfix-02` |
| Completion 最大下降 | `2%` |
| Hidden Test 最大下降 | `1%` |
| Regression 最大增加 | `0` |
| 平均成本最大增加 | `10%` |
| P95 延迟最大增加 | `15%` |

自动 Gate 已阻断 `developer/v2`，因此不得选择 `APPROVED FOR PROMOTION`，不得对 v2 执行 Promotion 或双版本部署演练。

## 5. 人工结论

允许的结论仅剩：

- [ ] `REJECTED / RERUN REQUIRED`

建议理由：`developer/v2` 在三种模式下均出现完成率和隐藏测试回退，并触发确定性失败；保留 v1 为当前版本，使用新的不可变版本号迭代候选，不原地修改 v2。

签署记录：`<待仓库所有者在对应 GitHub PR 中明确确认>`

## 6. 后续处理

- `developer/v1` 继续作为当前生产默认和回滚目标。
- `developer/v2` 保留为不可变、已评估但被阻断的历史候选。
- 后续改进使用 `developer/v3`，必须在其代码合并后，从新的干净精确 Git SHA 重新执行完整 v1/v3 对照。
- 本次没有 Promotion Release ID 或 Rollback Release ID；任何此类记录均为不适用，不得伪造。
