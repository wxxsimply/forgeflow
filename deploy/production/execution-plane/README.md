# ForgeFlow Production 执行面部署契约

状态：可审查的工程基础，尚未部署；P8-002 仍为 Critical / Open。

本目录定义控制面、执行面、Sandbox daemon、工作负载身份、Secret Manager CSI 挂载和默认拒绝网络的最小 Kubernetes 契约。它是为了使私人 IaC 的实现能被逐项审查，不能直接作为 Production 配置应用。

## 使用边界

execution-plane.template.yaml 是不可直接部署的模板：

- PRIVATE_ 镜像和 StorageClass 标记必须仅由私有 IaC 在经批准的 Release manifest 基础上替换；替换后只能使用 @sha256: digest，不能使用 tag。
- 云厂商工作负载身份绑定、SecretProviderClass 的 provider 参数、Secret Manager 路径、KMS、VPC、Region、域名和 egress allowlist 属于私有基础设施记录，绝不提交到此仓库。
- Platform Owner 必须确认集群的 CNI 实际执行 NetworkPolicy；不支持执行的集群不得进入 Staging。
- 模板通过仓库校验只表示工程契约完整，不表示身份、Secret、网络、节点或镜像已经部署、批准或验收。

私有渲染产物和连通性证据放在 Git 忽略的 .forgeflow/ 或批准的运维证据库。不得把 SecretProviderClass 的真实参数、客户端证书、私有 endpoint、kubeconfig、日志或测试输出提交到 Git。

控制面命名空间使用 Kubernetes Restricted enforce。执行面不能使用同一 enforce 级别，因为 Sandbox daemon 是唯一必要的特权例外；私有 IaC 必须以 CEL/Kyverno/等效 admission policy 仅允许名为 sandbox-daemon 的已批准 digest 使用 privileged，且拒绝所有 hostNetwork、hostPID、hostIPC、hostPath 和 host Docker socket。其余执行面 Pod 必须符合 Restricted。

## 强制拓扑

| 平面 | 身份 / 节点 | 允许能力 | 明确禁止 |
|---|---|---|---|
| 控制面 API | control-api；forgeflow.io/plane=control | 私网 PostgreSQL、其最小 Secret scope | 模型 Key、仓库写入、Docker endpoint、执行 PVC |
| 执行 Worker | execution-worker；专用 taint 节点 | claim Job、受限 workspace、模型 Secret、mTLS 到 daemon、经 egress gateway 的批准出口 | 公网入站、控制面身份、宿主 Docker socket、直接任意出网 |
| Sandbox daemon | sandbox-daemon；专用 sandbox taint 节点 | 仅为 Worker 提供 TLS 2376、启动固定 digest 的临时容器 | Host network/PID/IPC、hostPath、host Docker socket、控制面 Secret |
| Sandbox 子容器 | daemon 创建；每任务临时 | 已批准镜像、受限 workspace、CPU/内存/PID/超时 | 网络、root、可写根文件系统、capability、持久化 |

当前 Docker adapter 已在代码层固定子容器的 --network none、--read-only、--cap-drop ALL、非 root、no-new-privileges 和资源上限。daemon 的 privileged: true 是唯一被允许的例外；它必须在专用节点上运行，且模板中没有 docker.sock、hostPath、hostNetwork、hostPID 或 hostIPC。

execution-workspaces 必须是加密的、仅执行面可挂载的 ReadWriteMany 存储，并依照每个 Run 的生命周期清理。它不是 Artifact 后端，也不能挂载到 API；Artifact 仍使用受控对象存储。

## 工作负载身份与 Secret Manager

每一个 ServiceAccount 都是独立的工作负载身份锚点。私有 IaC 必须为它们映射不同的云角色，最小范围如下：

- control-api：控制面数据库角色、API 必需的 MFA/审计 Secret 和授权的 Artifact 读取；不得读取 Provider Key、Sandbox TLS 或 workspace。
- execution-worker：执行面数据库角色、模型 Provider Key、Worker runtime 配置和 Sandbox 客户端 TLS；不得读取 API MFA/Session Secret 或审计写入身份。
- sandbox-daemon：仅 Sandbox 服务端 TLS 和拉取已批准 Sandbox 镜像的身份；不得读取数据库、Provider 或 API Secret。

Secret 必须使用 secrets-store.csi.k8s.io 只读挂载。代码读取的是 *_FILE 路径；模板没有 secretKeyRef、明文 Secret、ConfigMap Secret 或把 Secret 写入环境变量的用法。证书轮换必须先在非生产环境验证新旧 client/CA 的重叠窗口，再撤销旧证书，并记录私有 Evidence。

## 网络执行规则

默认拒绝是基础规则，唯一允许的业务流是：

1. API → 数据面 TCP 5432（TLS）。
2. Worker → 数据面 TCP 5432（TLS）。
3. Worker → Sandbox daemon TCP 2376（双向 TLS）。
4. Worker 和 daemon → 受批准 egress gateway TCP 8443；gateway 在仓库外按批准的模型、Registry、对象存储与监控目的地实施 FQDN allowlist 和审计。
5. 控制面与执行面的 Pod → kube-dns TCP/UDP 53；仅为解析受前述策略约束的内部服务。
6. approved ingress gateway → API TCP 8080；边缘以外没有 API 入站规则。

API 不允许连接 Worker、Sandbox daemon 或 egress gateway。Sandbox 子容器由 Docker --network none 进一步隔离；即使 daemon 的 egress policy 出现错误，子容器也不能得到网络。

## 部署前与验收

1. Platform Owner 在私有 IaC 中替换所有 PRIVATE_ 标记，绑定批准的五类镜像 digest，并由独立 Security Owner 审阅差异。
2. 验证 CNI、节点 taint/label、Pod Security、ReadWriteMany 加密、egress gateway、Managed PostgreSQL TLS、Secret Manager CSI 和三份最小 IAM policy 均生效。
3. 使用 network-test-contract.md 执行允许和拒绝测试，记录 UTC、候选 SHA、命令摘要、退出码、对象、审阅人和脱敏 Evidence SHA-256。
4. 验证 API/Worker/daemon 的挂载与身份范围，轮换一项非生产 TLS Secret，确认旧凭据失效，随后执行 Worker drain 与 Sandbox 清理。
5. 只有真实 Staging 证据经独立复核后，P8-002 才能从 Open 更新为 Verified Closed；Critical 风险不能 Risk Accepted。
