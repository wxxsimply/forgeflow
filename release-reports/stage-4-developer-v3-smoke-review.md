# Stage 4 Developer v3 快速 Smoke 审核

> 结论：`NOT READY FOR FORMAL EVAL`。两次 v1/v3 smoke 均未形成有效质量比较；v3 保留为不可变历史候选。本记录只包含脱敏聚合信息，不是 Promotion Evidence。

## 运行身份

- UTC 日期：2026-09-04
- ForgeFlow commit：`8aa1675ae0320e8726b7204e256d1e294ae495c3`
- Fixture commit：`6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f`
- Private Grader commit：`5942ec84d403e37385203b4c7851d1b92573548a`
- 模型：`deepseek-v4-flash`
- Reasoning：`low`
- 模式：`planner_developer`
- Case：每个 Prompt 1 个，共 2 个 Observation
- 价格：cache miss `$0.22/M`、cache hit `$0.007/M`、output `$0.66/M`
- 价格来源：DeepSeek 官方 Models & Pricing
- 实测总成本：`$0.006510816`

原始 Evidence、任务正文、源码快照、模型原始输出、Private Grader 和隐藏测试均未写入本报告，也不得提交 GitHub。

## 聚合结果

| Prompt | Passed | Completion | Cost USD | P95 latency | 终态 |
|---|---:|---:|---:|---:|---|
| `developer/v1` | 0/1 | 0% | 0.003239568 | 86,254 ms | `timeout` |
| `developer/v3` | 0/1 | 0% | 0.003271248 | 32,695 ms | `model_output_invalid` |

v1 在原 45 秒单次模型调用限制下超时，可能属于 smoke 配置假阴性。v3 返回了只有外层 Markdown JSON 围栏的结构化对象，严格解码器在读取首个反引号时拒绝；没有进入补丁应用或隐藏测试阶段。

## 处置

1. 不启动 v1/v3 正式 180 Observation Eval，不执行 Promotion。
2. 将 smoke 模型调用超时提高到 60 秒，默认 Case 数仍为每个 Prompt 1 个。
3. 解码器仅兼容恰好一个、无前后文本的 `json` 围栏；剥离后继续执行完整严格 JSON、必需字段、未知字段、路径和领域校验。
4. 修复通过 PR CI 并合并后，从新的干净合并 SHA 使用新 Campaign ID 重新运行 2 Observation smoke。
5. 只有重跑没有基础设施错误且候选结果可接受，才进入正式 v1/v3 对照。

本次 smoke schema 为 `forgeflow.eval.smoke-campaign/v1`，`promotionEligible=false`；它不能作为 Promotion 输入。

## 修复后重跑

PR #29 将单次模型调用上限提高到 60 秒，并加入严格外层 JSON 围栏兼容。重跑固定在 ForgeFlow `698520cea5c0ca441f3b9d1eb628db7c411147cf`，Fixture、Private Grader、模型、Reasoning 和价格保持一致。

| Prompt | Passed | Completion | Cost USD | P95 latency | 终态 |
|---|---:|---:|---:|---:|---|
| `developer/v1` | 0/1 | 0% | 0.003360192 | 36,035 ms | `model_output_invalid` |
| `developer/v3` | 0/1 | 0% | 0.002933988 | 96,120 ms | `timeout` |

重跑成本为 `$0.00629418`，两次 smoke 合计 `$0.012804996`。v1 返回了普通文本前缀，不能在不放松严格协议的前提下自动截取；v3 在 60 秒限制下超时。两侧均未进入有效补丁和隐藏测试比较。

最终处置：不运行正式 v1/v3 Eval，不 Promotion v3。创建独立 `developer/v4`，按 DeepSeek 官方 JSON Output 建议增加最小 JSON 形状示例和首尾字符约束，再从其干净合并 SHA 重新执行 2 Observation smoke。
