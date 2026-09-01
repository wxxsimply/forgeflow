# ForgeFlow 阶段 3 Eval 人工审核与 Promotion 结论

> 状态：`DRAFT / NOT APPROVED`
> 说明：只有三种模式各 30 个终态 Observation、完整指标和人工复核全部完成后，才能复制并签署本模板。`TBD` 不得替换为估算或虚构数据。

## 1. 审核范围与可追溯性

| 字段 | 审核值 |
|---|---|
| Dataset / version | `software/v1` / `2026-08-30` |
| 主仓库 Git commit | `e18bfa9c5e73435634644c7c44c629d7cca07dab` |
| Fixture repository commit | `6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f` |
| Private Grader commit | `5942ec84d403e37385203b4c7851d1b92573548a` |
| Provider / model | `deepseek` / `deepseek-v4-flash` |
| Reasoning effort | `low` |
| Prompt versions | `eval/single-agent/v1`、`eval/planner/v1`、`eval/developer/v1`、`eval/reviewer/v1`、`eval/security/v1`、`eval/judge/v1` |
| Policy / Tool versions | `eval-policy/v1` / `eval-tools/v1` |
| Execution environment | `windows/amd64 go1.26.6` |
| Pricing mode / source | `cache_hit_miss` / <https://api-docs.deepseek.com/quick_start/pricing/> |
| 连续价格窗口 | Observation `2026-09-01T10:03:37Z` 至报告 `2026-09-01T11:33:30Z`；价格有效截止 `2026-09-02T01:00:00Z` |
| 私有 Evidence SHA-256 | `9674C3A46E2226672BB8D29B97E23F181A0E899E7384E3A84673F7766DF8DDDC`；只记录摘要，不提交 Evidence |
| JSON / Markdown 报告 SHA-256 | JSON `F58D631F9E3D89A163921013C041B8C5445F8974D979EB837A8A5D30959E7FD3`；Markdown `50224CC2F139ABC175C6E55002CA949A50439211EE96FB26641767101605494E` |

## 2. Evidence 完整性门禁

- [x] `single_agent`、`planner_developer`、`forgeflow` 各有 30 个唯一 Case，共 90 个终态 Observation。
- [x] 配置、Dataset 和 Case ID 与锁定版本一致，没有跨价格窗口或跨 Git commit 拼接。
- [x] 失败、拒绝、超时、审批请求和人工介入全部保留，没有删除后重跑成成功。
- [x] 每个 Observation 都有真实 Token、成本和延迟；缺失指标没有被估算值填充。
- [x] 显式测试来自实际退出码，Private Grader 在 Agent 工作区外运行。
- [x] Secret 与危险命令结果已复核；两项结果均为 0。
- [x] 原始 Evidence、Fixture 内容、Private Grader 和隐藏测试没有进入当前 Git 候选修改；原始 Evidence 和私有报告均由 `.gitignore` 排除。

任一项未勾选时，结论必须为 `REJECTED / INCOMPLETE`。

## 3. 三基线脱敏指标

| Mode | 完成数 | Completion | Hidden tests | Regression | Human intervention | Avg cost USD | P95 latency ms |
|---|---:|---:|---:|---:|---:|---:|---:|
| `single_agent` | `8/30` | `26.67%` | `30.00%` | `16.67%` | `10.00%` | `$0.0022` | `79816` |
| `planner_developer` | `11/30` | `36.67%` | `36.67%` | `6.67%` | `10.00%` | `$0.0046` | `128361` |
| `forgeflow` | `7/30` | `23.33%` | `23.33%` | `10.00%` | `10.00%` | `$0.0050` | `130700` |

## 4. 失败与安全样本核对

| 类别 | 数量 | 脱敏说明 / 审核结论 |
|---|---:|---|
| `model_output_invalid` | `51` | 严格 Schema/补丁校验失败，全部保留，未删除后重跑。 |
| 显式测试失败 | `4` | 来自真实验证命令退出码。 |
| 隐藏测试失败 | `6` | 来自 Agent 工作区外的 Private Grader。 |
| 审批 / 人工介入 | `9` | 3 个终态为 `approval_required`；Clarify/审批等人工介入均进入指标。 |
| Secret 检测 | `0` | 无阻断样本。 |
| 危险命令执行 | `0` | 危险类 Case 均未执行危险命令。 |
| 超时 / Provider / 基础设施失败 | `3` | 3 个 `internal_error` 原样保留；没有从统计中剔除。 |

## 5. 成本、隐私和发布边界

- 授权费用上限：`10 CNY`；代码硬上限 `$1.40`，并通过 `--prior-cost-usd 0.045562336` 纳入此前诊断。
- 实际 Provider 费用：正式 Evidence `$0.354427016`；授权周期合计 `$0.399989352`。按 2026-09-01 中国外汇交易中心中间价 1 USD = 6.7809 CNY 换算约 `2.7123 CNY`；来源：<https://www.safe.gov.cn/AppStructured/hlw/RMBQuery.do>，执行前后均使用当日公布值。
- [x] 实际费用未超过人工授权上限。
- [x] Provider 只收到批准的任务描述、仓库规则和单 Fixture 最多 128 KiB 源码快照；Private Grader、隐藏测试、Evidence 和凭据未出站。
- [x] 发布版只包含汇总指标、版本和结论，不包含可还原私有任务或源码的信息。
- [x] 当前候选报告中的 URL、路径、身份和错误信息已按发布边界脱敏。

## 6. 自动门禁与人工结论

自动 Promotion Gate：

- Gate 结果：`NOT APPLICABLE`。
- Gate reasons：这是首个完整真实三基线报告，尚无已批准的前序基线可做候选 Promotion 比较；本报告拟作为后续候选的初始对照。
- 阈值：Completion 最大下降 2%、Hidden Test 最大下降 1%、Regression 不增加、平均成本最大增加 10%、P95 延迟最大增加 15%。

人工结论只能选择一项：

- [ ] `APPROVED AS BASELINE`：接受为后续候选版本的真实对照基线，但不等同于 Production 发布批准。
- [ ] `REJECTED / RERUN REQUIRED`：证据或配置无效，必须使用新 Evidence 路径重新执行。
- [ ] `BLOCKED`：存在安全、隐私、成本或治理阻断，禁止进入阶段 4。

结论理由：技术 Evidence 和脱敏报告已完整，但完整 ForgeFlow 通过率 `7/30`，低于 `planner_developer` 的 `11/30` 和 `single_agent` 的 `8/30`。是否接受其作为真实初始对照基线，必须由仓库所有者人工复核并签署；在此之前保持 `DRAFT / NOT APPROVED`。

## 7. 人工签署

| 字段 | 签署值 |
|---|---|
| 审核人 | `TBD` |
| 角色 | `TBD` |
| UTC 时间 | `TBD` |
| 批准/拒绝的报告 commit | `TBD（完整 40 位 SHA）` |
| 签署方式或外部审批记录 | `TBD` |

未填写签署字段、保留 `TBD` 或同时选择多个结论时，本文件保持 `DRAFT / NOT APPROVED`，不得作为阶段 3 退出证据。
