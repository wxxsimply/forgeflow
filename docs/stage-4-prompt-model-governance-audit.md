# ForgeFlow 阶段 4：Prompt 与模型发布治理审计

> 状态：代码门禁已实现并通过重点测试；等待人工提交 PR、合并后执行受控 Promotion/rollback 演练

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
- `./scripts/verify.ps1` 全仓验证通过，包含 Go test/vet、可用时的 Staticcheck、Migration 契约、三套二进制构建以及前端类型检查、测试和生产构建。
- CI 固定版本 `go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...` 扫描通过，调用路径漏洞为 0。

## 4. 合并后人工演练（不得由自动化代签）

1. 人工复核并合并本阶段 PR。
2. 在隔离数据库执行 Migration 5，只启动 API，保持 Worker drained。
3. 导入经批准的 Eval Run，并依次为 planner、developer、reviewer、security 创建带原因的初始 Release。
4. 以 `FORGEFLOW_GOVERNANCE_ENFORCE_ACTIVE_RELEASES=true` 启动 Worker，确认 `/readyz` 返回 200。
5. 在数据库 Active Release 与镜像配置故意不匹配的受控环境确认 `/readyz` 返回 503，随后恢复正确配置。
6. 启动一个可暂停 Run，记录其版本绑定；Promotion 候选 Release 后确认旧 Worker 不接新 Job，且该 Run 的 Checkpoint 绑定未改变。
7. Drain Worker，部署同时含候选和旧 Prompt 的镜像，启动并验证 Readiness。
8. 由 Admin 提供原因执行 rollback，确认产生新的不可变 Release；重启 Worker 后验证目标版本生效。
9. 在本文件追加 UTC 时间、操作者、Eval Run ID、Promotion/rollback Release ID、镜像 Git SHA 和验证结果，不记录 Secret 或原始 Eval Evidence。

## 5. 当前未完成

- GitHub PR 尚未由仓库所有者人工提交和合并。
- 真实 Promotion/rollback 演练尚未执行，因此阶段 4 仍保持“进行中”。
- 没有新增候选 Prompt 或模型；旧版本保留能力已实现，但仍需在真实双版本镜像中验收。
