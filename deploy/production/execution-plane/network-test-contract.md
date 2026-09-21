# 执行面网络与身份验收契约

状态：阶段二真实 Staging 的人工测试清单；本文件本身不构成测试通过或 Production 批准。

所有测试都使用专用 fixture、测试账号和临时证书。记录候选完整 Git SHA、私有渲染 manifest 的 SHA-256、UTC、操作者、独立审阅人、命令摘要、退出码及脱敏 Evidence SHA-256；不得提交 kubeconfig、Pod 日志、endpoint、证书、Provider Key 或数据库 DSN。

## 必须成功

| 编号 | 源 → 目标 | 预期 |
|---|---|---|
| P8-002-P01 | Worker → Sandbox daemon TCP 2376 | 仅使用当前 CSI 挂载的客户端证书可完成 mTLS Docker health/info；过期或错误证书被拒绝。 |
| P8-002-P02 | Worker → Managed PostgreSQL TCP 5432 | 独立执行面 DB 角色经 TLS 可 claim/heartbeat Job，不能执行 API 的用户或 Session 管理操作。 |
| P8-002-P03 | Worker → approved egress gateway TCP 8443 | gateway 审计到已批准的模型/Registry/Object Store 目的地；Worker 不直接访问外网。 |
| P8-002-P04 | API → Managed PostgreSQL TCP 5432 | 控制面 DB 角色经 TLS 完成最小 API 操作，不能使用执行面角色。 |
| P8-002-P05 | Worker 创建 Sandbox | docker inspect 显示固定 digest、NetworkMode=none、非 root、只读根、capabilities dropped、资源上限和任务结束后清理。 |

## 必须拒绝

| 编号 | 源 → 目标 | 预期 |
|---|---|---|
| P8-002-N01 | API → Sandbox daemon TCP 2376 | 超时或拒绝；API Pod 无客户端 TLS 目录，也没有 Docker endpoint 配置。 |
| P8-002-N02 | API → 模型 Provider / egress gateway | NetworkPolicy 拒绝；API ServiceAccount 不具备 Provider Secret 或 egress IAM。 |
| P8-002-N03 | Worker → 未批准外部域名、云 metadata、内网 CIDR | gateway 或 NetworkPolicy 拒绝并留存脱敏拒绝记录。 |
| P8-002-N04 | Sandbox 子容器 → DNS、外网、metadata 或数据面 | 因 Docker --network none 失败；不得把失败改成 allowlist。 |
| P8-002-N05 | 任意控制面 Pod → execution workspace PVC、模型 Key、Sandbox 服务端 TLS | Kubernetes 挂载和 Secret Manager/IAM 均拒绝。 |
| P8-002-N06 | 任意执行面 Pod → API MFA/Session Secret、审计写入身份 | Secret Manager/IAM 均拒绝。 |
| P8-002-N07 | Sandbox daemon → host Docker socket、hostNetwork、hostPID、hostIPC、hostPath | Pod spec、admission 和节点审计均拒绝；daemon 的特权仅限专用执行节点内的 DinD。 |

任何允许项失败、拒绝项成功、TLS 旁路、跨平面 Secret 可读、Sandbox 具有网络、或镜像不是批准 digest 时立即停止 Staging 验收，保持 P8-002=Open，修复必须回到阶段一重新 Freeze。
