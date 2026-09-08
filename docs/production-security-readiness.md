# ForgeFlow Production 安全评审与风险登记

> 状态：阶段 8 安全评审范围与阻断流程已固定；真实独立评审和攻击面证据在阶段 9 完成
> 原则：任何 Open Critical、未批准 High 或跨租户/Secret 风险都直接 No-Go

## 1. 评审角色与独立性

| 角色 | 职责 | 独立性要求 |
|---|---|---|
| Security Owner | Threat Model、WAF/MFA、Secret、漏洞与攻击面门禁 | 不得是被评审安全敏感变更的唯一作者 |
| Service Owner | API/Auth/RBAC/审批、可用性和修复交付 | 不得单独接受自身引入的 High 风险 |
| Platform Owner | 网络、集群、Worker/Sandbox、镜像和审计后端 | Production 权限与日常开发权限分离 |
| Data Owner | 数据分类、Region、保留/删除/导出、PITR 和对象存储 | 必须批准数据位置和子处理者 |
| Release Approver | 汇总证据并签署最终 Go/No-Go | 不得在必需证据缺失时使用口头例外 |
| Incident Commander | P0/P1 冻结发布、隔离和恢复协调 | 事故期间拥有明确但限时的操作授权 |

真实姓名、联系方式、轮值和云厂商升级路径保存在私有运维系统，不提交 Git。单维护者阶段无法满足独立性时，Production 保持阻断；可以继续 Source Release 或受限 Staging，但不能伪造第二名审核人。

## 2. 独立评审范围

阶段 9 安全评审至少覆盖：

1. Internet edge、TLS、WAF/DDoS、CORS、CSP/HSTS、Cookie、CSRF、Session、限速和管理员 MFA。
2. API/Worker/PostgreSQL/Object Store 的租户边界、IDOR、工作负载身份和最小数据库权限。
3. Prompt Injection、模型输入最小化、Provider 数据发送告知、费用与 Token 预算。
4. Worktree 路径、符号链接、Patch、命令白名单、Sandbox 无网络、资源限制和宿主逃逸。
5. 镜像 digest、SBOM、provenance、签名、依赖投毒、构建身份和 Registry 权限。
6. Secret Manager、轮换/撤销、日志脱敏、审计防篡改、备份与恢复。
7. Promotion/rollback、Migration 前向兼容、Worker drain、旧审批/Checkpoint 不可复用。
8. 数据保留、删除、导出、事故通知和第三方/子处理者清单。

测试使用专用 fixture 和测试账号。不得把 Private Grader、隐藏测试、真实用户仓库、原始 Evidence 或凭据发送给外部扫描器。

## 3. Production 必需控制

### 身份与入口

- TLS 1.2+、自动续期、HSTS、CSP、请求大小限制和边缘限速。
- 管理员/Operator 必须使用 MFA。当前应用没有内建 MFA，因此上线前必须由经批准的 Identity-Aware Proxy 强制管理员 MFA，或实现并独立评审应用 MFA。
- Production 管理入口与普通用户入口分离策略；Break-glass 账号离线保管、每次使用告警、24 小时内复核。
- Session 绝对/空闲期限、Secure/HttpOnly/SameSite、CSRF 和撤销保持强制；禁止在 URL 或日志中传 Token。

### 工作负载与网络

- API 和 Worker 使用不同工作负载身份、节点池、数据库角色和 Secret scope。
- API 无模型 Key、仓库写权限或 Docker endpoint；Sandbox 无公网；Worker 无公网入站。
- Worker 的模型出口只能经 allowlist egress；云 metadata、内网 CIDR 和未批准域名默认拒绝。
- NetworkPolicy/Security Group 由 `production-architecture.md` 的矩阵生成，并执行正/负连通性测试。

### 数据、Secret 与审计

- PostgreSQL/Object Store/Backup 使用独立 KMS 范围、TLS 和访问日志；Production 与 Staging Key/账号完全分离。
- Secret 通过工作负载身份动态获取或只读挂载；不进入环境转储、镜像层、Git、Issue、聊天或普通日志。
- 管理员变更、审批、Promotion/rollback、用户/Session、Artifact 读取、Secret/权限变更写入外部 append-only 审计后端。
- 日志默认不记录任务正文、源码、Patch、Cookie、Authorization、模型 Key、DSN 或 webhook；诊断采样也不能绕过。

### 供应链

- 仅部署同一批准 Git SHA 构建的五类 digest 镜像；验证签名身份、provenance、SBOM 和漏洞报告。
- Critical=0、High=0 为默认门禁。可达 High 例外必须有补偿控制、到期时间和非作者批准；Critical 不接受上线例外。
- GitHub/Registry/云账号使用最小权限和 MFA；CI 不持有 Production 长期凭据，优先短期 OIDC。

## 4. 发现与阻断流程

| 严重度 | 示例 | 时限与处置 | Production 决策 |
|---|---|---|---|
| Critical | Sandbox 逃逸、跨租户读取、可利用 Secret 泄漏、签名绕过 | 立即隔离，修复后重新执行受影响全部证据 | 必须 No-Go，不接受风险 |
| High | 管理员无 MFA、可达依赖漏洞、审计可删除、对象存储公网风险 | 发布前修复；仅不可达且有补偿控制时可限时接受 | 默认 No-Go，需独立书面批准 |
| Medium | 有界信息泄漏、纵深控制缺失 | 记录 Owner、截止日期和验证；评估组合风险 | Release Approver 决定 |
| Low | 硬化或可观测性改进 | 纳入计划并跟踪 | 不单独阻断 |

发现状态只允许 `Open`、`Mitigated`、`Risk Accepted`、`Verified Closed`。作者提交修复后必须由独立审核人验证；只有 `Verified Closed` 才算关闭。风险接受记录必须包含资产、可利用性、影响、补偿控制、Owner、到期日和撤销条件。

## 5. 阶段 8 风险登记

| ID | 风险 | 严重度 | 当前控制 | Owner | 阶段 9 关闭条件 | 状态 |
|---|---|---|---|---|---|---|
| P8-001 | Production 对象存储后端尚未接入；当前仅本地 FileStore | High | Staging 仅使用 fixture，Artifact 有 SHA-256 | Data Owner + Service Owner | 实现适配器、迁移/一致性/权限测试通过 | Open |
| P8-002 | 专用 Worker 节点池、Sandbox daemon 和网络策略尚未部署 | Critical | 单机 Staging 仅模拟网络隔离 | Platform Owner | 真实专用执行面与正/负网络测试通过 | Open |
| P8-003 | 管理员 MFA 尚无应用内实现 | High | Session/CSRF/RBAC；可使用上游保护 | Security Owner | IAP 强制 MFA 或应用 MFA 独立评审通过 | Open |
| P8-004 | 外部 append-only 审计和受控 Trace 后端未接入 | High | PostgreSQL 审计、结构化日志 | Security Owner + Platform Owner | 删除保护、访问审计、脱敏和查询恢复验证 | Open |
| P8-005 | 云厂商、Region、数据驻留和子处理者尚未批准 | High | Provider-neutral 边界已定义 | Product Owner + Data Owner | 私有决策记录与用户告知材料批准 | Open |
| P8-006 | Production 负载、RPO/RTO 和故障切换未实测 | High | 目标和测试矩阵已固定 | Platform Owner + Data Owner | 阶段 9 实测达到 `production-slo-capacity.md` | Open |
| P8-007 | 独立安全评审人尚未在私有系统登记 | High | 评审范围与独立性规则已固定 | Release Approver | 指派非作者审核人并签署报告 | Open |

这些 Open 项是诚实的 Production 阻断项，不影响阶段 8 工程文档进入 `待集中验收`，但任何一项未按关闭条件处理都阻止阶段 9 最终 Go。

## 6. 安全评审签署模板

```text
Candidate Git SHA:
Release manifest SHA-256:
Environment / Region:
Independent reviewer:
Review UTC window:
Evidence IDs/hashes:
Critical open: 0 / High open: 0
Risk acceptances and expiry:
Decision: APPROVED FOR GO-NO-GO / REJECTED / CHANGES REQUIRED
Signature or immutable approval record:
```

PR 审查只能批准本阶段的范围、控制与阻断流程；不能替代阶段 9 的真实独立安全签署。
