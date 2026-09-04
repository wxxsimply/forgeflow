# ForgeFlow（Go）

ForgeFlow 是一个可治理的多 Agent 软件交付平台。当前版本先实现最小纵向链路：

完整开发、接口、登录、Prompt、评测和部署路线见 [FORGEFLOW_GO_IMPLEMENTATION_GUIDE.md](./FORGEFLOW_GO_IMPLEMENTATION_GUIDE.md)。

```text
Task -> Planner -> Plan Approval -> Worktree -> Developer -> Patch Approval -> Diff -> Test
                                                                                -> Reviewer + Security (isolated parallel branches)
                                                                                -> Judge -> Repair / Human Approval / End
```

当前已完成计划审批链路、Graph Runtime 可靠性基础、Repository Harness、模型与 Planner、Policy/Tool/Sandbox 安全底座、Developer/Reviewer/Security/Judge 纵向链路、PostgreSQL/Worker 生命周期、HTTP API/Session/RBAC、Web 登录与 Run 控制台、阶段 10 的 Observability/Eval 工程，以及阶段 11 的 Staging Compose、HTTPS、Secret、备份恢复、监控告警、发布回滚和安全文档。目标仓库只接受策略授权的结构化工具调用；修改发生在独立 Git worktree，原仓库保持不变。网络和未知工具默认拒绝，真实 Docker 执行默认关闭。

## 当前能力

- Go 单模块、标准库优先
- 可替换的 `Planner` 接口与 Mock Planner
- `ExecutionPlan` 确定性结构校验
- 最小 `Node` / `Edge` / `Runtime`
- Checkpoint 版本与乐观锁冲突检测
- 节点超时、有限重试和可配置重试判定
- 节点执行记录、幂等复用和未知副作用保护
- Run 时间、迭代和节点调用预算
- 隔离并行分支与确定性 Join
- 持久化取消和 Context 取消
- 人工审批中断、批准恢复、拒绝取消
- JSON Checkpoint 原子写入
- Run、Node、Approval 事件记录
- Git 仓库检查与 base revision 固定 commit 解析
- `AGENTS.md`、README、构建与测试配置发现
- 有大小和条数上限的文件列表、文本读取与代码搜索
- 路径穿越、绝对路径和符号链接逃逸防护
- 隔离 Git worktree、标准二进制 Patch 与 SHA-256 Artifact
- `ModelProvider` 契约、Fake Provider 与 OpenAI Responses API Adapter
- 版本化 Planner Prompt、Prompt SHA-256 与不可信仓库上下文隔离
- 严格 JSON Schema、未知字段拒绝、步骤依赖与环检测
- 模型调用、Token、延迟、Prompt 版本和可配置成本估算记录
- 模型调用/Token/成本预算与固定 Planner Eval
- 版本化 Tool Registry、严格输入/输出契约与统一审计记录
- 只读仓库工具：文件列表、读取、搜索、项目规则和 Git 状态
- 确定性 Policy Engine：默认拒绝未知工具、网络、越界路径和未知命令
- 参数化测试/静态检查命令白名单，不经过 Shell 字符串解释
- Docker Sandbox 与 Fake Runner：非 root、只读根文件系统、默认无网络和资源上限
- 工具调用量/输出字节预算、输出脱敏、超时与错误分类
- 高风险工具动作审批，可跨 Checkpoint 恢复并校验输入/工作区/版本绑定
- 版本化 Developer Prompt、严格 `ImplementationResult` Schema 与独立上下文构建
- 受审批约束的统一文本 Patch 工具，阻止越界、符号链接、保护文件和声明不一致
- 实际 Git Diff Artifact、文件数/字节数/行数变更预算和批准范围复核
- 根据构建配置选择白名单测试命令，成功结论仅来自记录的退出码
- 版本化 Reviewer/Security Prompt、严格 Schema 和仅含最终证据的独立上下文
- Reviewer 与 Security 隔离并行执行，超时或失败以明确结果进入确定性 Join
- 新增行安全扫描覆盖凭据、私钥、Shell 执行、过宽权限和全网开放，模型不能降级确认风险
- Judge 按测试退出码、阻断 Review、高风险 Security、保护文件、Diff 和预算作确定性决策
- 未确认的高风险问题绑定证据摘要进入人工审批；批准对象变化时拒绝复用
- 测试、Review 或 Security 确认失败最多进入一次同范围修复，修复证据与预算持久化
- 临时 Git fixture 端到端验证：审批重启、真实 `go test`、安全漏洞拦截、原仓库不变
- PostgreSQL Migration、显式 Schema 版本检查和 pgx 连接池
- Run 投影、Checkpoint、追加事件、审批、节点执行与下一任务 Outbox 的事务提交
- PostgreSQL Job Queue：`SKIP LOCKED` 独占租约、Heartbeat、过期回收、退避重试和 Dead Letter
- 独立 `forgeflow-worker` 进程与持久化取消轮询；两个 Worker 依赖乐观锁避免重复副作用
- Run 暂停、兼容性校验恢复和取消；恢复会核对 workspace/base commit、Prompt、Policy 与审批证据
- Artifact 大对象保存在受管目录，PostgreSQL 只保存 SHA-256、大小、类型和 storage key 元数据
- 真实 PostgreSQL 集成测试及一次备份/恢复演练
- `/api/v1` REST、SSE sequence 恢复和 OpenAPI 3.1 契约
- Argon2id、仅存 Token Hash 的 Session、Secure/HttpOnly/SameSite Cookie 和 CSRF
- Admin/Operator/Viewer RBAC、资源 owner SQL 过滤和仓库根目录限制
- 登录/Run/审批限速、Run 并发幂等键、审批 ETag 与审计日志
- React + TypeScript Web：登录、`/auth/me` 恢复、路由守卫、Session 管理和响应式应用壳
- Run cursor 列表、只读详情、Graph 摘要、事件时间线和 SSE sequence 恢复
- OpenAPI 自动类型生成、前端状态测试、Chromium E2E 和 Axe 无障碍门禁
- OpenTelemetry HTTP/Run/Node/Model/Tool span、W3C Trace Context 与无 Collector 安全降级
- Prometheus 低基数指标：Run、节点、模型成本、工具、审批、Queue、认证和 429
- 固定 30 Case 软件 Eval、隔离的三基线执行器、工作区外私有 Grader、原子断点恢复证据、JSON/Markdown 报告、受控 Prompt 候选差异报告和 Promotion 门禁
- Go 1.26.6 多阶段 API/Worker/CLI 镜像、受 CSP/HSTS 保护的静态 Web 镜像
- Caddy 自动 HTTPS、内部 Compose 网络、Docker Secret `_FILE` 注入和 API/Worker 权限隔离
- Prometheus/Alertmanager/OTel Collector、备份校验、隔离恢复演练和 Schema 安全回滚
- Threat Model、Operations、Security Review、Demo、ADR 和故障演练复盘
- CLI 与基础测试

## 快速开始

```powershell
./scripts/verify.ps1
```

Linux/macOS 也可以执行 `make verify`。两者都会检查格式、运行测试和 `go vet`，并构建 CLI。

可选环境变量见 [.env.example](./.env.example)：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `FORGEFLOW_ENV` | `development` | `development/test/staging/production` |
| `FORGEFLOW_LOG_LEVEL` | `info` | `debug/info/warn/error` |
| `FORGEFLOW_SERVICE_VERSION` | `development` | Trace 中记录的服务版本 |
| `FORGEFLOW_OTEL_ENDPOINT` | 无 | 可选 OTLP HTTP traces 地址；为空时安全降级 |
| `FORGEFLOW_OTEL_SAMPLE_RATIO` | `0.1` | Trace 采样率，范围 `0..1` |
| `FORGEFLOW_METRICS_ENABLED` | `true` | 是否开放 `/metrics` |
| `FORGEFLOW_DATA_DIR` | `.forgeflow` | Checkpoint 和本地运行数据目录 |
| `FORGEFLOW_WORKFLOW_MODE` | `planning` | `planning` 仅运行计划审批；`development` 仅允许在 Worker 中启用完整开发图 |
| `FORGEFLOW_PLANNER_MODE` | `mock` | `mock` 或 `openai` |
| `FORGEFLOW_MODEL_PROVIDER` | `openai` | OpenAI-compatible 端点身份：`openai` 或 `deepseek` |
| `FORGEFLOW_PLANNER_MODEL` | `gpt-5.6` | OpenAI-compatible Planner 模型 |
| `FORGEFLOW_PLANNER_PROMPT_VERSION` | `planner/v1` | 版本化 Prompt |
| `FORGEFLOW_PLANNER_TIMEOUT` | `120s` | 单次 Planner 调用超时 |
| `FORGEFLOW_DEVELOPER_MODEL` | `gpt-5.6` | Developer 使用的模型 |
| `FORGEFLOW_DEVELOPER_PROMPT_VERSION` | `developer/v1` | 版本化 Developer Prompt |
| `FORGEFLOW_DEVELOPER_TIMEOUT` | `5m` | 单次 Developer 调用超时 |
| `FORGEFLOW_REVIEWER_PROMPT_VERSION` | `reviewer/v1` | 版本化 Reviewer Prompt |
| `FORGEFLOW_REVIEWER_TIMEOUT` | `2m` | 单次 Reviewer 调用超时 |
| `FORGEFLOW_SECURITY_PROMPT_VERSION` | `security/v1` | 版本化 Security Prompt |
| `FORGEFLOW_SECURITY_TIMEOUT` | `2m` | 单次 Security 调用超时 |
| `FORGEFLOW_POSTGRES_ENABLED` | `false` | 是否启用 PostgreSQL Checkpoint 与 Queue |
| `FORGEFLOW_POSTGRES_DSN` | 无 | PostgreSQL 连接串，仅从 Secret/环境注入 |
| `FORGEFLOW_HTTP_ADDRESS` | `127.0.0.1:8080` | API 监听地址 |
| `FORGEFLOW_REPOSITORY_ROOTS` | `.` | 允许登记的仓库根目录，逗号分隔 |
| `FORGEFLOW_SESSION_TTL` | `24h` | Session 绝对有效期 |
| `FORGEFLOW_ARTIFACT_ROOT` | `.forgeflow/artifacts` | Artifact 大对象目录 |
| `FORGEFLOW_WORKER_LEASE_TTL` | `30s` | Worker Job 租约时长 |
| `FORGEFLOW_WORKER_METRICS_ADDRESS` | `127.0.0.1:9091` | Worker 内部健康与 Metrics 地址 |
| `FORGEFLOW_GOVERNANCE_ENFORCE_ACTIVE_RELEASES` | `false` | 启用后 Worker 启动、接 Job 和 `/readyz` 都要求数据库 Active Prompt/模型 Release 与镜像完全一致；受控 Staging/Production 必须设为 `true` |
| `OPENAI_API_KEY` | 无 | OpenAI-compatible Provider 凭据；兼容变量名，禁止写入仓库 |
| `FORGEFLOW_DOCKER_ENABLED` | `false` | 是否允许真实 Docker 沙箱执行 |
| `FORGEFLOW_SANDBOX_IMAGE` | 无 | 启用 Docker 时必须是固定 sha256 digest 的镜像 |
| `FORGEFLOW_SANDBOX_WORKSPACE_ROOT` | `.forgeflow/workspaces` | Docker 唯一允许挂载的受管工作区根目录 |

创建计划：

```powershell
go run ./cmd/forgeflow plan --task "为订单创建接口增加幂等机制" --mode mock
```

输出中会包含 `runId`，状态为 `waiting_for_plan_approval`。继续执行：

```powershell
go run ./cmd/forgeflow approve --run <runId> --comment "计划通过"
go run ./cmd/forgeflow show --run <runId>
go run ./cmd/forgeflow cancel --run <runId> --reason "不再需要"
```

运行数据保存在 `.forgeflow/runs/<runId>.json`。

PostgreSQL API、首次管理员和 Worker 的完整启动方式见 [阶段 8 运行说明](./docs/phase-8-http-api-auth-rbac.md)。OpenAPI 在 API 启动后可从 `http://127.0.0.1:8080/api/openapi.yaml` 获取。

Web 安装、启动和测试见 [阶段 9 Web 基础说明](./docs/phase-9-web-foundation.md)。开发时进入 `web` 目录运行 `npm ci` 和 `npm run dev`，然后打开 `http://127.0.0.1:5173`。

检查仓库及其项目规则：

```powershell
go run ./cmd/forgeflow inspect --repository . --base HEAD
```

该命令只读取仓库，输出固定后的 base commit、工作区状态和发现的规则/配置文档。

运行不联网的 Planner 基线评测：

```powershell
go run ./cmd/forgeflow eval --suite planner/v1
```

阶段 10 的完整 30 Case、真实 evidence 报告和 Promotion 命令见 [Observability 与 Eval 说明](./docs/phase-10-observability-eval.md)。Developer Prompt 候选的受控运行见[阶段 4 Eval 操作手册](./docs/stage-4-developer-v2-eval-runbook.md)；可先用 `-SmokeOnly -SmokeCaseLimit 1` 执行默认 2 Observation、目标 10 分钟内完成的候选筛查，正式 Promotion 仍要求完整 180 Observation 对照。

三基线真实执行入口会拒绝脏工作区、缺失 Key、零价格和不干净 Grader，防止无法追溯或虚构成本的运行。原始 Evidence 默认写入已被 Git 忽略的 `.forgeflow/evals`：

```powershell
$env:OPENAI_API_KEY="从安全密钥存储注入"
go run ./cmd/forgeflow eval execute --suite software/v1 `
  --fixture-repository D:\fixtures\forgeflow-eval-fixtures `
  --grader-repository D:\fixtures\forgeflow-eval-grader `
  --modes single_agent,planner_developer,forgeflow `
  --developer-prompt-version developer/v1 `
  --provider <openai-or-deepseek> `
  --model <固定模型> `
  --pricing-mode <cache_hit_miss-or-cache_read_write> `
  --pricing-source <官方HTTPS价格页> `
  --pricing-valid-from <RFC3339价格生效时间> `
  --pricing-valid-until <RFC3339价格有效截止时间> `
  --input-usd-per-million <当前真实输入价格> `
  --cached-input-usd-per-million <当前真实缓存输入价格> `
  --cache-write-input-usd-per-million <仅cache_read_write模式需要> `
  --output-usd-per-million <当前真实输出价格> `
  --max-total-cost-usd <本轮完整Eval的硬费用上限> `
  --prior-cost-usd <同一授权中已在其他Evidence花费的USD> `
  --output .forgeflow\evals\evidence.json
```

命令按 Case 原子保存，意外中断后原样重跑即可跳过已完成项。恢复时，现有 Evidence 的实测费用会和 `--prior-cost-usd` 一起计入同一个上限；每次模型调用前会按请求字节数、最大输出 Token 和最高适用输入费率预留保守最大费用，价格窗口尚未开始、额度不足或有效期不足时都不会联系 Provider。预算、此前费用、价格起止时间和实际加载的 Developer Prompt 版本会写入 Evidence 配置，配置漂移时不能续写同一路径。候选对照必须分别使用 `developer/v1` 与 `-CandidatePromptVersion` 指定的不可变候选，并写入两个新的私有 Evidence 路径。`forgeflow eval compare` 会验证两份三模式报告除 Developer Prompt 和累计 campaign cost 外完全可比，输出各模式指标增量及自动 Gate 结果，但不会代替人工批准。不要把 Key、私有 Grader 或原始 Evidence 提交到 GitHub。

Staging 部署从 [Operations Runbook](./docs/operations.md) 开始。复制 `deploy/staging/staging.env.example`，创建本机 Secret 后先运行：

```powershell
./scripts/staging-preflight.ps1
./scripts/staging-release.ps1 -Release 0.11.0 -ConfirmDeploy
```

部署拓扑不会公开 API、Worker、数据库或监控端口；只有 Caddy 对外提供 80/443。阶段 3 真实三基线已获人工批准为后续候选对照基线；阶段 4 的 Developer v2 正式对照已完成但被自动 Gate 阻断，v3 对照、Promotion/rollback 和公网 Staging 尚未验收，因此仍不能据此批准 Production。

启用真实 Provider 时，在 Worker/当前进程环境中设置 `OPENAI_API_KEY`，并确保目标仓库存在可解析的 Git commit：

```powershell
$env:OPENAI_API_KEY="从安全密钥存储注入"
go run ./cmd/forgeflow plan --task "为订单接口增加幂等机制" --repository . --base HEAD --mode openai
```

真实模式会把任务和受限的仓库规则上下文发送到配置的模型端点，但不会把 API Key、完整 Prompt 或仓库文件内容写入 Checkpoint。模型价格会变化，因此只有显式配置每百万 Token 价格时，`estimatedCostUsd` 才标记为已配置。

## 目录

```text
cmd/forgeflow/         CLI 入口
internal/application/ 用例编排
internal/auth/        Argon2id、Session、CSRF 和认证存储
internal/artifact/    Artifact 文件存储与 PostgreSQL 元数据
internal/apperror/    统一错误分类
internal/assessment/ Reviewer/Security 共享的只读证据上下文
internal/checkpoint/  Checkpoint 抽象与文件实现
internal/config/      环境配置加载与校验
internal/controlplane/ 用户可见资源的 owner-aware PostgreSQL 查询
internal/domain/      RunState、ExecutionPlan 等领域模型
internal/developer/   Developer、上下文、Prompt 与 ImplementationResult Schema
internal/graph/       Graph 定义与运行时
internal/httpapi/     REST/SSE Handler、RBAC、中间件与 OpenAPI
internal/judge/       确定性门禁、Repair 与人工复核决策
internal/model/       ModelProvider、Fake 与 OpenAI Responses Adapter
internal/observability/ 结构化日志
internal/planner/     Planner、Prompt、Schema 与 Eval
internal/postgres/    PostgreSQL 连接与 Schema 版本检查
internal/policy/      路径、命令、网络、预算与审批策略
internal/repository/  安全文件读取、Git 检查、worktree 与 Diff
internal/queue/       PostgreSQL/内存 Queue、租约和 Outbox 发布
internal/reviewer/    独立代码审查 Agent、Prompt 与 Schema
internal/sandbox/     Docker/Fake 沙箱执行器与资源隔离
internal/security/    独立安全 Agent 与不可覆盖的确定性扫描
internal/tool/        Tool 契约、注册表、运行时和内置工具
internal/worker/      Worker 心跳、取消传播和任务处理
migrations/          PostgreSQL Up/Down Migration 与显式 Runner
web/                 React、TypeScript、OpenAPI Client 与浏览器 E2E
```

`Planner` 已通过 `ModelProvider` 隔离。`mock` 是默认模式；`openai` 模式缺少 API Key 时明确失败，不会静默降级。OpenAI Adapter 使用 Responses API 的严格结构化输出，并在 Go 端再次校验。

## 许可证

ForgeFlow 使用 [Apache License 2.0](./LICENSE) 授权。第三方依赖仍遵循各自许可证；发布二进制、容器镜像或其他再分发产物时，应保留适用的许可证和归属声明。

## 剩余发布验收

阶段 0～3 已完成，阶段 4 正在完成 Developer Prompt 候选对照和治理演练，后续还需不可变镜像、公网 Staging、运维安全、Production 准备和 `v1.0.0` 发布验收。准确状态与执行顺序见[后续分阶段路线图](./FORGEFLOW_POST_IMPLEMENTATION_ROADMAP.md)。
