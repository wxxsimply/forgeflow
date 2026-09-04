# ForgeFlow Developer Prompt v2 候选说明

> 状态：正式 v1/v2 对照已于 2026-09-03 UTC 完成，自动 Promotion Gate 返回 `false`；`developer/v2` 已阻断且不得 Promotion，保留为不可变历史候选。

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

PR #21 合并 commit 为 `76ede9b4e875adf7e9494d7c0c38eb5f767b8de6`；Eval 执行器现在真实加载 `--developer-prompt-version` 指定的生产 Prompt，使用生产六字段响应契约，并在调用 Provider 前核对 Evidence 配置中的 Prompt version。四项必需检查均成功。

## 4. 合并后门禁

1. [x] Eval CLI 通过 `--developer-prompt-version` 加载生产 Prompt，并把同一版本写入 Evidence；不会只修改报告标签。
2. [x] 使用合并后的精确 Git SHA `63a311779bd20109a0e640367e9898d2e22cb683` 构建候选 Eval。
3. [x] 以相同 Fixture、Private Grader、模型、Reasoning、Policy、Tool、预算和价格窗口运行当前版本与候选版本对照。
4. [x] 原始 Evidence 留在私有路径，只生成脱敏聚合审核记录。
5. [x] 自动 Gate 因完成率、隐藏测试、回归率和确定性失败阻断 v2，未执行 Promotion。
6. [ ] 仓库所有者在 GitHub PR 中签署 `REJECTED / RERUN REQUIRED`。

详细结果见 `release-reports/stage-4-developer-v2-review.md`。阶段 4 保持“进行中”；v2 不进入双版本镜像和 Promotion/rollback，后续改进使用新的不可变 `developer/v3`。

双运行的参数、预算和恢复步骤见 `docs/stage-4-developer-v2-eval-runbook.md`。受控脚本必须先经 PR 合并；正式 Evidence 必须记录该合并后的精确 SHA，不能在有未提交修改的工作区运行。
