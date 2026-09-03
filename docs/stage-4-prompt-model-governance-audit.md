# ForgeFlow 阶段 4：Prompt 与模型发布治理审计

> 状态：治理代码、Developer v2 候选、生产 Eval 绑定、受控双运行、多模式候选差异报告、价格生效窗口门禁和受控治理演练工具已由 PR #14、#15、#20、#21、#22、#23、#24、#25 合并并通过四项必需检查；正式候选 Eval 和受控双版本 Promotion/rollback 人工演练尚未执行

## 1. 已实现的控制

- Prompt Release 新增模型绑定；同一不可变记录包含 Agent、Prompt version、Prompt SHA-256、模型、Eval Run ID、操作者、原因、回滚来源和创建时间。
- Promotion 同时核对 Eval 中的 Prompt 与模型版本，且必须提供非空原因；模型升级不能绕过现有 Eval Gate。
- Rollback 只允许选择仍嵌入当前镜像、SHA 完全一致且模型仍由当前镜像配置的目标；回滚创建新记录，不覆盖历史。
- Worker 在 `FORGEFLOW_GOVERNANCE_ENFORCE_ACTIVE_RELEASES=true` 时执行启动预检、`/readyz` 动态校验和领取 Job 前复检。
- 数据库 Active Release 与 Worker 的 Prompt version、Prompt SHA 或模型任一不一致时，Worker 不接收新 Job。
- 已经执行中的 Run 不会被 Promotion 热替换；新 Active Release 会使旧 Worker 在下一次接 Job 前失败，必须 drain 并重启到匹配镜像。
- Resume Guard 记录并核对 Prompt version/SHA、模型、Policy、Tool、Workspace、审批和 Git base commit。
- Prompt Loader 使用版本目录并通过 `go:embed prompts/*/*` 保留旧版本；Rollback 还会重新计算嵌入文件 SHA，避免只凭数据库字符串回滚。

## 2. 数据库与 API 变更

- Migration：`000005_prompt_release_model`。
- `PromptRelease.model` 为必需 OpenAPI 字段。
- Promotion/rollback 的 `comment` 为必填原因。
- 审计详情包含 `model`、`evalRunId`、`reason`、目标 Release 和 rollback 来源；审计主体继续使用已认证 Admin 用户。

## 3. 自动验证

- Active Release 完全匹配时通过，模型不匹配时失败。
- Worker `/readyz` 对治理不一致返回 HTTP 503，对匹配状态返回 HTTP 200。
- 暂停 Run 的 Prompt、模型、Policy、Tool 绑定匹配时可恢复，不兼容 Worker 配置会拒绝恢复。
- Migration、Governance、Lifecycle、Config、Worker 和 HTTP API 重点测试通过。
- 新增 PostgreSQL API 集成测试，验证两次 Promotion 和一次 rollback 均保留不可变 Release 历史、只有一个 Active Release、审计记录包含操作者/原因/Eval Run ID，并逐字节确认 Promotion 前后的既有 Run checkpoint 没有被改写。
- `./scripts/verify.ps1` 全仓验证通过，包含 Go test/vet、可用时的 Staticcheck、Migration 契约、三套二进制构建以及前端类型检查、测试和生产构建。
- CI 固定版本 `go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...` 扫描通过，调用路径漏洞为 0。

当前 Windows 主机没有可用的 `FORGEFLOW_TEST_POSTGRES_DSN`，Docker Desktop Linux Engine 也未能启动，因此新增 PostgreSQL 集成测试在本地按既有测试契约明确显示为 `SKIP`，没有伪装成通过。PR #15 的 GitHub `PostgreSQL integration` Job 使用 PostgreSQL 17 实际执行并成功；同一 head 的 Go verification、Web verification 和 deployment `validate` 也全部成功。

合并证据：PR #14 合并 commit `43d676c86c68c25486e14ae0aab466057d05e941`；PR #15 合并 commit `6509f7b33bbdd5b3372bb9c3d7710cbf85397162`。

Developer v2 候选由 PR #20 合并，commit 为 `3302aeb7aa3725761bf614695ba8f2415980df81`，四项必需检查全部成功；生产默认仍为 `developer/v1`。

生产 Eval Prompt 绑定由 PR #21 合并，commit 为 `76ede9b4e875adf7e9494d7c0c38eb5f767b8de6`，四项必需检查全部成功。执行器会加载实际版本化 Prompt、复用生产响应 Schema，并在 Provider 调用前拒绝 Evidence 配置与实际 Prompt 不一致的运行。

受控双运行脚本由 PR #22 合并，commit 为 `ca3390e6c25d2c665d1e8078e3b76c8b599128a2`。脚本强制精确 SHA、独立 Evidence 路径、同一价格窗口、共享 `1.00 USD` 硬上限、显式付费确认和安全断点恢复；PR 四项必需检查全部成功。

多模式候选差异报告由 PR #23 合并，commit 为 `5f68f169dedb324a6d5cbf919f5d9af49a6ba26a`。报告强制核对三种模式的可比配置和共享 campaign cost 链路，输出指标增量与自动 Gate；三模式 Promotion 会重复执行同一校验，不能绕过人工批准。PR 四项必需检查全部成功。

价格生效窗口门禁由 PR #24 合并，commit 为 `37d7fb606a2eb9662297875a09a9a2978a1dbfd8`。Eval CLI、Evidence、断点恢复和阶段 4 双运行脚本会在任何 Provider 调用前拒绝尚未生效、已经过期、起止倒置或剩余时长不足的价格窗口；PR 四项必需检查全部成功。

受控 Promotion/rollback 演练工具由 PR #25 合并，commit 为 `27a520cc7d95d28267d5f6aed9ddbfcc65a03f4a`。工具默认只读，Eval 导入、Promotion 与 rollback 使用互不复用的显式确认开关，私有记录不包含凭据或原始 Evidence；PR 四项必需检查全部成功。

## 4. 合并后人工演练（不得由自动化代签）

1. [x] 人工复核并合并阶段 4 代码与 PostgreSQL 集成测试 PR。
2. 在隔离数据库执行 Migration 5，只启动 API，保持 Worker drained。
3. 导入经批准的 Eval Run，并依次为 planner、developer、reviewer、security 创建带原因的初始 Release。
4. 以 `FORGEFLOW_GOVERNANCE_ENFORCE_ACTIVE_RELEASES=true` 启动 Worker，确认 `/readyz` 返回 200。
5. 在数据库 Active Release 与镜像配置故意不匹配的受控环境确认 `/readyz` 返回 503，随后恢复正确配置。
6. 启动一个可暂停 Run，记录其版本绑定；Promotion 候选 Release 后确认旧 Worker 不接新 Job，且该 Run 的 Checkpoint 绑定未改变。
7. Drain Worker，部署同时含候选和旧 Prompt 的镜像，启动并验证 Readiness。
8. 由 Admin 提供原因执行 rollback，确认产生新的不可变 Release；重启 Worker 后验证目标版本生效。
9. 在本文件追加 UTC 时间、操作者、Eval Run ID、Promotion/rollback Release ID、镜像 Git SHA 和验证结果，不记录 Secret 或原始 Eval Evidence。

## 5. 当前未完成

- 真实 Promotion/rollback 演练尚未执行，因此阶段 4 仍保持“进行中”。
- `developer/v2` 已作为候选新增并保留 `developer/v1`，生产 Eval 绑定也已完成；下一门禁是按 `docs/stage-4-developer-v2-eval-runbook.md` 生成同条件 Eval 对照，再进行真实双版本镜像验收。
- 没有候选模型变更。
- Promotion/rollback 的安全操作步骤见 `docs/stage-4-governance-drill-runbook.md`；该工具不会代替正式 Eval、Admin 批准、Worker drain 或人工签署。
- 当前待合并改动为 API/Worker 健康端点增加构建 Git SHA，并要求治理演练在变更前核对运行镜像身份；合并和 CI 成功前不得将其记为验收通过。
