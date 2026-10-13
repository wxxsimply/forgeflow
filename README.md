# ForgeFlow

> Go + React 实现的 AI 辅助开发工作流个人项目：让模型生成的改动经过人工审批、隔离执行和证据校验，而不是直接落到目标仓库。

**当前能做什么：**个人预览提供无模型费用的模拟规划与审批；本地可使用自己的 DeepSeek 密钥，在一个受控 Git 仓库中尝试真实开发。**尚未完成 Production 验收，也不会自动发布代码。**

## 工作流

1. **规划并审批：**Planner 生成结构化计划，人工决定是否继续。
2. **隔离修改：**Developer 在独立 Git worktree 生成补丁；审批后运行受限测试。
3. **证据裁决：**Reviewer 与 Security 独立审查，Judge 根据实际 Diff、测试退出码和策略规则决定结果。

## 工程重点

- **可恢复：**[`Graph Runtime`](internal/graph/) + [`Checkpoint`](internal/checkpoint/) 记录状态和审批；[`PostgreSQL Queue`](internal/queue/) 用租约分配 Worker 任务。
- **受约束：**[`Policy`](internal/policy/) 限制路径、命令和预算；[`Git worktree`](internal/repository/) 隔离原仓库；[`Docker Sandbox`](internal/sandbox/) 默认无网络。
- **可核验：**[`API / RBAC`](internal/httpapi/)、[`Web 控制台`](web/) 展示过程；[`确定性 Judge`](internal/judge/) 不让模型自行宣布通过。

技术栈：Go 1.26.9、PostgreSQL 17、React/TypeScript、Docker、OpenAPI、OpenTelemetry。

## 快速体验

无需 API 密钥、数据库或 Docker，可先运行模拟计划：

```powershell
git clone https://github.com/wxxsimply/forgeflow.git
cd forgeflow
go run ./cmd/forgeflow plan --task "为订单接口增加幂等机制" --mode mock
```

命令会输出 `runId` 和 `waiting_for_plan_approval`；**不会调用模型或修改仓库**。要在本机尝试真实开发，请阅读[本地 DeepSeek 指南](docs/local-deepseek.md)：仅限本机管理员和一个允许的仓库，调用可能收费，余额预检不是精确费用上限。

## 验证与边界

PR 检查覆盖 Go 测试与 race/vet/Staticcheck、PostgreSQL 集成、Web 单测/浏览器测试和部署资产契约。项目还提供[30 Case 评测定义与门禁](docs/phase-10-observability-eval.md)，但**评测框架不等于生产效果证明**。个人预览仍为 Mock；本地真实开发不是多用户执行平台，公网 Staging/Production 的完整验收尚未完成。

[运行与认证](docs/phase-8-http-api-auth-rbac.md) · [安全审查](docs/security-review.md) · [Production 参考架构](docs/production-architecture.md) · [贡献指南](CONTRIBUTING.md) · [Apache-2.0](LICENSE)
