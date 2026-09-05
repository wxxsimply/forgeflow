# ForgeFlow Developer Prompt v4 候选说明

> 状态：本地不可变候选，尚未提交、合并、执行付费 smoke、正式 Eval 或批准 Promotion。

## 1. 候选原因

`developer/v2` 已被正式 v1/v2 Eval 自动 Gate 阻断。`developer/v3` 随后进行了两次不可晋级 smoke：第一次暴露 45 秒调用超时和外层 JSON 围栏，PR #29 完成严格围栏兼容并把超时提高到 60 秒；第二次运行中 v1 返回普通文本前缀，v3 再次超时。两次 smoke 均未进入有效候选质量比较，因此不得启动正式 v1/v3 Eval。

DeepSeek 官方 JSON Output 指南要求 Prompt 包含 `json` 和期望 JSON 格式示例。v3 要求严格 JSON，但没有提供最小字段形状示例。已经产生运行证据的 v3 不得原地修改，因此创建新的 `developer/v4`。

## 2. v4 设计

v4 保留 v3 的权限、安全、作用域、证据和完整补丁边界，只补充紧凑的输出格式约束：

- 首个非空白字符必须是 `{`，最后一个必须是 `}`。
- 明确禁止 Markdown 围栏、介绍、道歉、拒绝说明和尾随文本。
- 明确列出六个必需顶层字段，并要求数组字段不能省略。
- 提供只展示字段形状的最小 JSON 示例，禁止复制占位值。
- 补丁仍须完整、可应用，并与 `changedFiles` 完全一致。

候选 Prompt 内容 SHA-256 为 `16fad8192c43891b8c7e10a7b22a936cd641c288db18144e10523e8a01336d81`；摘要按 `system.txt` 字节后直接拼接 `user.tmpl` 字节计算，与生产 `PromptLoader` 一致。

JSON Schema、权限、工具、预算、模型、Reasoning、生产默认版本和上下文范围均不改变。生产默认仍为 `developer/v1`。

## 3. 不可变与回滚边界

- `developer/v1`、`developer/v2`、`developer/v3` 文件保持原样并继续嵌入二进制。
- v2 保留为正式 Gate 阻断的历史候选；v3 保留两次 smoke 未通过的历史候选。
- v4 使用独立目录和独立 SHA-256；合并候选不等于 Promotion。
- 严格解码器只容忍单一、无额外文本的 `json` 外层围栏，解析后仍执行完整 Schema 和领域校验。

## 4. 合并后门禁

1. 人工审核并合并 v4 PR，确认四项必需检查全部成功。
2. 从包含 v4 的干净合并 SHA 运行 `-CandidatePromptVersion developer/v4 -SmokeOnly -SmokeCaseLimit 1` 预检。
3. 在有效价格窗口内运行 2 Observation 付费 smoke；只读取脱敏聚合结果。
4. smoke 存在超时、结构错误或确定性失败时停止，不启动正式 Eval。
5. 只有 smoke 排除基础设施错误且候选结果可接受，才运行完整 v1/v4 对照。
6. 正式自动 Gate 通过后仍须由 Admin 人工批准，才可进入 Promotion/rollback 演练。
