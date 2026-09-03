# ForgeFlow 阶段 3：三基线执行器审计

> 日期：2026-08-31～2026-09-01
> 状态：已完成；正式三基线 90-Observation Evidence 与私有报告已完成技术审计，仓库所有者已批准为后续候选版本的真实对照基线
> 数据集：`software/v1`

## 已完成的本地实现

- `single_agent`：单次受控模型调用同时作出决策并生成补丁。
- `planner_developer`：Planner 决策和计划后，由 Developer 生成补丁，不启用完整复核链。
- `forgeflow`：Planner、Developer、显式测试、Reviewer、Security、确定性终态与有界 Repair。
- 三种模式共享同一模型、Reasoning、价格、超时、Fixture 和执行主机配置，不复用消息历史或工作区。
- 每个 Case 从数据集绑定的精确 Fixture SHA 创建全新 detached Git worktree，完成后受管清理。
- Agent 只能输出结构化决策或统一 Diff；补丁应用前检查禁止路径、文件数和 Diff 行数预算。
- 显式测试只允许数据集固定的 `go`/`npm` 命令，结论取实际进程退出码；测试环境不继承 `OPENAI_API_KEY`。
- 隐藏测试由 Agent 工作区外的 Private Grader 执行，Agent 不读取 Grader 源码或隐藏测试结果。
- Observation 支持 `cache_hit_miss`（DeepSeek）和 `cache_read_write`（OpenAI）两种官方计费语义，分开记录普通输入、缓存读取、可选缓存写入、输出和推理 Token，并按各自费率计算真实成本。
- Evidence 记录 Provider、计费模式、官方价格来源、价格生效/截止时间和各项费率；窗口尚未开始或有效期不足以覆盖下一次调用时会在发出付费请求前停止。
- 拒绝、超时、审批请求、模型输出错误和测试失败都形成 Observation，不从统计中删除。
- Evidence 在每个 Case 后通过同目录临时文件、Sync 和原子 Rename 更新；同配置重跑会跳过已记录 Case。
- 恢复时若 Git、模型、Reasoning、Prompt、Policy、Tool、Fixture 或 Grader 配置变化，会要求使用新的 Evidence 路径。
- Evidence 不写任务正文、Prompt、模型原始输出或工作区路径；失败消息会脱敏路径、Key 和凭据模式。

## 强制前置门禁

`eval execute` 在付费调用前要求：

1. 主仓库工作区干净，并记录精确 40 位 Git SHA。
2. Private Grader 工作区干净，并记录精确 Git SHA。
3. 30 个 Fixture SHA 全部可解析，并记录 Fixture HEAD。
4. OpenAI-compatible Provider Key 只通过兼容变量 `OPENAI_API_KEY` 从当前进程环境注入；变量名不代表必须使用 OpenAI。
5. 显式选择 Provider 和计费模式，并提供当前真实价格、官方 HTTPS 来源及 RFC3339 生效/截止时间。
6. `cache_hit_miss` 只接受普通输入、缓存命中和输出价格；`cache_read_write` 还必须提供缓存写入价格。
7. 三种模式和输出路径合法；原始 Evidence 位于 Git 忽略的 `.forgeflow/evals`。

## 已通过的本地验证

- Eval 数据集、报告和 Promotion 既有测试。
- Evidence 原子写入、单 Case 恢复和配置漂移拒绝测试。
- 禁止路径、Diff 预算、Secret 检测和失败消息脱敏测试。
- 临时真实 Git 仓库中的 Patch apply 与 `go test` 退出码集成测试。
- 本地 Private Grader 自身测试和 30 Case Fixture commit 预检。
- 全仓库 Go 测试、格式、Vet 和项目验证脚本（最终结果以提交前命令输出为准）。
- 专用 `release-reports` 脱敏发布边界和阶段 3 人工审核/Promotion 签署模板；模板保持 `DRAFT / NOT APPROVED`，不预填虚构成绩。

## 2026-08-31 非付费预检快照

- 主仓库 commit：`865e9d726eda914b2979b07a79aef336726203a1`（PR #6 已合并；本次审计分支从该 commit 创建）。
- Fixture 仓库 HEAD：`6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f`，工作区干净。
- Private Grader HEAD：`5942ec84d403e37385203b4c7851d1b92573548a`，工作区干净。
- `software/v1` 共 30 个 Case，30 个 Fixture commit 全部通过 `--validate-only` 验证。
- Private Grader `go test ./...` 通过。
- 当前终端未配置 Provider Key，因此没有发起模型请求，也没有产生费用或真实模型成绩。
- 任何曾出现在聊天、日志或命令历史中的 Key 都必须先在 Provider 控制台撤销；新 Key 只能从本地安全存储注入，不得写入本审计记录。

## 真实执行前的人工门禁

真实 Evidence 必须追溯到包含执行器和计费门禁的精确 Git SHA。仓库所有者需要先人工审核并提交本次预检记录；随后确认旧 Key 已撤销、新 Key 已在本地安全注入，并明确批准一次付费 smoke。没有这些人工条件时不得发起模型调用，也不得进入阶段 4。

## 提交后的真实执行

下面是 DeepSeek Responses API 的执行模板。先撤销任何曾出现在聊天、日志或命令历史中的 Key，再把新 Key 仅注入当前 PowerShell 进程；不要写入仓库中的 `.env`：

```powershell
$secureKey = Read-Host "DeepSeek API Key" -AsSecureString
$keyPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureKey)
try { $env:OPENAI_API_KEY = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($keyPointer) }
finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($keyPointer) }
$env:FORGEFLOW_OPENAI_BASE_URL = "https://api.deepseek.com"

go run ./cmd/forgeflow eval execute `
  --suite software/v1 `
  --fixture-repository D:\fixtures\forgeflow-eval-fixtures `
  --grader-repository D:\fixtures\forgeflow-eval-grader `
  --modes single_agent,planner_developer,forgeflow `
  --provider deepseek `
  --model deepseek-v4-flash `
  --pricing-mode cache_hit_miss `
  --pricing-source https://api-docs.deepseek.com/quick_start/pricing/ `
  --pricing-valid-from <当前峰谷价格窗口开始时间，RFC3339> `
  --pricing-valid-until <当前峰谷价格窗口结束时间，RFC3339> `
  --input-usd-per-million <当前缓存未命中价格> `
  --cached-input-usd-per-million <当前缓存命中价格> `
  --output-usd-per-million <当前输出价格> `
  --max-total-cost-usd <将本轮人民币授权按执行时汇率换算后的USD硬上限> `
  --prior-cost-usd 0.045562336 `
  --output .forgeflow\evals\evidence.json
```

DeepSeek 高峰与非高峰价格不同。完整三基线必须在同一个官方价格窗口内运行；优先选择周末或足够长的非高峰窗口。执行前记录人民币兑美元汇率的来源、查询时间和保守换算结果，`--max-total-cost-usd` 不得高于 10 元人民币对应的美元额度；峰值诊断的 `$0.045562336` 通过 `--prior-cost-usd` 计入同一授权。可先增加 `--limit 1` 做一次付费 smoke，确认结构化输出、Token 和成本正确后去掉该参数，原命令会从下一个 Case 继续。如果价格窗口到期，必须停止并使用新的 Evidence 路径，不能用新价格续写旧配置。

费用门禁在每次 Provider 调用前按“请求 UTF-8 字节数 + 消息封装余量 + `max-output-tokens`”计算保守 Token 上限，并使用当前计费模式下最高的输入费率预留额度。调用完成后预留额替换为 Provider 返回 Token 的实测费用；三种模式共享同一个并发安全预算。断点恢复会重新汇总 Evidence 内所有 Observation 的费用。若下一次调用的保守最大费用超过剩余额度，命令以 `budget_exhausted` 停止且不会调用 Provider；已有 Evidence 保持完整。

完成 90 个 Observation 后生成私有 JSON 与 Markdown 对比报告：

```powershell
go run ./cmd/forgeflow eval --suite software/v1 --evidence .forgeflow\evals\evidence.json --format json --output .forgeflow\evals\comparison.json
go run ./cmd/forgeflow eval --suite software/v1 --evidence .forgeflow\evals\evidence.json --format markdown --output .forgeflow\evals\comparison.md
```

## 2026-08-31 DeepSeek 付费 smoke 结果

- 人工授权范围：仅 `software/v1` 第一个 Fixture、`single_agent` 模式、一次模型调用；允许发送任务描述、仓库规则和最多 128 KiB 源码快照。
- 明确禁止发送：Private Grader、隐藏测试源码、其他 Fixture 和原始 Evidence；本次执行遵守该边界。
- 主仓库 commit：`400411b10c94d071c3cd7c43d4f73505c9a6719e`。
- Fixture HEAD：`6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f`；Grader HEAD：`5942ec84d403e37385203b4c7851d1b92573548a`。
- Provider/模型：`deepseek` / `deepseek-v4-flash`；Prompt：`eval/single-agent/v1`；Policy：`eval-policy/v1`；Tool：`eval-tools/v1`。
- 计费模式：`cache_hit_miss`，采用执行时官方非高峰价格；价格来源：<https://api-docs.deepseek.com/quick_start/pricing/>。
- 结果：命令成功完成并原子记录 1 个终态 Observation，但 Case 结果为 `failed/internal_error`，脱敏原因为 Provider 返回了推理 Token 而没有最终输出文本。
- 计量：1 次模型请求，输入 2401 Token、缓存命中 0、输出 1124 Token、推理 1124 Token；成本 `$0.00127006`，延迟 7942 ms。
- 安全结果：未检测到 Secret 或危险命令；未生成补丁、未运行第二个 Case、未自动重试。
- DeepSeek 官方说明 JSON Output 偶尔可能返回空内容：<https://api-docs.deepseek.com/guides/json_mode/>。本次失败保留在统计中，不伪装为模型成功成绩。
- 原始 Evidence 位于 Git 忽略的 `.forgeflow/evals`，不得提交 GitHub；本节只记录脱敏汇总。

该 smoke 证明真实 Provider 连通、价格窗口门禁、Token/成本/延迟计量、失败终态和 Evidence 原子落盘可工作，但没有证明该 Case 或基线通过。再次调用或扩大到其余 89 个 Observation 前，必须重新取得明确的付费和数据出站授权。

## 2026-08-31 限额诊断结果

- 后续人工授权：额外最多 3 次 DeepSeek 付费诊断，总费用上限 `$0.01`；仍只允许发送 `feature-01` 已授权的数据范围。
- 诊断 1 使用 `reasoning-effort=none`：模型返回实施补丁，但补丁在第 33 行损坏，Observation 为 `failed/model_output_invalid`；输入 2322 Token、输出 373 Token、推理 0 Token，成本 `$0.00075702`，延迟 3689 ms。
- 诊断 2 使用 `reasoning-effort=low`：模型补丁可应用，修改 1 个文件，构建、显式测试和 1/1 隐藏测试均通过，Observation 为 `completed`；输入 2322 Token（其中缓存命中 2304）、输出 1428 Token、推理 1108 Token，成本 `$0.000962568`，延迟 23267 ms。
- 因第 2 次诊断已成功定位可用配置，没有执行授权额度中的第 3 次调用。两次额外诊断总成本 `$0.001719588`，低于 `$0.01` 上限；连同最初 smoke 的累计成本为 `$0.002989648`。
- 三次运行均未检测到 Secret 或危险命令；没有发送其他 Fixture、Private Grader、隐藏测试源码或原始 Evidence。
- 结论：当前 `deepseek-v4-flash` 在该 Case 上使用 `low` 可完成闭环；`medium` 遇到无最终文本，`none` 生成损坏补丁。这是单 Case 诊断结论，不代表完整基线成绩。
- 诊断同时发现 Evidence 配置此前没有记录 Reasoning。当前分支已增加 `reasoningEffort` 配置字段和断点续跑漂移测试；正式基线必须使用包含该修复的新 Git commit 和全新 Evidence 路径。
- 原始诊断 Evidence 继续保存在 Git 忽略的 `.forgeflow/evals`，不得提交 GitHub。

本次限额诊断授权已完成且未用尽次数或费用上限；剩余诊断额度没有自动延续。后续完整三基线已取得单独的费用和数据出站授权，范围见下一节。

## 2026-09-01 三基线价格窗口试运行

- 仓库所有者已明确授权将 `software/v1` 全部 30 个私有 Fixture 的任务描述、仓库规则和每个最多 128 KiB 源码快照发送给 DeepSeek，用于三基线正式 Eval；明确禁止发送 Private Grader、隐藏测试源码、原始 Evidence 或凭据。
- 主仓库 commit：`6d989ea394a6c3961ee353e2eaba593f160b1df5`（PR #8 已合并）；Fixture HEAD：`6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f`；Grader HEAD：`5942ec84d403e37385203b4c7851d1b92573548a`。三个工作区在执行前均通过洁净性和 30 Case 预检。
- 固定配置：`deepseek/deepseek-v4-flash`、`reasoning-effort=low`、`cache_hit_miss`、非高峰缓存命中 `$0.007/M`、未命中 `$0.22/M`、输出 `$0.66/M`；价格来源：<https://api-docs.deepseek.com/quick_start/pricing/>。
- `single_agent` 完成 30/30：8 个无失败码的 `completed`，4 个 `completed/hidden_grader_failed`，1 个 `failed/explicit_tests_failed`，16 个 `failed/model_output_invalid`，1 个 `approval_required`；30 次模型请求，成本 `$0.084260068`，P95 延迟 64955 ms。
- `planner_developer` 完成 15/30：4 个 `completed`，11 个 `failed/model_output_invalid`；30 次模型请求，成本 `$0.061973780`，当前样本 P95 延迟 89679 ms。
- `forgeflow` 尚未开始。合计保存 45 个终态 Observation、60 次模型请求、输入 141387 Token（缓存命中 2304）、输出 175181 Token、推理 159805 Token；累计成本 `$0.146233848`，整体 P95 延迟 64955 ms。
- 45 个 Observation 的 Secret 检测和危险命令执行计数均为 0；Private Grader 与隐藏测试始终在 Agent 工作区外运行，原始 Evidence 始终位于 Git 忽略的 `.forgeflow/evals`。
- 用户主动暂停后付费进程已终止；恢复时原价格有效期已经结束。根据配置漂移门禁，本次 45-Observation Evidence 保留为私有试运行证据，不与后续价格窗口混写，也不计作完整三基线报告。

完整 90 次正式基线必须在新的、足够长的连续官方价格窗口中使用全新 Evidence 路径从头执行。不得删除上述失败样本、把部分结果拼接成完整报告，或提前勾选阶段 3 退出门槛。

## 2026-09-01 ForgeFlow 全链路峰值窗口诊断

- 仓库所有者额外授权当前时段继续使用 DeepSeek，人民币费用上限为 10 元；本次诊断及后续同轮完整基线均受该总上限约束。
- 主仓库 commit：`7f3bbd0`（PR #9 已合并）；模型与 Reasoning 继续固定为 `deepseek-v4-flash/low`。诊断发生在官方峰值窗口，采用缓存命中 `$0.014/M`、未命中 `$0.44/M`、输出 `$1.32/M` 的真实费率。
- 使用独立私有 Evidence 和 `--modes forgeflow --limit 5` 执行 `feature-01`～`feature-05`。共保存 5 个终态 Observation、12 次模型请求、输入 24914 Token（缓存命中 11904）、输出 30054 Token、推理 27073 Token；成本 `$0.045562336`，P95 延迟 69887 ms。
- `feature-03` 为 `completed`：生成的补丁可应用，显式测试和私有隐藏测试均通过，并完成 4 次模型角色调用。其余 4 个 Case 为 `failed/model_output_invalid`，均按真实失败保留，没有删除或重试。
- 5 个 Case 的 Secret 检测和危险命令执行计数均为 0；Private Grader、隐藏测试、原始 Evidence 和凭据没有发送给 Provider，原始 Evidence 继续由 `.gitignore` 排除。
- 该诊断证明完整 ForgeFlow 链可以真实走通，但只是小样本诊断，不计作 90 次正式基线。诊断结束后已确认没有付费进程残留。

北京时间 18:00 的一次性续跑已配置为使用足够长的非高峰窗口、新 Evidence 路径和费用检查。本次 `$0.045562336` 必须计入 10 元人民币总上限。正式运行必须基于包含代码级费用门禁的已合并 commit，传入固定的 `--max-total-cost-usd` 和 `--prior-cost-usd 0.045562336`；若该 commit 尚未合并，自动化必须停止且不得发起付费调用。

## 2026-09-01 正式三基线结果

- PR #12 已合并，正式执行固定在主仓库 commit `e18bfa9c5e73435634644c7c44c629d7cca07dab`；Fixture commit 为 `6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f`，Private Grader commit 为 `5942ec84d403e37385203b4c7851d1b92573548a`，三个工作区执行前均洁净且 30/30 Fixture 预检通过。
- 固定配置为 `deepseek/deepseek-v4-flash`、`reasoning-effort=low`、`max-output-tokens=16000`、`cache_hit_miss`；连续非高峰费率为缓存命中 `$0.007/M`、未命中 `$0.22/M`、输出 `$0.66/M`，官方来源为 <https://api-docs.deepseek.com/quick_start/pricing/>，价格有效截止为 `2026-09-02T01:00:00Z`。
- 三个模式分别从 `2026-09-01T10:03:37Z`、`2026-09-01T10:23:54Z`、`2026-09-01T10:49:24Z` 开始记录，报告在 `2026-09-01T11:33:30Z` 生成；全部位于同一个连续非高峰价格窗口。
- `single_agent`：8/30 通过，Completion `26.67%`，Hidden Test `30.00%`，Regression `16.67%`，Human Intervention `10.00%`，平均成本 `$0.0022`，P95 `79816 ms`。30 次请求，输入 69649、缓存命中 69120、输出 100147、推理 90329 Token，正式运行成本 `$0.066697240`。
- `planner_developer`：11/30 通过，Completion `36.67%`，Hidden Test `36.67%`，Regression `6.67%`，Human Intervention `10.00%`，平均成本 `$0.0046`，P95 `128361 ms`。55 次请求，输入 131636、缓存命中 36864、输出 177866、推理 164861 Token，正式运行成本 `$0.138499448`。
- `forgeflow`：7/30 通过，Completion `23.33%`，Hidden Test `23.33%`，Regression `10.00%`，Human Intervention `10.00%`，平均成本 `$0.0050`，P95 `130700 ms`。74 次请求，输入 149807、缓存命中 73344、输出 199841、推理 182845 Token，正式运行成本 `$0.149230328`。
- 合计 90 个终态 Observation、159 次模型请求；输入 351092、缓存命中 179328、输出 477854、推理 438035 Token。正式 Evidence 成本 `$0.354427016`；加上同一授权下此前诊断 `$0.045562336` 后，授权周期总成本 `$0.399989352`，按 2026-09-01 中国外汇交易中心中间价 1 USD = 6.7809 CNY 换算约 `2.7123 CNY`，低于 10 元授权和 `$1.40` 代码硬上限。
- 所有 90 个 Observation 均有成本和时长；Secret 检测 0，危险命令执行 0。失败样本原样保留：`model_output_invalid` 51、显式测试失败 4、隐藏测试失败 6、`internal_error` 3；9 个 Observation 记录人工介入，其中 3 个终态为 `approval_required`。没有删除失败后重跑成成功。
- 原始 Evidence SHA-256：`9674C3A46E2226672BB8D29B97E23F181A0E899E7384E3A84673F7766DF8DDDC`；私有 JSON 报告 SHA-256：`F58D631F9E3D89A163921013C041B8C5445F8974D979EB837A8A5D30959E7FD3`；私有 Markdown 报告 SHA-256：`50224CC2F139ABC175C6E55002CA949A50439211EE96FB26641767101605494E`。三份文件都位于 `.forgeflow/evals` 并由 `.gitignore` 排除，只提交本节脱敏汇总。
- 这些指标显示完整 ForgeFlow 当前并未优于简化基线，不能据此宣传性能提升；它们构成后续 Prompt/模型候选版本的真实初始对照。仓库所有者 `wxxsimply` 已于 `2026-09-01T13:09:19Z` 人工复核，并以 PR #13 和明确确认签署 `APPROVED AS BASELINE`。

## 阶段 3 完成记录

- [x] 三种模式各 30 个终态 Observation，共 90 次真实执行。
- [x] 成本和 P95 延迟数据完整。
- [x] JSON/Markdown 对比报告已生成并完成技术完整性检查。
- [x] JSON/Markdown 对比报告已由仓库所有者人工复核。
- [x] 脱敏汇总已由人工签署 `APPROVED AS BASELINE` 结论。
- [x] 原始 Evidence 已确认由 `.gitignore` 排除，未进入当前 Git 候选修改。

阶段 3 已满足退出门槛，可以进入阶段 4；该签署不等同于候选 Prompt、模型或 Production 发布批准。
