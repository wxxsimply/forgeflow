# ForgeFlow Developer Prompt v2 候选说明

> 状态：候选 Prompt 已由 PR #20 合并并通过四项必需检查；尚未完成候选 Eval、Promotion 或部署批准。

## 1. 候选目的

阶段 3 的脱敏汇总记录了 51 个 `model_output_invalid`，主要边界是严格 Schema 和补丁校验失败。该数字是三种模式的合计，不代表全部失败都由生产 Developer Prompt 导致，也不能据此预先声明 v2 会改善成绩。

`developer/v2` 在不扩大 Agent 权限和上下文范围的前提下增加输出前自检，重点约束：

- 六个顶层字段必须完整且不为 `null`。
- 只返回一个严格 JSON 对象，不带 Markdown 或尾随文本。
- unified diff 的文件头、hunk、行数和完整性。
- `changedFiles` 与补丁路径一一对应且不越过批准范围。
- JSON 字符串正确保留补丁中的换行、引号和反斜杠。
- Evidence 不得虚构测试或命令成功。

## 2. 不变边界

- `developer/v1` 文件保持原样并继续嵌入二进制。
- `FORGEFLOW_DEVELOPER_PROMPT_VERSION` 默认值仍为 `developer/v1`。
- 本候选不修改模型、Policy、Tool、Schema、权限、预算或发布数据库。
- 合并候选 PR 不等于 Promotion，也不会热替换运行中的 Run。

## 3. 自动验证

- Prompt Loader 同时加载 v1 和 v2，并验证两个 SHA-256 不同。
- 使用 `developer/v2` 构建治理 Catalog 时，`developer/v1` 仍可按版本和 SHA-256 解析为未配置的回滚目标。
- 全仓门禁必须在候选 PR 上重新通过。

PR #20 合并 commit 为 `3302aeb7aa3725761bf614695ba8f2415980df81`；Go verification、PostgreSQL integration、Web verification 和 deployment `validate` 均成功。

## 4. 合并后门禁

1. Eval CLI 必须通过 `--developer-prompt-version` 加载生产 Prompt，并把同一版本写入 Evidence；禁止只修改报告标签。
2. 使用合并后的精确 Git SHA 构建候选 Eval。
3. 以相同 Fixture、Private Grader、模型、Reasoning、Policy、Tool、预算和价格窗口运行当前版本与候选版本对照。
4. 原始 Evidence 留在私有路径，只提交人工脱敏报告。
5. 指标不完整、确定性失败或超过 Promotion Gate 时不得 Promotion。
6. 通过门禁后仍需 Admin 人工批准，再执行 drain、双版本镜像部署、Readiness 校验和 rollback 演练。

在上述步骤完成前，阶段 4 保持“进行中”，路线图中的双版本镜像和人工 Promotion/rollback 验收项不得勾选。
