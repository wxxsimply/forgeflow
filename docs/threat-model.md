# ForgeFlow Threat Model

版本：2026-09-08；适用范围：Staging Compose 与阶段 8 Production 参考控制面、数据面和执行面。

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
| Artifact/备份篡改 | 错误恢复、伪造证据 | SHA-256、受管 storage key、restore 前校验、对象版本和只追加审计 | 备份恢复与跨存储一致性演练 |
| 供应链镜像投毒 | 运行恶意依赖 | CI 锁定依赖；生产镜像必须 digest；SBOM/漏洞扫描后 Promotion | 发布 Preflight/安全审查 |
| SSRF/元数据访问 | 内网凭据泄漏 | API 无出口；Sandbox 无网络；Worker Base URL 由运维配置 | 网络策略演练 |
| 告警 webhook 泄漏 | 事故通道被滥用 | `url_file` Secret，仅 Alertmanager 可读；告警不含任务正文 | Compose 检查 |
| 资源耗尽 | 队列阻塞或费用失控 | Run/Token/Tool/Diff/容器预算、限速、队列告警 | 预算与负载测试 |
| 管理员账号接管 | 数据导出/删除、Promotion 或 IAM 被滥用 | 上游 IAP 或应用 MFA、最小权限、短 Session、Break-glass 告警 | 管理员 MFA 与撤销演练 |
| 审计删除或抵赖 | 无法确定操作者和事故范围 | 外部 append-only 后端、工作负载身份、时间同步、删除保护 | 篡改/查询恢复演练 |
| 删除/导出越权 | 数据泄漏或跨租户删除 | owner scope、幂等 Job、双人确认、短时下载、删除清单 | 导出/删除 IDOR 与恢复测试 |
| 数据跨境或子处理者漂移 | 违反用户告知、合同或数据驻留 | Region allowlist、子处理者登记、配置门禁和版本化隐私政策 | 配置与登记审计 |

## 残余风险与生产阻断项

- `P8-001`：当前 Artifact 只有本地 FileStore，Production 对象存储适配器、迁移和一致性验证尚未完成。
- `P8-002`：Compose Worker 仍与控制面同机；Production 专用 Worker/Sandbox 节点池和网络策略尚未部署。
- `P8-003`：应用没有内建管理员 MFA；上线前必须由批准的 IAP 强制 MFA 或实现应用 MFA。
- `P8-004`：OTel/审计仍未接入外部 append-only、删除保护的 Production 后端。
- `P8-005`：云厂商、Region、数据驻留和子处理者尚未批准。
- `P8-006`：阶段 3 三基线已完成并获签署，但候选正式 Eval、完整镜像证据、Production 负载/RPO/RTO 和故障切换仍未执行。
- `P8-007`：独立安全评审人和私有值班表尚未登记。
- PostgreSQL 容器内网络未启用 TLS；其风险仅限单机 Staging，跨主机 Production 必须使用受管 TLS 数据库。

上述阻断项完成前，不批准 Production。详细 Owner 和关闭条件见 `docs/production-security-readiness.md`；逻辑边界见 `docs/production-architecture.md`。Threat Model 在新增 Tool、网络出口、认证方式、存储后端、数据 Region 或部署拓扑时必须重审。
