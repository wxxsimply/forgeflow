# ForgeFlow Production 值班、变更与发布治理

> 状态：阶段 8 流程基线；人员联系方式、云账号和环境地址只保存在私有运维系统
> Production 变更必须同时满足技术门禁和相应角色的人工批准

## 1. 私有值班登记

上线前在私有系统创建以下记录，仓库只保存记录 ID/状态，不保存电话、邮箱、SSH 地址或云账号：

| 职责 | Primary | Secondary | 覆盖时段 | 升级时限 | 私有记录 ID |
|---|---|---|---|---:|---|
| Service on-call | 待指派 | 待指派 | 待批准 | P1 10 分钟 | 待回填 |
| Platform on-call | 待指派 | 待指派 | 待批准 | P1 10 分钟 | 待回填 |
| Security on-call | 待指派 | 待指派 | 待批准 | P0 立即 | 待回填 |
| Data on-call | 待指派 | 待指派 | 待批准 | 数据事故 10 分钟 | 待回填 |
| Cloud/Provider escalation | 待指派 | 待指派 | 供应商承诺 | 按合同 | 待回填 |
| Release Approver | 待指派 | 待指派 | 发布窗口 | Go/No-Go 前 | 待回填 |

单人无法提供 Primary/Secondary 和独立安全审批时，不开放 Production 用户流量。可以把 Source Release 与 Production 服务审批分开，不能以“个人项目”删除安全门禁。

## 2. 事故级别与升级

| 级别 | 判断 | 首次响应 | 必要动作 |
|---|---|---:|---|
| P0 | 活跃跨租户、可利用 Secret 泄漏、执行面逃逸、大范围数据破坏 | 立即 | 冻结发布和新 Run、隔离、撤销凭据、Incident Commander、Security/Data Owner |
| P1 | Production 不可用、持续数据错误、队列失控、恢复/回滚失败 | ≤10 分钟 | 冻结发布、保护证据、流量控制、评估回滚/恢复 |
| P2 | 部分功能退化、SLO 快速燃烧但有绕行 | ≤4 小时 | 指派 Owner、限制变更、进入修复窗口 |
| P3 | 无当前用户影响的缺陷或改进 | 下一工作日 | 正常 Issue/PR 流程 |

P0/P1 按 `docs/operations.md` 和 `docs/incident-review-template.md` 执行。临时权限/绕过必须有 Owner、到期时间和撤销验证；P0/P1 复盘及高优先级行动完成前不恢复普通发布。

## 3. 变更等级

| 等级 | 示例 | 必需审批 | 验证与回滚 |
|---|---|---|---|
| Standard | 已批准的无状态配置、文档 | Service Owner；必需 CI | 预定义回滚，发布记录 |
| Normal | API/Web 功能、依赖、一般镜像 | 非作者 Reviewer + Service Owner | 测试、Staging、digest、观察窗口 |
| High risk | Auth/RBAC、Prompt/model、Policy/Tool、Sandbox、Migration、网络、Secret、数据保留 | 对应 Security/Data/Platform Owner + Release Approver | 独立测试、Eval/安全证据、drain、明确 rollback |
| Emergency | 仅 P0/P1 缓解 | Incident Commander + 对应 Owner | 最小改动、实时审计、24 小时内补 PR/复核 |

作者不得独自批准 High risk。任何变更都不能在同一动作中同时扩大权限并降低监控。Migration 只允许向前兼容；数据库恢复与应用回滚是两个独立审批动作。

## 4. 发布 Go/No-Go

发布负责人按以下顺序核对，任一步失败即停止：

1. 冻结 Git SHA、Prompt/model/Policy/Tool、Fixture/Grader 和 Migration。
2. 正式候选 Eval 和人工 Promotion 决策通过。
3. 五类镜像从同一 SHA 构建，SBOM/provenance/签名/扫描通过，并由负责人手动上传。
4. digest-only Staging 完整 E2E、安全、告警、恢复、回滚、负载和 Demo 通过。
5. SLO/RPO/RTO/容量实测回填；阶段 8 风险登记无 Open Critical/未批准 High。
6. 数据 Region、子处理者、隐私/条款和支持/值班登记批准。
7. Release Approver 在 `release-reports/v1.0.0-go-no-go-template.md` 签署 `GO`；其他值一律视为 No-Go。
8. 仅在 GO 后，由仓库所有者手动创建签名 Tag 和 GitHub Release。

PR 合并、CI 绿色、模型 Gate 或本文件本身都不等同于最终 GO。

## 5. Production 发布步骤

- 发布前 24 小时公布窗口、Owner、回滚版本和变更冻结范围。
- 对 Worker 停止新 claim 并 drain；确认 Queue、租约和 worktree 状态。
- 先执行数据库向前兼容检查/Migration，再滚动 API/Web；Worker 仅在 Active Release 与镜像完全一致后启动。
- 逐步恢复流量：内部探测 → 运维账号 → 小比例 → 全量；每一步观察错误预算、Queue、费用和安全告警。
- 写入不可变 deployment record、manifest/hash、操作者、时间和健康结果。
- 观察窗口未结束前保留发布人员在线，禁止并行高风险变更。

## 6. 回滚和恢复决策

应用回滚触发条件包括：持续 5xx、Readiness/Active Release 不匹配、Queue/租约异常、SLO 快速燃烧或安全门禁失败。按阶段 7 脚本回到旧 digest 镜像，先做 `db check`，绝不自动执行 Down Migration。

若旧应用不兼容当前 Schema，停止应用回滚，进入事故流程。数据库恢复必须创建新实例/数据库和 Artifact 前缀，核对 checksum、Migration、删除清单、租户边界和 E2E 后再切换。不得覆盖在线数据库来追求较短 RTO。

安全事故优先隔离和凭据撤销；证据可能被污染时，普通应用回滚不能作为唯一处置。

## 7. 访问和职责分离

- GitHub、Registry、云 Production、Secret、数据库和审计后端分别授权，不使用一个长期管理员 Token。
- CI 仅用短期 OIDC 或环境审批获取发布权限；Pull Request 工作流无 Production 写权限。
- 日常 on-call 可以观察、drain 和执行批准 Runbook，但修改 IAM/KMS、删除数据、风险接受和最终发布需要额外批准。
- 每季度复核权限；离岗或职责变化立即撤销。Break-glass 每次使用都创建 P1/P0 级审计。

## 8. 阶段 9 私有登记门禁

以下字段可留在私有系统，但必须在 Go/No-Go 模板中填写不敏感记录 ID：

- Primary/Secondary/Security/Data on-call 与覆盖时段。
- Cloud、Database、Registry、Model Provider 的升级路径。
- Production 账号/Region/IaC 变更审批。
- 隐私/条款法务批准与子处理者登记。
- 独立安全评审、恢复演练和容量测试记录。

任一 ID 缺失或过期时，Release Approver 必须签署 No-Go。
