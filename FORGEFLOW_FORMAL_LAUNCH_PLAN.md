# ForgeFlow 正式上线执行计划

> 审查日期：2026-09-22
>
> 审查基线：origin/main，8e5082d4ff64115906689576699f302a980550a7
>
> 定位：本计划替代零散的“下一步”描述，用于把现有工程基线推进到可审计的封闭试点和正式发布。它不替代安全评审、法务批准、云账号配置或最终 Go/No-Go。

## 1. 审查结论

ForgeFlow 的主体产品、数据库、Web、治理、对象存储适配器、管理员 MFA、append-only 审计、数据治理门禁和执行面部署契约已经进入 main。最近的执行面 PR 已通过 Go verification、PostgreSQL integration、deployment-assets validate 和 Web verification。

项目现在适合继续进行正式上线准备，但还不适合向真实用户开放 Production 流量。原因不是缺少普通功能页面，而是以下真实环境证据尚未产生：

1. 最终候选尚未 Freeze，新的候选 smoke 也尚未证明补丁协议修复有效。
2. 五类镜像尚未完成真实构建、扫描、签名、上传和 digest 拉取验证。
3. 公网 HTTPS Staging、独立执行面、真实 Secret Manager、对象存储、审计后端和数据驻留尚未验收。
4. 备份恢复、故障切换、容量、负载、安全边界和告警投递没有真实证据。
5. 独立安全评审、值班、Release Approver、用户告知、子处理者和最终法律文本尚未批准。

此外，本次只读核对发现 GitHub main 当前未启用分支保护。尽管 CI 工作流存在，未受保护的 main 允许绕过必需检查，是正式上线前的 P0 治理缺口。

## 2. 上线目标与边界

本计划分为两个可审计目标，不能把前者误称为后者：

| 目标 | 可以做什么 | 不可以做什么 | 必须完成的门禁 |
|---|---|---|---|
| 封闭试点 | 仅允许批准的内部或签约测试用户，使用专用测试数据和受控仓库 | 不对外宣传为 Production Ready；不处理未批准真实用户数据；不跳过安全、数据或恢复证据 | Freeze、候选 smoke、供应链、真实 Staging、核心安全与数据控制通过；每项例外均有书面批准 |
| 正式发布 v1.0.0 | 依据批准 Region 和合同范围接入真实用户，发布签名 Tag 与 GitHub Release | 不存在 Open Critical、未批准 High、未验证数据/恢复/身份边界或缺失签署 | 本计划全部阶段与 11 个阶段 9 门禁均通过；P8-002 必须 Verified Closed，其余 P8 项必须 Verified Closed 或具备政策允许的、未过期的独立风险接受 |

当前状态是工程准备完成、真实验收未开始。任何文档、演示或市场文案在最终 Go 前只能使用“受限预览”或“正式上线准备中”。

## 3. 固定推进规则

1. 每个工作项必须有一个 Owner、一名非作者 Reviewer、一个可复核的证据位置和明确的停止条件。
2. 代码、Prompt、Policy、Tool、Fixture、Grader、Migration、镜像或运行配置改变时，受影响的候选和验收证据立即失效。
3. 付费模型调用、Registry 写入、部署、真实告警投递、恢复、Promotion、rollback、Tag 和 Release 始终需要单独人工确认。
4. 原始模型输出、隐藏测试、Private Grader、真实仓库、凭据、数据库 dump、私有审批和内部网络地址不得提交到 Git。
5. main 只能通过 Pull Request 更新；GitHub 必需检查、至少一名非作者审批、线性历史或受控 merge queue、管理员绕过限制和推送限制必须在 GitHub 中启用。
6. 每个 PR 在提交前必须运行与修改范围匹配的检查；合并前必须保持 Go verification、PostgreSQL integration、validate 和 Web verification 全绿。
7. Freeze 后不再创建功能 PR。发现问题时创建独立修复 PR，合并后生成新候选并从 Freeze 重新开始。

## 4. 当前阻断项

| ID | 阻断项 | 严重度 | 当前事实 | 关闭证据 |
|---|---|---|---|---|
| LAUNCH-001 | main 未受 GitHub 分支保护 | P0 | REST 分支保护查询返回未受保护 | 必需检查、审批、管理员限制和直接推送限制截图或配置导出 |
| LAUNCH-002 | 最终候选 Freeze 未完成 | P0 | 私有输入、候选 SHA、责任人和审批记录未冻结 | 通过的私有 acceptance plan 预检及 Evidence 哈希 |
| LAUNCH-003 | 候选补丁 smoke 未重新执行 | P0 | 2026-09-09 的两个 Observation 在 patch_check 失败 | 新候选上 1 Case x 2 Prompt 的成功或明确 No-Go 记录 |
| P8-001 | 对象存储、导出/删除与恢复未在真实环境验证 | High | 代码和单元测试已存在 | Bucket/IAM/KMS、迁移、恢复、一致性和权限证据 |
| P8-002 | 独立执行面未部署 | Critical | Kubernetes 契约存在，但真实节点、身份和网络不存在 | 专用节点、mTLS、Secret Manager、正反网络与 Sandbox 证据 |
| P8-003 | MFA 和 break-glass 未在真实身份入口验证 | High | 应用内 TOTP 工程基础已存在 | HTTPS、上游 Operator MFA、轮换、旁路拒绝与演练证据 |
| P8-004 | append-only 审计与受控 Trace 未在真实后端验证 | High | 适配器和失败关闭已存在 | Object Lock、KMS、删除拒绝、查询恢复和脱敏证据 |
| P8-005 | Region、子处理者、数据告知与接受未批准 | High | Production 配置门禁已存在 | 私有决策、公开生效告知、接受记录和环境验证 |
| P8-006 | SLO、容量、RPO/RTO、故障切换未实测 | High | 目标与矩阵已定义 | 脱敏负载、恢复、故障切换和容量报告 |
| P8-007 | 独立安全评审和值班未固化 | High | 角色模型已定义 | 私有 on-call、独立 Reviewer 和最终签署记录 |

## 5. 阶段 A：发布治理与候选冻结

目标：建立不能绕过的代码治理，并生成唯一、可追溯的候选。

| 工作项 | Owner | 完成条件 | 停止条件 |
|---|---|---|---|
| LAUNCH-001，保护 main | Repository Owner | 启用 PR 必需、四项必需检查、非作者审批、管理员限制与直接推送限制 | 任一项无法启用时，不进入真实 Staging |
| LAUNCH-002，收集私有 Freeze 输入 | Release Approver | 数据范围、预算、Registry、签名、OIDC、HTTPS Staging、值班、独立安全评审和审批记录均有效 | 缺任何输入、输入过期或候选 SHA 不一致 |
| LAUNCH-003，生成并预检私有计划 | Release Approver | 初始化器在干净 main 绑定完整 SHA，填充计划预检通过 | 占位符、脏工作区、非忽略路径、短 SHA 或计划覆盖 |
| LAUNCH-004，重新运行候选 smoke | Eval Owner + Security Reviewer | developer/v1 与候选 Prompt 均完成 JSON、diff、apply、显式测试和隐藏测试，无协议或基础设施错误 | 费用/数据授权无效、Fixture 变更、补丁协议失败、未知调用结果 |
| LAUNCH-005，决定候选 | Release Approver | 记录 Promotion、保留基线或 No-Go 的签署结论 | 未获签署时不得运行正式 Eval 或构建发布镜像 |

必须人工执行：生成私有计划、付费调用、候选结论签署。仓库只提交脱敏汇总和 Evidence 哈希。

## 6. 阶段 B：正式 Eval 与不可变供应链

目标：证明候选质量，并将同一 SHA 制造成可验证的部署资产。

| 工作项 | Owner | 完成条件 | 停止条件 |
|---|---|---|---|
| LAUNCH-006，正式 Eval | Eval Owner | 30 Fixture x 3 模式 x 2 Prompt，共 180 Observation 均有终态；成本、P95、回归与人工介入已汇总 | smoke 未通过、预算耗尽、数据范围变化、Fixture 或 Grader 变化 |
| LAUNCH-007，人工评审正式 Eval | Release Approver + Independent Reviewer | 候选结论、例外、失败样本和下一步均已签署 | 自动 Gate 不能代替人工签署 |
| LAUNCH-008，构建五类镜像 | Release Owner | API、Worker、Web、Caddy、Sandbox 均从批准 SHA 产出 linux/amd64 digest | 任一镜像使用 tag、构建上下文漂移或 SHA 不一致 |
| LAUNCH-009，SBOM、provenance、签名和漏洞扫描 | Security Owner | 每个 digest 有可验证 SBOM、provenance、OIDC 签名和扫描；Critical=0、High=0 或合规例外 | 可达 Critical 或未批准 High |
| LAUNCH-010，干净主机拉取与 Release manifest | Release Owner | 五个 digest 在干净 Linux 主机拉取并检查；manifest 校验通过 | digest、签名、架构或 metadata 不一致 |

必须人工执行：Registry 登录与上传、OIDC 签名身份确认、风险接受、镜像拉取验证。

## 7. 阶段 C：真实 Staging 与 Production 控制验证

目标：在不开放 Production 用户流量前关闭 P8-001 至 P8-005 的真实环境证据缺口。

### C.1 基础设施与身份

1. 创建隔离的 control、execution、data、edge 和 egress 网络边界；为 API、Worker、Sandbox daemon、数据库、对象存储、审计和监控使用不同身份、KMS 范围和最小权限。
2. 将 main 的执行面模板渲染到私有 IaC。实际值必须来自批准的 Release manifest、Secret Manager、工作负载身份和真实 Region，不能把私有值回写仓库。
3. 确认 CNI 实际执行 NetworkPolicy；Worker 到 Sandbox daemon 使用 mTLS；Sandbox 子容器保持无网络、非 root、只读根、零 capability、固定 digest 与资源上限。
4. 部署生产级 Secret Manager、KMS、Managed PostgreSQL TLS、对象存储版本和 Object Lock Compliance。应用不得使用长期明文环境变量或主机文件 Secret。

### C.2 必须通过的真实验证

| 范围 | 必须证明 | 关联风险 |
|---|---|---|
| 网络与 Sandbox | API 无 Docker/模型出口；Worker 只能访问数据库、daemon 和批准 egress；Sandbox 无 DNS、外网、metadata 或数据面访问 | P8-002 |
| 数据与对象存储 | tenant 前缀隔离、KMS、完整性、迁移补偿、导出/删除、版本恢复与备份删除清单 | P8-001 |
| MFA 与入口 | HTTPS、Cookie、CSRF、管理员 TOTP、Operator MFA、旁路拒绝、break-glass、轮换和撤销 | P8-003 |
| 审计与遥测 | Object Lock 删除拒绝、审计访问记录、Trace 脱敏、查询和恢复、后端不可用时失败关闭 | P8-004 |
| 数据治理 | Provider、Region、端点、子处理者、保留、隐私告知与显式接受均与批准 Policy 一致 | P8-005 |

每项验证均记录 UTC、批准 SHA、环境、角色、命令摘要、退出码、脱敏 Evidence SHA-256 和独立复核人。任意拒绝测试意外成功、Secret 可跨平面读取、Sandbox 可联网、审计可删除或 MFA 可绕过时，立即停止并保持 No-Go。

## 8. 阶段 D：Staging 业务验收与可运维性

目标：证明真实用户路径、治理路径和故障路径在同一 digest 下可重复。

| 工作项 | 通过条件 |
|---|---|
| LAUNCH-011，digest-only Staging 部署 | HTTPS 公开入口；数据库、Worker、OTLP、监控和 daemon 不公开；Migration、API、Worker、Web、Prompt、Policy、manifest 和 Git SHA 一致 |
| LAUNCH-012，端到端业务验收 | 登录、Session、RBAC、创建 Run、审批、Sandbox、重试、报告、Artifact 下载、导出和删除均通过；Fixture 保持不变 |
| LAUNCH-013，治理演练 | Worker drain 后完成 Eval import、Promotion、Readiness 不匹配拒绝、rollback 和旧 Run 绑定验证 |
| LAUNCH-014，告警与事件响应 | 九类合成告警实际送达主/备值班渠道；告警脱敏、Runbook、确认与升级路径有效 |
| LAUNCH-015，备份与恢复 | 创建真实备份及异地副本，在隔离数据库恢复，完成登录到报告的验收；不覆盖在线库 |
| LAUNCH-016，安全演练与 Demo | 路径、链接、命令、Secret、RBAC、mTLS、网络与日志脱敏测试通过；另一名 Operator 复现 Demo |

封闭试点前必须完成 LAUNCH-011 至 LAUNCH-016，并由 Security Owner、Platform Owner、Data Owner 和 Release Approver 联合确认风险范围。

## 9. 阶段 E：容量、正式发布与发布后观察

目标：将封闭试点提升为正式发布，或以证据明确维持 No-Go。

1. 按 production-slo-capacity.md 的稳态、突发、Worker 故障、API 滚动、Provider 限速和 PostgreSQL 故障切换矩阵执行实测。
2. 回填 API 可用性/延迟、入队、平台开销、Artifact 完整性、容量、RPO、RTO 和错误预算；任何待实测字段都阻止正式发布。
3. 在私有系统登记独立安全评审人、Primary/Secondary、Platform/Data/Service Owner 与 Release Approver。
4. 由独立安全评审确认 Open Critical=0、未接受 High=0；对允许的例外记录 Owner、补偿控制、到期和撤销条件。
5. 审批隐私政策、服务条款、子处理者、Region、保留、删除和导出流程的生效版本。
6. 汇总脱敏证据，填充 Go/No-Go 模板；只有所有必需角色签署 GO 后才创建签名 v1.0.0 Tag 和 GitHub Release。
7. 发布后持续观察健康、错误预算、告警和安全信号；达到预先批准的回滚阈值时，按 digest-only manifest 回滚，不执行 Down Migration。

## 10. GitHub 与本地质量门禁

每个仓库 PR 至少执行下表所列检查；不以“文档改动”作为跳过安全检查的理由。

| GitHub 检查 | 本地等价入口 | 典型阻断 |
|---|---|---|
| Go verification | scripts/verify.ps1，go test，go vet，Staticcheck，govulncheck，构建 | 格式、ST1005、race、漏洞、Migration 或构建失败 |
| PostgreSQL integration | go test -p 1 ./internal/postgres ./internal/userdata ./internal/httpapi，并使用 PostgreSQL 17 | Migration、SQL、租户或级联删除回归 |
| validate | validate-staging-assets、validate-operations-assets、validate-execution-plane-contract、validate-production-readiness、stage-9 acceptance preflight | Compose、Secret、部署契约、Release 或文档状态漂移 |
| Web verification | web 的 npm ci、npm run check、OpenAPI 生成差异、Playwright E2E | 类型、单元测试、构建、浏览器或 API schema 漂移 |

提交前的最小人工复核顺序：

1. 确认只暂存本 PR 文件，个人文档、PDF、output、.forgeflow、.env、Secret 和构建产物保持未跟踪或被忽略。
2. 运行 git diff --cached --check，并扫描冲突标记。
3. 运行与改动范围对应的本地检查；涉及 Go、数据库、部署或 Web 时分别执行上表检查。
4. 推送后确认四项 GitHub 检查均成功，再请求非作者审阅。
5. 合并前再次确认目标分支为最新 main；发生冲突时重放到 main，保留双方有效配置，不在 GitHub 冲突编辑器中删除安全门禁。

## 11. 11 个最终门禁映射

| 门禁 | 本计划对应阶段 | 不可替代的通过证据 |
|---|---|---|
| freeze | A | 私有计划绑定干净候选 SHA 且预检通过 |
| candidate-smoke | A | 1 Case x 2 Prompt 无协议/基础设施错误 |
| formal-eval | B | 180 Observation、成本、质量与人工结论 |
| image-supply-chain | B | 五个 digest、SBOM、provenance、签名、扫描和干净拉取 |
| staging-deploy | C/D | 私有 IaC、digest-only HTTPS Staging 与一致性 |
| governance-drill | D | Promotion、Readiness、rollback、drain 的受控证据 |
| staging-e2e | D | 从登录到报告、导出/删除、Sandbox 和 Fixture 不变性 |
| operations-security | D | 告警、恢复、安全、审计、MFA、Demo 和独立复核 |
| load-recovery | E | SLO、容量、RPO/RTO、故障切换实测 |
| final-go-no-go | E | 角色签署、风险处理、公开文档批准 |
| github-release | E | 签名 Tag、Release、资产和批准 SHA/digest 一致 |

## 12. 首个执行顺序

1. Repository Owner 先修复 LAUNCH-001，启用 main 分支保护。
2. Release Approver 准备 LAUNCH-002 的九项私有 Freeze 输入，并确认候选范围和预算。
3. 从干净 main 运行 Freeze 预检；通过后单独授权候选 smoke。
4. smoke 成功才进入正式 Eval；正式 Eval 获批才进入镜像和真实 Staging。
5. 所有阶段必须按顺序推进。任何代码修复先走独立 PR、四项 GitHub 检查和审阅，再回到新的 Freeze。

在 LAUNCH-001 至 LAUNCH-005 完成前，不应采购或部署面向真实用户的 Production 流量；在 P8-002 未 Verified Closed 前，不应运行不可信仓库的真实执行任务。
