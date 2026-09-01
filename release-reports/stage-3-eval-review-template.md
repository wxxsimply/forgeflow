# ForgeFlow 阶段 3 Eval 人工审核与 Promotion 结论

> 状态：`DRAFT / NOT APPROVED`
> 说明：只有三种模式各 30 个终态 Observation、完整指标和人工复核全部完成后，才能复制并签署本模板。`TBD` 不得替换为估算或虚构数据。

## 1. 审核范围与可追溯性

| 字段 | 审核值 |
|---|---|
| Dataset / version | `software/v1` / `TBD` |
| 主仓库 Git commit | `TBD（完整 40 位 SHA）` |
| Fixture repository commit | `TBD（完整 40 位 SHA）` |
| Private Grader commit | `TBD（完整 40 位 SHA）` |
| Provider / model | `TBD` |
| Reasoning effort | `TBD` |
| Prompt versions | `TBD` |
| Policy / Tool versions | `TBD` |
| Execution environment | `TBD` |
| Pricing mode / source | `TBD` |
| 连续价格窗口 | `TBD（UTC 起止时间）` |
| 私有 Evidence SHA-256 | `TBD；只记录摘要，不提交 Evidence` |
| JSON / Markdown 报告 SHA-256 | `TBD` |

## 2. Evidence 完整性门禁

- [ ] `single_agent`、`planner_developer`、`forgeflow` 各有 30 个唯一 Case，共 90 个终态 Observation。
- [ ] 配置、Dataset 和 Case ID 与锁定版本一致，没有跨价格窗口或跨 Git commit 拼接。
- [ ] 失败、拒绝、超时、审批请求和人工介入全部保留，没有删除后重跑成成功。
- [ ] 每个 Observation 都有真实 Token、成本和延迟；缺失指标没有被估算值填充。
- [ ] 显式测试来自实际退出码，Private Grader 在 Agent 工作区外运行。
- [ ] Secret 与危险命令结果已复核；任何非零结果均有阻断结论或正式风险处置。
- [ ] 原始 Evidence、Fixture 内容、Private Grader 和隐藏测试没有进入 Git/GitHub。

任一项未勾选时，结论必须为 `REJECTED / INCOMPLETE`。

## 3. 三基线脱敏指标

| Mode | 完成数 | Completion | Hidden tests | Regression | Human intervention | Avg cost USD | P95 latency ms |
|---|---:|---:|---:|---:|---:|---:|---:|
| `single_agent` | `TBD/30` | `TBD` | `TBD` | `TBD` | `TBD` | `TBD` | `TBD` |
| `planner_developer` | `TBD/30` | `TBD` | `TBD` | `TBD` | `TBD` | `TBD` | `TBD` |
| `forgeflow` | `TBD/30` | `TBD` | `TBD` | `TBD` | `TBD` | `TBD` | `TBD` |

## 4. 失败与安全样本核对

| 类别 | 数量 | 脱敏说明 / 审核结论 |
|---|---:|---|
| `model_output_invalid` | `TBD` | `TBD` |
| 显式测试失败 | `TBD` | `TBD` |
| 隐藏测试失败 | `TBD` | `TBD` |
| 审批 / 人工介入 | `TBD` | `TBD` |
| Secret 检测 | `TBD` | `TBD` |
| 危险命令执行 | `TBD` | `TBD` |
| 超时 / Provider / 基础设施失败 | `TBD` | `TBD` |

## 5. 成本、隐私和发布边界

- 授权费用上限：`TBD`。
- 实际 Provider 费用：`TBD`；换算来源与时间：`TBD`。
- [ ] 实际费用未超过人工授权上限。
- [ ] Provider 只收到批准的数据范围。
- [ ] 发布版只包含汇总指标、版本和结论，不包含可还原私有任务或源码的信息。
- [ ] 报告中的 URL、路径、身份和错误信息已脱敏。

## 6. 自动门禁与人工结论

自动 Promotion Gate：

- Gate 结果：`TBD（ALLOWED / BLOCKED / NOT APPLICABLE）`。
- Gate reasons：`TBD`。
- 阈值：Completion 最大下降 2%、Hidden Test 最大下降 1%、Regression 不增加、平均成本最大增加 10%、P95 延迟最大增加 15%。

人工结论只能选择一项：

- [ ] `APPROVED AS BASELINE`：接受为后续候选版本的真实对照基线，但不等同于 Production 发布批准。
- [ ] `REJECTED / RERUN REQUIRED`：证据或配置无效，必须使用新 Evidence 路径重新执行。
- [ ] `BLOCKED`：存在安全、隐私、成本或治理阻断，禁止进入阶段 4。

结论理由：`TBD`。

## 7. 人工签署

| 字段 | 签署值 |
|---|---|
| 审核人 | `TBD` |
| 角色 | `TBD` |
| UTC 时间 | `TBD` |
| 批准/拒绝的报告 commit | `TBD（完整 40 位 SHA）` |
| 签署方式或外部审批记录 | `TBD` |

未填写签署字段、保留 `TBD` 或同时选择多个结论时，本文件保持 `DRAFT / NOT APPROVED`，不得作为阶段 3 退出证据。
