# 阶段 9 集中验收预检审计

> 状态：进行中，仅完成无外部副作用的验收窗口准备
> 日期：2026-09-08

## 已完成

- PR #36 已合并，阶段 8 的四项必需检查全部成功，合并提交为 `bf6f59b67bff62ded208ed977e642177450cf7c2`。
- 新增不含凭据的 `forgeflow.acceptance-plan/v1` 模板，固定 11 个串行门禁、负责人角色、超时、依赖和私有 Evidence 路径。
- 新增默认只读的阶段 9 预检，统一调用阶段 5～8 工程契约校验，并能校验填充计划必须位于 Git 忽略目录、无占位符、绑定当前干净 HEAD。
- 新增集中验收窗口手册和公开脱敏摘要模板。
- 静态预检已通过；未读取 Key，未联系模型 Provider、Registry 或 Staging，未执行付费 Eval、Promotion、部署、Tag 或 Release。

## 本地验证

- `scripts/stage-9-acceptance-preflight.ps1` 完整静态模式：通过。
- `scripts/stage-9-acceptance-preflight.ps1 -SkipEngineeringValidators` CI 去重模式：通过。
- PowerShell AST 语法解析：通过。
- `scripts/verify.ps1`：Go 全包测试、Vet、构建、前端类型检查、11 个 Vitest 测试和生产构建通过。
- `git diff --check`、冲突标记与凭据模式扫描：提交前执行并保持通过。

## 当前阻断

1. 本批阶段 9 预检资产尚待人工提交和 PR 合并；最终候选 SHA 尚未形成。
2. `developer/v4` 旧 smoke 的补丁预检失败尚未由新 SHA smoke 解除。
3. 费用/数据授权记录、独立评审人、值班、Registry、Staging 和 Production 私有配置尚未填入本地验收计划。
4. `P8-001`～`P8-007` 仍全部是 Production Go 阻断项。

## 下一人工节点

仓库所有者审核并合并本批 PR。四项必需检查成功后，从新的 `main` 合并 SHA 建立 `.forgeflow/acceptance/1.0.0/plan.json` 并运行填充计划预检。只有预检通过且当次付费/数据范围授权有效时，才能运行候选 smoke。
