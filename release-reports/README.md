# ForgeFlow Release Reports

本目录只保存经过人工复核、可以进入 Git/GitHub 的脱敏发布报告和签署记录。

禁止提交：

- `.forgeflow/evals` 中的原始 Evidence；
- Fixture 任务正文、仓库规则、源码快照或模型原始输出；
- Private Grader、隐藏测试源码或逐项隐藏测试细节；
- API Key、数据库凭据、Webhook、内部路径或其他 Secret；
- 未完成的指标、推测值或把结构验证冒充真实模型成绩的内容。

正式报告必须能追溯到精确 Git、Fixture、Grader、模型、Reasoning、Prompt、Policy、Tool 和价格窗口，并明确包含全部失败、拒绝、超时及人工介入样本。原始 Evidence 保持在受控私有存储中，不通过本目录发布。

阶段 3 完整三基线完成后，复制 `stage-3-eval-review-template.md` 为带日期或批准版本的文件，填入脱敏指标并由仓库所有者人工签署。模板本身不代表批准。

阶段 9 使用 `stage-9-acceptance-summary-template.md` 汇总可公开的最终验收结果。填充后的私有验收计划、逐步 Evidence 和内部审批记录必须保留在 `.forgeflow/acceptance` 或批准的私有不可变存储中；模板、空白字段或部分通过均不代表 `GO`。
