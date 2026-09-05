# ForgeFlow 完成度审计（2026-09-03 更新）

## 结论

ForgeFlow 的主体工程实现、Git/GitHub 基线、30 个真实 Fixture、隔离 Private Grader 和阶段 3 真实三基线均已完成；仓库所有者已将阶段 3 报告签署为后续候选的真实初始对照。阶段 4 的 Developer v1/v2 正式对照也已完成，但自动 Gate 阻断 v2。项目目前仍不满足 `v1.0.0` 最终发布条件：新的不可变候选 Eval、人工 Promotion/rollback 和双版本镜像演练尚未完成，后续还缺少 Registry、域名、服务器 Secret 和公网 Staging 验收环境。

| 阶段 | 结论 | 证据/剩余项 |
|---|---|---|
| 0 工程基线 | 完成 | format/test/vet/build、首次 commit、受保护 `main` 与 GitHub CI 已完成 |
| 1 Graph Runtime | 完成 | 乐观锁、Checkpoint、超时、重试、幂等、并行、取消/恢复及预算测试 |
| 2 Repository Harness | 完成 | worktree、路径/链接边界、Diff/Artifact 与真实 Git fixture 测试 |
| 3 Model/Planner | 完成 | Provider、严格 Schema、版本化 Prompt、预算和审批 |
| 4 Policy/Tool/Sandbox | 工程完成 | allowlist、默认断网、非 root、资源限制；公网 Staging 沙箱 smoke test 待执行 |
| 5 Developer/真实测试 | 完成 | 有界 Patch、真实命令退出码、有限 Repair 与原仓库不变测试 |
| 6 Reviewer/Security/Judge | 完成 | 独立上下文、并行分支、确定性门禁和安全扫描 |
| 7 PostgreSQL/Queue | 完成 | Migration、Outbox、租约、恢复、暂停/取消与数据库集成测试；CI 新增 PostgreSQL service 验证 |
| 8 API/Auth/RBAC | 完成 | REST/SSE、Session/CSRF、RBAC、资源隔离与审计 |
| 9 Web | 完成 | Run/Approval/Diff/Trace/Report/Eval 页面、OpenAPI 类型和浏览器测试 |
| 10 Observability/Eval | 阶段 3 完成、阶段 4 进行中 | 指标、Trace、远端隔离 Grader、30 个真实 Fixture、三基线 Evidence 和初始报告签署已完成；Developer v2 被正式 Gate 阻断，v3 两次 smoke 未形成有效比较，正在审核 v4 候选；正式候选对照与 Promotion/rollback 待完成 |
| 11 部署/安全 | 部分完成 | Compose、mTLS 沙箱引擎、HTTPS、备份/恢复/告警/回滚脚本与文档已完成；真实 Staging 验收未完成 |

## 本轮补齐的工程缺口

1. Worker 根据 `FORGEFLOW_WORKFLOW_MODE` 组装 Planning 或完整 Development Graph；完整模式包含 Planner、Developer、Reviewer、Security、Policy、Tool 和 Docker Runner。
2. API 的审批及暂停/恢复/取消只提交状态与 Outbox，由 Worker 独占执行副作用；Worker 能正确处理已批准等待态和持久化取消。
3. Staging 使用独立 Docker-in-Docker 执行面，并通过内部网络和自动生成的 mTLS 客户端证书访问；未挂载宿主 Docker Socket。
4. 新增 Eval/Prompt Governance Migration、报告导入/查询、Agent/Prompt Catalog、Promotion/rollback API 和 Eval Web 页面。
5. Agent Patch 硬保护已覆盖策略源码、版本化 Prompt 和内置 Eval 数据，Agent 不能修改自己的治理边界。
6. CI 新增 Staticcheck、govulncheck、OpenAPI 生成差异、Migration 合约及真实 PostgreSQL 集成任务。
7. Eval CLI 新增 `--fixture-repository`，明确区分“数据结构合法”和“30 个真实 commit 均存在”。
8. govulncheck 初次发现 12 个可达漏洞后，项目升级 pgx 5.9.2、OpenTelemetry 1.44.0 及安全修复后的传递依赖；2026-08-18 GitHub CI 又识别出 Go 1.26.5 标准库的新公告，因此工具链基线继续升级到 Go 1.26.6，复扫结果为 0 个可达漏洞。

## 本轮验证结果

- `go test -timeout 300s ./...`：通过。
- `go vet ./...`：通过。
- `staticcheck 2026.1 ./...`：通过。
- `govulncheck v1.6.0 ./...`：0 个可达漏洞。
- `npm run check`：TypeScript、11 个 Vitest 测试和生产构建通过。
- OpenAI/Staging Compose 合并配置：通过。
- PostgreSQL 17 CI 上的 Migration 1～5、`internal/postgres` 和 `internal/httpapi` 集成测试：通过。
- Windows 本机 `go test -race` 无法启动，所有测试进程统一返回系统错误 `0xc0000139`；这不是数据竞争报告。Linux CI 保留 `go test -race ./...` 作为强制门禁。

## 仍需真实环境完成的发布门槛

1. 使用同一精确 Git SHA、Fixture、Private Grader、模型、Reasoning、价格窗口和共享预算运行 Developer v1/v2 候选对照，并由 Admin 签署结论。
2. 对获批候选完成 Worker drain、双版本镜像、Active Release Readiness、Promotion 和 rollback 演练；未获批则保留 `developer/v1`。
3. 构建、扫描、签名并由发布负责人手动上传不可变镜像，记录 digest、SBOM 和漏洞门禁。
4. 准备域名、DNS、TLS 邮箱、Registry、OpenAI/PostgreSQL/Alert Secret 和专用 Staging 主机，执行 Preflight 与 Release。
5. 在 Staging 签署 HTTPS 全链路、Sandbox、告警投递、备份恢复、版本回滚和 3～5 分钟 Demo 证据。
6. 只有上述结果及 Production 准备全部通过，才允许打 `v1.0.0`、开放 Production 流量或宣传候选改进结论。
