# 阶段 9 集中验收预检审计

> 状态：重新进入检查完成；仍停在私有 Freeze 门禁前
> 日期：2026-09-08；重新核对：2026-09-18

## 已完成

- PR #36 已合并，阶段 8 的四项必需检查全部成功，合并提交为 `bf6f59b67bff62ded208ed977e642177450cf7c2`。
- PR #38 已合并安全 unified diff 规范化与回归测试，合并提交为 `0cf6ded008d8bba4f7f7bc1ff4f391131467739f`；该修复尚未由新的付费候选 smoke 验证。
- PR #78 已合并个人预览能力边界文案，四项必需 CI 全部成功；2026-09-18 重新进入时的主线参考为 `7ff279e`。该短 SHA 只用于说明核对起点，不是最终冻结候选。
- 新增不含凭据的 `forgeflow.acceptance-plan/v1` 模板，固定 11 个串行门禁、负责人角色、超时、依赖和私有 Evidence 路径。
- 新增默认只读的阶段 9 预检，统一调用阶段 5～8 工程契约校验，并能校验填充计划必须位于 Git 忽略目录、无占位符、绑定当前干净 HEAD。
- 新增集中验收窗口手册和公开脱敏摘要模板。
- 2026-09-18 重新运行静态预检：完整阶段 5～9 工程契约模式与 `-SkipEngineeringValidators` CI 去重模式均通过；未读取 Key，未联系模型 Provider、Registry 或 Staging，未执行付费 Eval、Promotion、部署、Tag 或 Release。
- 2026-09-09 在已授权范围内完成一次 2 Observation `developer/v1`/`developer/v4` smoke，实际记录费用 `$0.008919152`；两侧均在 `patch_check` 被损坏补丁阻断，正式 Eval 未启动。
- 严格 unified diff 规范化和离线回归测试已经随 PR #38 进入主线；现在缺少的是从本次重新进入 PR 合并后的最终候选 SHA 运行新 smoke，旧失败不能改写为通过。

## 本地验证

- `scripts/stage-9-acceptance-preflight.ps1` 完整静态模式：通过。
- `scripts/stage-9-acceptance-preflight.ps1 -SkipEngineeringValidators` CI 去重模式：通过。
- PowerShell AST 语法解析：通过。
- `scripts/verify.ps1`：2026-09-18 重新运行通过，包括 Go 全包测试、Vet、构建、前端类型检查、23 个 Vitest 测试和生产构建。
- `git diff --check`、冲突标记与凭据模式扫描：提交前执行并保持通过。

## 当前阻断

1. 本次重新进入状态校正文档尚待人工提交和 PR 合并；合并后必须先选定最终候选 40 位 SHA，不能预先把当前分支或短 SHA 写入私有冻结计划。
2. `developer/v1` 与 `developer/v4` 的旧 smoke 均出现 `patch_check` 损坏；规范化修复已进入主线，但尚未由新候选 smoke 验证。
3. 费用/数据授权记录、独立评审人、值班、Registry、Staging 和 Production 私有配置尚未填入本地验收计划。
4. `P8-001`～`P8-007` 仍全部是 Production Go 阻断项。

## 下一人工节点

仓库所有者审核并合并本次重新进入状态校正 PR。四项必需检查成功后，从新的 `main` 选定完整 40 位候选 SHA，并在仓库外准备 Staging HTTPS origin、签名身份、OIDC issuer、值班与独立安全复核记录、Eval 数据范围和费用上限记录；再建立或更新 `.forgeflow/acceptance/1.0.0/plan.json` 并运行填充计划预检。只有预检通过且当次付费/数据范围授权仍有效时，才能明确授权使用新 Campaign 重跑候选 smoke。
