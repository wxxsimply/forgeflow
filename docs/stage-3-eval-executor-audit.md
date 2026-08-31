# ForgeFlow 阶段 3：三基线执行器审计

> 日期：2026-08-31  
> 状态：执行器与双 Provider 计费已合并，非付费预检通过；等待轮换后的本地 Key 和人工付费授权
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
- Evidence 记录 Provider、计费模式、官方价格来源、价格有效截止时间和各项费率；有效期不足以覆盖下一次调用时会在发出付费请求前停止。
- 拒绝、超时、审批请求、模型输出错误和测试失败都形成 Observation，不从统计中删除。
- Evidence 在每个 Case 后通过同目录临时文件、Sync 和原子 Rename 更新；同配置重跑会跳过已记录 Case。
- 恢复时若 Git、模型、Prompt、Policy、Tool、Fixture 或 Grader 配置变化，会要求使用新的 Evidence 路径。
- Evidence 不写任务正文、Prompt、模型原始输出或工作区路径；失败消息会脱敏路径、Key 和凭据模式。

## 强制前置门禁

`eval execute` 在付费调用前要求：

1. 主仓库工作区干净，并记录精确 40 位 Git SHA。
2. Private Grader 工作区干净，并记录精确 Git SHA。
3. 30 个 Fixture SHA 全部可解析，并记录 Fixture HEAD。
4. OpenAI-compatible Provider Key 只通过兼容变量 `OPENAI_API_KEY` 从当前进程环境注入；变量名不代表必须使用 OpenAI。
5. 显式选择 Provider 和计费模式，并提供当前真实价格、官方 HTTPS 来源及 RFC3339 有效截止时间。
6. `cache_hit_miss` 只接受普通输入、缓存命中和输出价格；`cache_read_write` 还必须提供缓存写入价格。
7. 三种模式和输出路径合法；原始 Evidence 位于 Git 忽略的 `.forgeflow/evals`。

## 已通过的本地验证

- Eval 数据集、报告和 Promotion 既有测试。
- Evidence 原子写入、单 Case 恢复和配置漂移拒绝测试。
- 禁止路径、Diff 预算、Secret 检测和失败消息脱敏测试。
- 临时真实 Git 仓库中的 Patch apply 与 `go test` 退出码集成测试。
- 本地 Private Grader 自身测试和 30 Case Fixture commit 预检。
- 全仓库 Go 测试、格式、Vet 和项目验证脚本（最终结果以提交前命令输出为准）。

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
  --pricing-valid-until <当前峰谷价格窗口结束时间，RFC3339> `
  --input-usd-per-million <当前缓存未命中价格> `
  --cached-input-usd-per-million <当前缓存命中价格> `
  --output-usd-per-million <当前输出价格> `
  --output .forgeflow\evals\evidence.json
```

DeepSeek 高峰与非高峰价格不同。完整三基线必须在同一个官方价格窗口内运行；优先选择周末或足够长的非高峰窗口。可先增加 `--limit 1` 做一次付费 smoke，确认结构化输出、Token 和成本正确后去掉该参数，原命令会从下一个 Case 继续。如果价格窗口到期，必须停止并使用新的 Evidence 路径，不能用新价格续写旧配置。

完成 90 个 Observation 后生成私有 JSON 与 Markdown 对比报告：

```powershell
go run ./cmd/forgeflow eval --suite software/v1 --evidence .forgeflow\evals\evidence.json --format json --output .forgeflow\evals\comparison.json
go run ./cmd/forgeflow eval --suite software/v1 --evidence .forgeflow\evals\evidence.json --format markdown --output .forgeflow\evals\comparison.md
```

## 尚未完成，禁止提前声明

- [ ] 三种模式各 30 个终态 Observation，共 90 次真实执行。
- [ ] 成本和 P95 延迟数据完整。
- [ ] JSON/Markdown 对比报告已生成并人工复核。
- [ ] 脱敏汇总已由人工签署 Promotion 结论。
- [ ] 原始 Evidence 已确认未进入 Git/GitHub。

这些项目完成前，阶段 3 保持“进行中”，不能进入阶段 4。
