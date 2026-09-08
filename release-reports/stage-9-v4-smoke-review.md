# 阶段 9 Developer v4 候选 smoke 审核

> 结论：`BLOCKED`。不得启动正式 180 Observation Eval，不批准 Promotion。

## 运行范围

- 执行日期：2026-09-09（Asia/Shanghai）。
- ForgeFlow：`ae0327f1300f3fa86fc4fb4bdb495a524499cc70`（PR #37 合并提交）。
- Fixture：`6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f`。
- Private Grader：`5942ec84d403e37385203b4c7851d1b92573548a`。
- 模型/Reasoning：`deepseek-v4-flash` / `low`。
- 范围：`feature-01`，`planner_developer`，`developer/v1` 与 `developer/v4` 各一次，共 2 个 Observation。
- 数据边界：只发送已授权 Fixture 的任务描述、仓库规则和最多 128 KiB 源码快照；未发送 Private Grader、隐藏测试源码、原始 Evidence 或凭据。
- 费用硬上限：`$0.10`；实际记录费用：`$0.008919152`。
- 定价：DeepSeek 官方非高峰价，cache hit / cache miss / output 分别为 `$0.007 / $0.22 / $0.66` 每百万 Token；来源为 `https://api-docs.deepseek.com/quick_start/pricing/`。

## 脱敏结果

| Prompt | 通过 | 失败阶段 | 脱敏错误 | 费用 USD | 延迟 |
|---|---:|---|---|---:|---:|
| `developer/v1` | 0/1 | `patch_check` | corrupt patch at line 39 | 0.005474764 | 54.995 秒 |
| `developer/v4` | 0/1 | `patch_check` | corrupt patch at line 21 | 0.003444388 | 32.664 秒 |

两侧均完成 JSON 解码和变更集检查，但没有通过补丁预检。显式测试和隐藏测试没有运行，因此报告中的零值不能解释为测试执行后的质量结果。smoke 固定为 `promotionEligible=false`。

## 诊断与修复

两个 Prompt 在不同响应中都产生 Git 判定为损坏的统一 diff，说明当前阻断不应只归因于 v4 的任务能力。执行器新增严格的安全规范化层：

1. 将 CRLF/CR 统一为 LF，并保证末尾换行。
2. 仅把 hunk 内零长度行恢复为合法的单空格上下文行；非空无前缀行继续拒绝。
3. 只根据已有的上下文、增加和删除行重算 hunk 数量，不修改 hunk 起始行、路径或任何非空内容。
4. Markdown fence、NUL、无 `diff --git`、无 hunk、无实际变更和非法 no-newline marker 继续拒绝。
5. 规范化后仍必须先通过 `git apply --check`；源上下文不匹配仍失败且不写入文件。

离线回归覆盖了可安全修复的空白上下文/错误计数、非空无前缀拒绝、源上下文不匹配拒绝、取消和进程启动错误分类。该修复尚未绑定到已合并 Git SHA，不能用本次失败 smoke 证明修复有效。

## 下一门禁

人工审核并合并本批修复 PR，四项必需 CI 全部成功后，从新的干净 `main` SHA 使用新 Campaign 再运行同范围 2 Observation smoke。只有新 smoke 排除补丁协议/基础设施错误，才允许讨论正式 Eval；不得复用本次价格有效期、Campaign 或原始 Evidence。

本报告只包含可公开的脱敏聚合数据。原始 Evidence、模型输出、补丁、Fixture 源码、Private Grader、隐藏测试和 Key 均未写入仓库。
