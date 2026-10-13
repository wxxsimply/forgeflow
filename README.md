# ForgeFlow

**一个用 Go 实现的、带人工审批和安全边界的 AI 辅助开发工作流个人项目。** 输入开发任务后，系统将规划、代码修改、测试、审查与最终判定串成可追踪的流程；关键步骤必须由人批准，模型输出不能直接等同于交付结果。

**当前状态：**本仓库的个人预览配置为模拟规划与审批，不调用模型或修改源码；在自己的 Windows 电脑上可选择[本地 DeepSeek 受控开发](docs/local-deepseek.md)，使用单个私有 API 密钥处理一个允许的 Git 仓库。它不是开放给所有注册用户的自动开发服务，Production 尚未部署或验收。

## 30 秒看懂

```text
任务 → Planner → 计划审批 → 隔离 Git worktree → Developer → 补丁审批
     → 实际 Diff + 沙箱测试 → Reviewer ∥ Security → 确定性 Judge → 结束 / 人工处理
```

- **工作流可恢复：**Graph Runtime 将 Run 状态、节点执行和审批写入 Checkpoint；PostgreSQL Queue 用租约领取任务，Worker 可在重启后继续处理。
- **改动受约束：**目标仓库不直接修改；路径、命令、网络、文件范围和资源预算由策略检查，测试在受限 Docker 沙箱内执行。
- **结论有证据：**实际 Git Diff、测试退出码、独立代码/安全审查结果交给确定性 Judge；模型不能自行宣布通过或覆盖阻断项。

技术栈：**Go 1.26.9 · PostgreSQL 17 · React/TypeScript · Docker · OpenAPI · OpenTelemetry/Prometheus**。

## 核心设计与代码入口

| 问题 | 项目中的处理方式 | 代码 |
| --- | --- | --- |
| 审批时进程退出，任务如何继续？ | Graph 中断并持久化 Checkpoint、事件和审批；恢复时校验版本与工作区证据 | [`internal/graph`](internal/graph/)、[`internal/checkpoint`](internal/checkpoint/)、[`internal/application`](internal/application/) |
| 多个 Worker 如何避免重复执行？ | PostgreSQL Outbox/Job Queue、`SKIP LOCKED` 租约、心跳和乐观锁 | [`internal/queue`](internal/queue/)、[`internal/postgres`](internal/postgres/)、[`internal/worker`](internal/worker/) |
| 如何限制模型生成的危险操作？ | 结构化工具调用、默认拒绝的 Policy、隔离 worktree 与无网络沙箱；审批绑定具体范围 | [`internal/policy`](internal/policy/)、[`internal/tool`](internal/tool/)、[`internal/repository`](internal/repository/)、[`internal/sandbox`](internal/sandbox/) |
| 如何避免“模型说完成了”就算通过？ | 使用实际 Diff 和测试结果，Reviewer/Security 独立审查，Judge 按确定性规则裁决 | [`internal/developer`](internal/developer/)、[`internal/reviewer`](internal/reviewer/)、[`internal/security`](internal/security/)、[`internal/judge`](internal/judge/) |
| 用户如何查看与审批？ | REST/SSE API、Session/RBAC、Run 与审批页面；OpenAPI 生成前端类型 | [`internal/httpapi`](internal/httpapi/)、[`internal/auth`](internal/auth/)、[`web`](web/) |

模型层通过 [`ModelProvider`](internal/model/) 与流程解耦；默认使用 Mock Planner。真实执行需要显式配置凭据和运行边界，不会在缺少密钥时静默切换为模拟结果。

## 快速体验

只看无需密钥、数据库或 Docker 的计划审批链路：

```powershell
git clone https://github.com/wxxsimply/forgeflow.git
cd forgeflow
go run ./cmd/forgeflow plan --task "为订单接口增加幂等机制" --mode mock
```

输出包含 `runId` 和 `waiting_for_plan_approval` 状态；此命令**不会调用模型，也不会修改仓库**。把实际输出的 `runId` 传给 CLI 的 `show` 或 `approve` 子命令即可继续查看或批准模拟流程。

要在本机尝试真实开发，需要 PowerShell 7、Go、Node.js/npm 和 Linux 容器模式的 Docker Desktop。先阅读[本地运行指南](docs/local-deepseek.md)，再在干净克隆中执行：

```powershell
pwsh -NoProfile -File ./scripts/local-deepseek.ps1 -Action init
notepad .forgeflow\local\secrets\deepseek_api_key
pwsh -NoProfile -File ./scripts/local-deepseek.ps1 -Action start
```

网页地址为 `http://127.0.0.1:5173`；本地管理员密码由初始化脚本生成。密钥文件被 Git 忽略，仅交给本地 Worker。**提交真实任务就可能产生 DeepSeek 费用**：请使用关闭自动充值的低余额专用密钥，不要在仓库、Issue 或聊天中粘贴密钥；余额预检不是精确账单上限。停止服务：`pwsh -NoProfile -File ./scripts/local-deepseek.ps1 -Action stop`。

## 验证与边界

GitHub PR 的必需检查覆盖 Go 测试与 race/vet/Staticcheck/漏洞分析、PostgreSQL 集成、Web 类型检查/单测/Chromium E2E，以及部署资产契约。Windows 本地验证先在 `web/` 执行 `npm ci`，再回到仓库根目录运行：

```powershell
pwsh -NoProfile -File ./scripts/verify.ps1
```

项目还提供 30 Case 软件任务的评测定义与契约、三种执行链对照与确定性 Promotion Gate；**评测框架存在不代表模型效果已达到生产标准**。真实评测需要独立的 fixture、Grader、费用授权和完整证据，详见[评测说明](docs/phase-10-observability-eval.md)。

目前的边界同样重要：个人预览模式仍是 Mock；本地真实开发仅限管理员、一个允许的仓库和受控调用次数；不会自动提交 PR 或部署；公网 Staging/Production 的完整验收尚未完成。架构与运行资料是设计和操作依据，**不是已上线 Production 的证明**。

## 延伸阅读

- [本地 DeepSeek 运行指南](docs/local-deepseek.md) · [个人预览部署手册](docs/personal-preview-deployment.md)
- [PostgreSQL Queue 与恢复](docs/phase-7-postgres-queue-lifecycle.md) · [API、认证与权限](docs/phase-8-http-api-auth-rbac.md)
- [安全审查](docs/security-review.md) · [评测与观测](docs/phase-10-observability-eval.md) · [Production 参考架构](docs/production-architecture.md)
- [贡献与提交规范](CONTRIBUTING.md) · [Apache-2.0 许可证](LICENSE)
