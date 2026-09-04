# ForgeFlow Developer Prompt v3 候选说明

> 状态：本地候选，尚未提交、合并、执行正式 Eval 或批准 Promotion。

## 1. 候选原因

`developer/v2` 已在 ForgeFlow `63a311779bd20109a0e640367e9898d2e22cb683` 上完成 v1/v2 正式对照。两侧均覆盖三种模式各 30 个 Fixture，共 180 个终态 Observation，共享实测成本 `$0.754819756`。自动 Promotion Gate 返回 `false`，三种模式的候选通过数分别由 `14/30`、`13/30`、`9/30` 降至 `12/30`、`7/30`、`6/30`。

报告显示 v2 新增的长篇逐项输出自检没有改善严格 Schema 可靠性，且多个原本通过的 Case 出现不可应用补丁。v2 已形成正式 Evidence，因此按照不可变版本治理不得原地修改。

## 2. v3 设计

`developer/v3` 保留 v1 的权限、安全、作用域和证据边界，只增加一段紧凑的输出约束：

候选 Prompt 内容 SHA-256 为 `55af46bb2f3a2ff92ce62c8bcf7e37ad07a4131ffda0ea8ef32e18b0d5a829db`；该摘要按 `system.txt` 字节后直接拼接 `user.tmpl` 字节计算，与生产 `PromptLoader` 一致。

- 返回 Schema 要求的唯一严格 JSON 对象，不输出 Markdown 或尾随文本。
- 补丁必须针对所提供的精确文件内容，完整且可应用。
- 使用仓库相对 `a/`、`b/` 路径和准确 hunk 范围。
- `changedFiles` 与补丁路径完全一致。
- 不截断补丁，不把解释文本写进 diff。

JSON Schema、权限、工具、预算、模型、Reasoning、默认生产版本和上下文范围均不改变。生产默认仍为 `developer/v1`。

## 3. 不可变与回滚边界

- `developer/v1`、`developer/v2` 文件保持原样并继续嵌入二进制。
- `developer/v2` 保留为自动 Gate 阻断的历史候选。
- `developer/v3` 使用独立目录和 SHA-256；合并候选不等于 Promotion。
- Eval 脚本通过 `-CandidatePromptVersion` 选择候选，并拒绝把当前 `developer/v1` 当作候选。

## 4. 合并后门禁

1. 从包含 v3、参数化脚本和本说明的干净合并 SHA 运行预检。
2. 用 `-CandidatePromptVersion developer/v3` 在同一价格窗口、共享预算和固定 Fixture/Grader SHA 下运行完整 v1/v3 对照。
3. 只提交人工脱敏聚合审核记录，不提交原始 Evidence。
4. 自动 Gate 未通过时继续保留 v1，不得 Promotion。
5. 自动 Gate 通过后仍须由 Admin 人工批准，才可进入 drain、双版本镜像、Readiness、Promotion 和 rollback 演练。
