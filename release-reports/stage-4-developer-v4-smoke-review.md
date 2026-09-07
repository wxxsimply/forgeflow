# Developer v4 首次 smoke 审核

> 结论：NOT READY FOR FORMAL EVAL。v4 保持不可变，当前不批准 Promotion。

## 运行记录

- 执行日期：2026-09-05；读取、审核日期：2026-09-07。
- ForgeFlow：`108c57ff0740e4c4fccfa27522297177602b0476`（PR #30）。
- Fixture：`6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f`。
- Grader：`5942ec84d403e37385203b4c7851d1b92573548a`。
- 模型/Reasoning：`deepseek-v4-flash` / `low`。
- 范围：同一首个 Fixture，各运行一次 `planner_developer`，共 2 个结果。
- 单次模型调用上限：60 秒。
- 记录费用：`$0.00640506`，两侧均返回了用量；2026-09-07 未新增付费调用。
- 价格依据：[DeepSeek 官方价格页](https://api-docs.deepseek.com/quick_start/pricing/)，执行当时 cache miss / cache hit / output 分别为 `$0.22 / $0.007 / $0.66` 每百万 Token；有效窗口为 `2026-09-04T10:00:00Z` 至 `2026-09-07T01:00:00Z`。该窗口已过期，不可复制用于新调用。

## 结果

| Prompt | 通过数 | Case 耗时 | 记录费用 USD | 失败位置 |
|---|---:|---:|---:|---|
| developer/v1 | 0/1 | 20.660 秒 | 0.002293852 | Git 补丁预检：源文件上下文不匹配 |
| developer/v4 | 0/1 | 43.363 秒 | 0.004111208 | Git 补丁预检：补丁格式损坏 |

两侧 Case 耗时之和为 64.023 秒，不等同于包含编译、启动和报告生成的完整墙钟耗时。两侧均完成两次模型调用、通过 Developer JSON 解码及变更集校验；补丁预检阻断了后续应用。显式测试和隐藏测试没有执行，报告中的零值不能解读为已经执行测试后未通过。

现有终态消息不能确定格式损坏的具体来源，例如 hunk 计数、行截断或转义；不得仅凭此结果断言某种修复有效，也不能推断 v4 比 v1 更好。

## 已完成的本地工作与下一步

1. 为后续 Evidence 添加可选 `failureStage`，区分补丁预检和实际应用失败；保留原有错误代码及严格 Git 检查。
2. 修复 Git 命令超时/取消原因丢失，以及进程启动错误被误记为模型输出错误的问题。
3. smoke 汇总加入固定失败分类；不输出 Provider 原文、补丁或隐藏测试名称。
4. 使用独立临时仓库中的合成补丁测试合法应用、损坏格式、源内容不匹配和上下文到期，确认拒绝的补丁不会修改源文件。
5. 人工提交并合并本次诊断代码；下一轮先根据补丁诊断定位生成问题，使用新 Campaign 和干净 SHA 获取证据。尚无证据支持直接创建 v5 或运行完整正式对照。

此文件仅包含脱敏审核数据。原始 Evidence、源码、补丁、私有 Grader、隐藏测试和凭据均不包含在内。smoke 的 `promotionEligible=false` 保持有效。

## 本地验证及人工提交

2026-09-07：`scripts/verify.ps1` 通过，包含 Go 测试、Vet、可用时的 Staticcheck、二进制构建和 Web 检查；Web 11 项测试通过。PowerShell 语法检查和旧 Evidence 离线评分通过。`developer/v1` 至 `developer/v4` 的 Prompt 文件没有修改。上述验证未新增模型调用。

当前分支：`codex/stage-4-patch-diagnostics`。下列提交、推送和创建 PR 均由仓库所有者人工执行；先审核改动，尤其确认本报告只含可发布的脱敏数据。

```powershell
git add -- FORGEFLOW_POST_IMPLEMENTATION_ROADMAP.md README.md docs/completion-audit.md docs/stage-4-developer-prompt-v4-candidate.md docs/stage-4-developer-v2-eval-runbook.md docs/stage-4-prompt-model-governance-audit.md internal/eval/eval_test.go internal/eval/grader.go internal/eval/report.go internal/evalexec/protocol.go internal/evalexec/workspace_test.go scripts/stage-4-developer-prompt-eval.ps1 release-reports/stage-4-developer-v4-smoke-review.md
git diff --cached --check
git diff --cached --stat
git commit -m "fix: add eval patch diagnostics and defer long validation"
git push -u origin codex/stage-4-patch-diagnostics
gh pr create --base main --head codex/stage-4-patch-diagnostics --title "fix: add eval patch diagnostics and defer long validation" --body "Record patch precheck and apply failure stages, preserve cancellation and process errors, and add safe smoke failure counts. Reorganize the remaining roadmap so quick engineering checks run first and full Eval, image, Staging, security, recovery, and load validation run in the final gate. Includes synthetic patch regression tests and the v4 smoke review. Local verification passed."
```

四项必需 CI 成功后，在 GitHub 网页人工合并。该 PR 完成的是诊断能力和状态记录，不能作为 v4 补丁生成问题已经修复的证明。
