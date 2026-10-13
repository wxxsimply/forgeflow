# 受控真实开发：代码准备，不代表上线许可

当前公网个人预览继续保持 `planning` / `mock`、不调用模型、不执行仓库代码。本文件对应一个**未部署**的独立验收环境，不能把 `compose.controlled.yaml` 叠加到公网个人预览或当前 1.6 GB、与其他项目共用的服务器上。

## 本次代码边界

- `controlled_development` 仅由 Worker 使用；API 仍按 `planning` 启动，但启用 `FORGEFLOW_CONTROLLED_DEVELOPMENT_API=true`，只允许指定、状态正常的管理员对精确匹配的 `/repositories/demo` 创建任务。普通注册用户不能借此触发付费执行。
- API 不接收模型密钥、Docker 地址或 Docker 证书；只有 Worker 通过私有 Secret 文件取得模型密钥。Worker 在继续每个任务前重新核对数据库中的管理员身份、仓库路径与任务预算。
- 每个新任务固定 1 次迭代、4 次模型调用（规划、开发、代码审查、安全审查），不自动修复；模型 HTTP 重试数为 0。失败不会为了凑结果而自动扩大预算。
- 模型请求仅允许 `deepseek-flash`，单次请求提示内容合计最多 128 KiB，输出上限 16,000 tokens；每次调用前通过 DeepSeek `/user/balance` 查询余额。余额查询失败、余额不是仅人民币或余额超过 5 元时拒绝模型调用。
- 账单仍由 DeepSeek 决定。`MaxEstimatedCostUSD` 和请求大小只是应用侧的次级保护，**不等同于单次 2 元、每日 5 元的可审计硬账单限额**。使用前必须有仅供此环境使用、无自动充值、总可用余额不超过 5 元的独立账号；资金可能在第三方计费后才准确反映。不能把现有高余额密钥直接放入此环境。

## 尚未满足的上线条件

1. 独立、资源充足的执行主机或节点，隔离于现有公网预览和其他项目；验证 DIND/mTLS、无外网测试 Sandbox、只读根文件系统、资源上限和故意越界时的拒绝。当前主机检查时总内存约 1.6 GB、可用约 518 MB，不作为执行节点。
2. 独立 DeepSeek 低余额账号与私有密钥文件、关闭自动充值，并在真正调用前复核[官方模型标识和价格](https://api-docs.deepseek.com/quick_start/pricing/)。密钥不得进入 Git、聊天、Compose 环境文本或 API 容器。
3. 独立数据库及验收域名，确认只有指定管理员能创建真实任务。不要将既有模拟任务队列交给真实开发 Worker；旧的 `development` 模式不受本受控策略保护。
4. 在独立环境先通过无密钥配置检查、镜像摘要核对、数据库备份/恢复、完整 CI；随后才单独批准一次最多 4 调用的付费验收，人工对账并检查源码仓库未被直接改写。
5. 真正向普通注册用户开放真实执行前，还需要每用户配额、可靠的按日账单记账、执行面扩容和针对不可信仓库的隔离验收；本次工作没有宣称完成这些能力。

## 离线验证

`go test ./internal/controlleddev ./internal/config ./internal/httpapi ./cmd/forgeflow-api ./cmd/forgeflow-worker` 和 `./scripts/verify.ps1` 不需要模型密钥，也不会调用 DeepSeek。PostgreSQL 集成测试须按 CI 的测试数据库配置执行；未提供测试库时本地对应测试会跳过，不能把跳过写成通过。

未来独立环境的 Compose 文件顺序为 `deploy/staging/compose.yaml`、`deploy/staging/compose.openai.yaml`、`deploy/staging/compose.controlled.yaml`。此处仅记录配置契约，不是发布命令；禁止在当前公网服务器上执行。
