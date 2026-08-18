# ForgeFlow Portfolio Delivery

## 项目叙事

ForgeFlow 解决的不是“让模型直接写代码”，而是如何把多 Agent 软件交付放进可审批、可恢复、可审计、可评测的工程系统。Graph 管理状态和并行流程；Agent 只返回结构化判断；Policy、Sandbox、预算和人工审批在模型之外强制执行。

## 可展示的工程深度

- 可靠性：Checkpoint 乐观锁、租约/Heartbeat、幂等 NodeExecution、取消/暂停/恢复和有限 Repair。
- 安全：worktree、路径与链接边界、命令白名单、默认无网络、Secret 隔离、CSRF/RBAC/owner 查询。
- AI 工程：版本化 Prompt、严格 Schema、Token/成本审计、独立 Reviewer/Security 上下文、确定性 Judge。
- 平台：Go API/Worker/CLI、PostgreSQL Queue、React Web、OpenAPI、SSE、OpenTelemetry、Prometheus。
- 交付：非 root 镜像、HTTPS Compose、备份恢复、告警、Release manifest、Schema 安全回滚和事故 Runbook。

## 可复现证据

```text
scripts/verify.ps1                 本地格式、测试、vet、构建、Web 检查
scripts/stage9-real-e2e.ps1        PostgreSQL/API/Worker/浏览器真实 E2E
forgeflow eval                     固定 Eval、报告和 Promotion Gate
scripts/staging-preflight.ps1      部署网络、端口、Secret 与权限契约
scripts/staging-security-drill.ps1 控制面/执行面隔离演练
scripts/staging-restore-drill.ps1  checksum + 隔离数据库恢复
scripts/demo-staging.ps1           HTTPS 登录到最终报告的演示流程
```

## 诚实边界

作品集不展示虚构成功率、成本或延迟。真实 30 Case Baseline、Production 专用 Worker、异地 Artifact、镜像漏洞扫描结果、负载/入侵演练和 Beta 数据完成前，版本保持 pre-1.0，演示环境只处理 fixture 仓库并只输出 Patch，不自动合并或部署用户代码。
