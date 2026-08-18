# ForgeFlow Threat Model

版本：2026-08-11；适用范围：Staging Compose 与后续生产控制面/执行面。

## 资产与安全目标

核心资产包括用户 Session、密码 Hash、模型 API Key、仓库内容、Patch/测试证据、审批决策、审计事件、PostgreSQL 数据和备份。安全目标是：租户隔离；原仓库工作文件不被直接修改；未经批准不执行高风险副作用；Secret 不进入模型上下文、日志、镜像或 Git；确定性策略不能被 Prompt 或模型结果覆盖。

## 信任边界

1. 浏览器与公网 Caddy：仅 HTTPS 443；所有输入不可信。
2. Caddy 与 API/Web：内部 `app` 网络；仅 API/Web 可达。
3. API 与 PostgreSQL：控制面边界；API 无模型 Key、无 Docker 能力。
4. Worker 与仓库/workspace：执行面边界；Worker 可读模型 Key并管理受限 worktree。
5. Sandbox：默认无网络、非 root、只读根文件系统、资源上限；其输出不可信。
6. OpenAI/告警 webhook：受控外部出口；仅 Worker/Alertmanager可出网。
7. 备份介质：离线安全边界；必须完整性校验、加密和访问审计。

## 主要威胁与控制

| 威胁 | 影响 | 主要控制 | 验证 |
|---|---|---|---|
| 路径穿越、符号链接逃逸 | 读取/覆盖宿主文件 | 规范化相对路径、受管根目录、链接边界检查 | Repository/Tool 安全测试 |
| 命令注入 | Worker 或宿主执行任意命令 | 参数数组、无 Shell 解释、命令白名单、危险字符拒绝 | Policy/Sandbox 测试 |
| Prompt Injection | 绕过策略或泄露 Secret | 仓库内容标记为不可信；Policy/预算/审批在模型外强制 | Planner/Developer/Grader 测试 |
| 跨租户 IDOR | 读取他人 Run/Artifact | owner 过滤、RBAC、不可猜 UUID | HTTP/PostgreSQL 越权 E2E |
| Session 窃取/CSRF | 冒充用户执行操作 | Secure/HttpOnly/SameSite Cookie、CSRF Token、Session Hash/撤销 | Auth 测试与 HTTPS 检查 |
| 模型 Key 泄漏 | 外部滥用和费用损失 | Key 只以 Secret 挂载 Worker；API 启动时拒绝 Key；日志脱敏 | Preflight 与配置测试 |
| Docker 控制面被利用 | 宿主接管 | API 永不挂 Docker Socket；默认禁用 Sandbox；生产 Worker 使用专用主机 | Compose Preflight、主机隔离演练 |
| Queue 重放/重复副作用 | 多次执行 Patch/工具 | 租约、Heartbeat、乐观锁、幂等键、审批证据绑定 | Worker 并发/故障测试 |
| Artifact/备份篡改 | 错误恢复、伪造证据 | SHA-256、受管 storage key、restore 前校验、只追加审计 | 备份恢复演练 |
| 供应链镜像投毒 | 运行恶意依赖 | CI 锁定依赖；生产镜像必须 digest；SBOM/漏洞扫描后 Promotion | 发布 Preflight/安全审查 |
| SSRF/元数据访问 | 内网凭据泄漏 | API 无出口；Sandbox 无网络；Worker Base URL 由运维配置 | 网络策略演练 |
| 告警 webhook 泄漏 | 事故通道被滥用 | `url_file` Secret，仅 Alertmanager 可读；告警不含任务正文 | Compose 检查 |
| 资源耗尽 | 队列阻塞或费用失控 | Run/Token/Tool/Diff/容器预算、限速、队列告警 | 预算与负载测试 |

## 残余风险与生产阻断项

- Compose Worker 仍与控制面同机，不构成最终生产隔离；生产必须迁移到专用 Worker 主机。
- 当前 Artifact 使用本地 Volume，未实现跨区对象存储复制。
- OTel Collector 默认只输出基础 debug traces，正式环境需接入受控 Trace Backend。
- 尚无真实 30 Case 三基线报告、邀请制 Beta、宿主入侵演练和 SBOM/镜像漏洞扫描结果。
- PostgreSQL 容器内网络未启用 TLS；其风险依赖 Docker 私有网络和单机边界，跨主机生产必须启用 TLS。

上述阻断项完成前，不批准 Production。Threat Model 在新增 Tool、网络出口、认证方式、存储后端或部署拓扑时必须重审。
