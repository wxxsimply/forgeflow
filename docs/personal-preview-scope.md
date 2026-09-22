# ForgeFlow 个人预览边界

> 状态：PERSONAL-001 已完成
>
> 生效日期：2026-09-22
>
> 适用对象：个人维护的受限公开预览，不是生产级托管服务

## 1. 用户范围

- 同时最多 5 个活跃测试账号，包括项目所有者。
- 新测试者必须由项目所有者明确邀请并单独创建账号；公网登录页可访问不等于开放注册。
- 真实姓名、邮箱和登录凭据只保存在私有系统，不写入 Git、Issue、PR 或公开日志。

## 2. 仓库与数据范围

- 仅允许项目所有者事先检查并复制到专用预览目录的合成仓库或公开演示仓库。
- 当前允许根目录由个人预览部署配置绑定，API 只读访问仓库，Worker 仅在 Mock/Planning 模式下工作。
- 禁止生产密钥、密码、访问令牌、客户数据、个人敏感信息、受保密协议约束的代码和来源不明的仓库。
- 在 PERSONAL-008 完成前，不允许任意用户提交仓库地址，也不运行不可信代码。

## 3. 模型和费用边界

部署中的个人预览固定使用：

- FORGEFLOW_WORKFLOW_MODE=planning
- FORGEFLOW_PLANNER_MODE=mock
- FORGEFLOW_DOCKER_ENABLED=false
- 服务器不保存 DeepSeek 或 OpenAI Key
- 运行时外部模型调用上限：0 次
- 运行时模型费用上限：单次 0 USD、每日 0 USD、本轮总计 0 USD

历史上独立完成的本地付费演练不授权服务器再次调用模型。未来如需真实模型任务，必须创建独立任务，重新确认允许发送的数据、模型、当次调用次数和费用上限；不得复用本记录作为付费授权。

## 4. 版本与部署边界

- 每次预览部署都必须绑定 main 上已通过必需检查的 40 位 Git SHA。
- 部署、DNS、安全组、Secret、数据库恢复和回滚均需项目所有者单独执行或明确授权。
- 候选 SHA、部署时间和回滚版本保存在私有发布记录；公开仓库只保存不含身份、凭据和内部日志的边界摘要。

## 5. 停止条件

出现以下任一情况立即停止新任务，必要时关闭入口或回滚：

- 发现越权访问、凭据泄漏、跨用户数据读取或日志包含 Secret。
- 实际运行模式不再是 Planning/Mock，Docker 执行被开启，或服务器出现模型 Key。
- 仓库不在已检查的专用预览目录内，或包含禁止数据。
- HTTPS、登录、注销、健康检查、数据库或回滚能力异常。
- 出现未经授权的模型调用、未知调用结果或任何非零模型费用。
- 部署版本无法对应已通过 GitHub 必需检查的完整 Git SHA。

## 6. 可复核证据

- FORGEFLOW_PERSONAL_PREVIEW_PLAN.md：个人预览部署范围和历史验收。
- docs/preview-rollout-result.md：实际部署与人工验收记录。
- deploy/personal-preview/compose.yaml：Planning、Mock、Docker 关闭和仓库挂载契约。
- scripts/preview_rollout_preflight.py：真实运行时的只读边界检查。
- scripts/validate-personal-preview-scope.ps1：仓库级边界防漂移检查。

本记录关闭 PERSONAL-001。它不关闭 HTTPS、Secret、备份恢复、可观测性、候选 smoke 或不可信执行边界等后续工作项。
