# 阶段 6：Staging 基础设施与访问控制清单

> 状态：工程清单已评审，真实资源创建与公网验收后置到阶段 9  
> 评审日期：2026-09-08  
> 适用范围：单机 Linux/amd64 Staging，不可直接作为 Production 信任模型

## 1. 评审结论

本清单覆盖主机、DNS、端口、存储、Registry、Secret、备份和访问控制。仓库只保存要求、占位符和验证脚本；真实 IP、账号、联系方式、凭据和恢复介质位置必须保存在团队私有运维系统。阶段 6 只确认部署输入与验证方法完整，不创建长期付费资源，也不声称公网 Staging 已通过。

## 2. 资源清单

| 项目 | 已批准的工程要求 | 阶段 9 证据 |
|---|---|---|
| 主机 | 专用 Linux/amd64 主机；系统和 Docker 使用受支持版本；启用时间同步与安全更新 | 主机版本、补丁日期和脱敏资产编号 |
| Docker | Docker Engine、Compose v2；部署账号可拉取镜像但不共享交互式开发账号，Staging 主机不承担镜像构建 | `docker version` 和 `docker compose version` |
| 源码 | 只从人工审核的 Git tag/40 位 commit 获取；服务器工作区必须干净 | `git rev-parse HEAD`、`git status --short` |
| 域名 | 独立 Staging FQDN；A/AAAA 只指向该主机；禁止复用 Production Cookie 域 | 脱敏 DNS 查询、证书域名和有效期 |
| Registry | 专用 read-only pull token；所有 ForgeFlow 与第三方镜像按 digest 固定 | 脱敏登录方式、Release manifest 和实际 Compose 镜像 |
| 存储 | PostgreSQL、Artifact、Workspace、Caddy、监控和 Sandbox Engine 使用独立命名卷 | `docker volume ls`、容量和挂载核对 |
| 备份 | 数据库 dump、校验和、manifest 与 Artifact 快照时间点对齐，并加密复制到异地目标 | 备份时间、校验和、恢复演练结果 |
| 值班 | Primary/Secondary on-call、安全联系人、数据负责人和云厂商升级路径已登记 | 私有运维记录 ID，不提交联系方式 |

## 3. 网络与端口

- 公网入站只允许 Caddy 的 TCP `80`、TCP `443`；启用 HTTP/3 时才允许 UDP `443`。
- SSH 只允许批准的管理来源、密钥认证和最小权限账号；禁止密码登录和公网管理面板。
- PostgreSQL、API、Worker、Prometheus、Alertmanager、OTLP 和 Docker Engine 不发布宿主端口。
- API 只能访问应用、数据和监控内部网络，不得取得 OpenAI Key、Docker endpoint 或 Sandbox 客户端证书。
- Worker 通过内部 `worker-execution` 网络和 mTLS 访问 `sandbox-engine`；禁止挂载宿主 `docker.sock`。
- Sandbox 任务容器必须无网络、非 root、只读根文件系统、零 capabilities，并受 CPU、内存、PID 和超时限制。
- 阶段 9 保存云防火墙/主机防火墙的脱敏规则和外部端口扫描结果；发现额外公网端口立即停止验收。

## 4. 存储与恢复边界

- `postgres-data`：只供 PostgreSQL 使用；禁止通过宿主公网端口访问。
- `artifacts`：保存报告与大对象；与数据库备份保持同一恢复点。
- `workspaces`：由 Worker 写入，API 只读；原 Fixture 仓库必须在验收前后保持 commit 与状态不变。
- `caddy-data` / `caddy-config`：保存 ACME 状态；备份中不得泄露账户私钥。
- `prometheus-data` / `alertmanager-data`：仅内部运维使用，按批准的保留期清理。
- `sandbox-docker-data` / `sandbox-docker-certs`：只属于隔离执行面；重建或轮换证书时先 drain Worker。
- 数据库恢复只能写入 `forgeflow_restore_*` 隔离数据库；不得覆盖在线数据库。

## 5. Secret 与访问控制矩阵

| Secret/凭据 | 创建位置 | 唯一消费者 | 删除或轮换条件 |
|---|---|---|---|
| PostgreSQL password | Staging 主机 `deploy/staging/secrets` | PostgreSQL | 新角色和 DSN 验证后撤销旧角色 |
| PostgreSQL DSN | 同上 | Migration、API、Worker | 与密码同步轮换 |
| Alert webhook | 同上 | Alertmanager | 新 webhook 测试投递成功后撤销旧值 |
| OpenAI-compatible API key | 同上 | Worker | 新 Key smoke 成功后撤销旧 Key |
| Bootstrap admin password | 同上，首次部署前临时创建 | Bootstrap API overlay | 首次管理员登录验证后立即运行清理脚本删除 |
| Registry pull token | 主机 Docker credential store，仓库外 | Docker Engine | 泄露、人员变更或到期时轮换 |
| SSH private key | 运维人员批准的密钥存储 | SSH 客户端 | 权限变更、设备丢失或到期时吊销 |

所有 Compose Secret 必须是部署账号拥有的普通非符号链接文件，Linux mode 为 `0600`。不得把 Secret 写入 `staging.env`、Release manifest、Evidence、命令历史、Issue、聊天或普通日志。

## 6. 阶段 9 执行顺序

1. 人工冻结并获取已批准 commit/tag，确认工作树干净。
2. 人工完成只读 Registry 登录，准备实际 digest Release manifest。
3. 复制 `staging.env.example`，填写批准 SHA、域名、仓库路径和全部 digest 引用。
4. 创建 PostgreSQL、DSN、Alert 和一次性 Bootstrap Secret，设置所有权与 `0600`。
5. 运行 `staging-preflight.ps1 -Manifest ... -RequireDigests`；失败即停止。
6. 使用同一 manifest 执行 Bootstrap 部署，验证管理员登录后运行 `staging-bootstrap-cleanup.ps1`。
7. 创建模型 Key，使用同一 manifest 启动 Worker/Sandbox；服务器只拉取镜像，不构建镜像。
8. 运行 `staging-acceptance.ps1`，验证 HTTPS、版本、Prompt/model readiness、浏览器全链路和原仓库不变性。
9. 按阶段 7 Runbook 执行告警、备份恢复、安全、回滚和 Demo 演练。
10. 保存脱敏 Evidence；确认 Bootstrap Secret 不存在后才允许进入 Production Go/No-Go。

## 7. 阻断条件

出现以下任一情况立即停止部署或验收：源码 SHA 与 manifest 不一致、工作树不干净、镜像不是 digest、Secret 权限过宽或为符号链接、API 获得模型/Docker 凭据、额外公网端口、Worker Prompt/model release 未 ready、Bootstrap Secret 未删除、Fixture 仓库发生变化、日志或 Evidence 泄露敏感数据。
