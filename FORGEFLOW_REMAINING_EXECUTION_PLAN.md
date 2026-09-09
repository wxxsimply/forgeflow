# ForgeFlow 剩余执行计划

> 当前执行方式已调整为个人预览。近期任务以 `FORGEFLOW_PERSONAL_PREVIEW_PLAN.md` 为准；本文保留为未来升级到正式 Production 时的完整验收计划。


> 制定日期：2026-09-09  
> 当前发布目标：`v1.0.0`  
> 当前 ForgeFlow 基线：`0cf6ded008d8bba4f7f7bc1ff4f391131467739f`  
> 当前状态：阶段 9 进行中，尚未达到 Production Go 或 GitHub Release 条件

## 1. 文档目的

本文档承接 `FORGEFLOW_POST_IMPLEMENTATION_ROADMAP.md`，只安排当前基线之后尚未完成的工作。原路线图仍是范围和验收标准的最终依据；本文档用于明确近期执行顺序、人工操作、自动化任务、证据位置和停止条件。

执行时不得因为工程代码已经完成，就把尚未运行的真实 Eval、镜像供应链、Staging、安全、恢复或负载测试标记为通过。

## 2. 当前完成情况

### 2.1 已完成

- [x] 阶段 0～3 已完成并留下审计记录。
- [x] 30 个 Private Fixture 已建立并验证。
- [x] Private Grader 已隔离保存。
- [x] 三基线 Eval 已执行并由仓库所有者签署为基线。
- [x] 阶段 4～8 的代码、脚本、模板、Runbook 和快速工程检查已经完成。
- [x] 阶段 9 冻结计划模板、11 个串行门禁和公开摘要模板已经建立。
- [x] PR #38 已合并，合并提交为 `0cf6ded008d8bba4f7f7bc1ff4f391131467739f`，四项必需 CI 全部成功。
- [x] Eval 执行器已经加入安全 unified diff 规范化和回归测试。
- [x] DeepSeek Key 仅保存在仓库外部受控文件中，未写入 Git。

### 2.2 已执行但未通过

- [x] 2026-09-09 已运行一次 2 Observation 候选 smoke。
- [x] `developer/v1` 与 `developer/v4` 均完成 JSON 解码和变更集检查。
- [ ] 两者均未通过 `patch_check`，显式测试和隐藏测试没有执行。
- [ ] 本次失败不能用于判断 v4 的真实任务质量，也不能用于 Promotion。
- [x] 对应传输级补丁修复已经通过 PR #38 合并。
- [ ] 修复尚未从新的干净 `main` SHA 通过候选 smoke 验证。

### 2.3 尚未完成

- [ ] 填充并通过私有阶段 9 验收计划预检。
- [ ] 从 PR #38 合并 SHA 重跑 2 Observation 候选 smoke。
- [ ] 完成 180 Observation 正式候选 Eval。
- [ ] 人工完成 Prompt/模型 Promotion 决策和 rollback 演练。
- [ ] 构建、扫描、签名并上传五类不可变镜像。
- [ ] 完成真实公网 HTTPS Staging 部署和 E2E。
- [ ] 完成告警、恢复、安全、回滚、Demo、容量和稳定性验收。
- [ ] 完成独立安全复核和最终 Go/No-Go 签署。
- [ ] 手动创建签名 Tag 和 GitHub Release。

## 3. 冻结基线

在任何新的付费或长时间任务开始前，以下值必须写入被 Git 忽略的 `.forgeflow/acceptance/1.0.0/plan.json`：

| 项目 | 当前值 | 状态 |
|---|---|---|
| ForgeFlow commit | `0cf6ded008d8bba4f7f7bc1ff4f391131467739f` | 已确定 |
| Fixture commit | `6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f` | 已确定 |
| Grader commit | `5942ec84d403e37385203b4c7851d1b92573548a` | 已确定 |
| Baseline Prompt | `developer/v1` | 已确定 |
| Candidate Prompt | `developer/v4` | 已确定 |
| Policy | `eval-policy/v1` | 已确定 |
| Tool | `eval-tools/v1` | 已确定 |
| Migration | `5` | 已确定 |
| Provider / Model | DeepSeek / `deepseek-v4-flash` | 已确定 |
| Reasoning | `low` | 已确定 |
| Registry | `ghcr.io/wxxsimply` | 已确定 |
| Staging HTTPS origin | 待填写 | 阻断 |
| 签名身份 | 待填写 | 阻断 |
| OIDC issuer | 待填写 | 阻断 |
| Primary/Secondary on-call 私有记录 | 待填写 | 阻断 |
| Independent Security Reviewer 私有记录 | 待填写 | 阻断 |
| Eval 数据范围授权记录 ID | 待固化到本地私有记录 | 阻断 |
| Eval 费用上限记录 ID | 每次运行前单独确认 | 阻断 |
| Release approval 记录 ID | 当前只能是 `pending` | 正常待办 |

只要代码、Prompt、Policy、Tool、Fixture、Grader 或 Migration 发生变化，就必须更新候选 SHA，并使受影响的旧证据失效。

## 4. 总体执行原则

1. 所有门禁按顺序执行，不并行跨越依赖。
2. 日常快速检查单次控制在 10 分钟内。
3. 正式 Eval、镜像构建、真实部署、负载和恢复等高耗时任务集中在预留验收窗口执行。
4. 每个外部调用必须有明确数据范围、费用上限和失败停止条件。
5. Private Grader、隐藏测试源码、原始 Evidence、凭据和用户数据不得发送给模型或上传 GitHub。
6. Registry 登录、镜像上传、服务器 Secret 配置、Promotion、Tag 和 GitHub Release 必须人工执行。
7. 任一门禁失败时停止后续步骤，不允许用“已知问题”绕过。
8. 修复失败项需要修改代码时，新建 `codex/` 分支并重新走 PR；合并后重新冻结 SHA。

## 5. 分阶段执行计划

### 阶段 A：补齐集中验收窗口信息

目标：生成可验证、没有占位符的私有验收计划。

#### 需要人工提供

- [ ] Staging 的真实 HTTPS origin，例如 `https://staging.example.com/`。
- [ ] 实际使用的镜像签名身份约束。
- [ ] 实际 OIDC issuer。
- [ ] Primary/Secondary on-call 和联系方式所在私有系统的记录 ID。
- [ ] 独立 Security Reviewer 的私有记录 ID。
- [ ] 本轮 Eval 的 USD 硬上限和授权记录 ID。
- [ ] 数据范围授权记录 ID。

独立 Security Reviewer 不要求购买 GitHub Pro，也不一定要加入仓库。可以通过线下或其他受控方式复核脱敏证据，但必须是除实现者之外的真实人员，并留下可追溯记录。

#### 可自动完成

- [ ] 把已知 SHA、Prompt、模型、Policy、Tool、Migration 和 Registry 写入私有计划。
- [ ] 校验计划路径位于 `.forgeflow/acceptance/1.0.0/` 且被 Git 忽略。
- [ ] 校验所有冻结值完整、当前 HEAD 一致、三个仓库干净。
- [ ] 运行阶段 5～9 静态工程契约检查。

执行命令：

```powershell
pwsh ./scripts/stage-9-acceptance-preflight.ps1 `
  -Plan .forgeflow/acceptance/1.0.0/plan.json
```

退出门槛：预检退出码为 0；没有 unresolved placeholder；没有外部请求或付费调用。

### 阶段 B：重跑最小候选 smoke

目标：证明 PR #38 已解除补丁协议阻断，然后才能启动正式 Eval。

#### 执行范围

- `feature-01`。
- `planner_developer` 模式。
- `developer/v1` 与 `developer/v4` 各 1 个 Observation。
- 使用新的 Campaign ID，不复用旧 Evidence 或旧价格快照。
- 执行前重新核对 DeepSeek 官方价格和高峰/非高峰时段。
- 费用使用独立硬上限；达到上限立即停止。

#### 通过条件

- [ ] 两个 Observation 均完成 JSON 解码。
- [ ] 两个 Observation 均通过变更集验证。
- [ ] 不再出现补丁协议、执行器、Worktree 或 Grader 基础设施错误。
- [ ] `git apply --check` 和补丁应用成功。
- [ ] 显式测试及隐藏测试真实执行。
- [ ] Fixture 仓库运行前后 commit 与工作区状态不变。
- [ ] 生成不包含私有内容的公开脱敏摘要。

如果 smoke 仍失败：立即停止正式 Eval，诊断失败属于模型能力还是基础设施；若要修改仓库代码，则准备修复 PR 并停在人工 GitHub 提交节点。

如果 smoke 通过：更新私有门禁 `candidate-smoke=passed`，进入阶段 C。

### 阶段 C：正式 180 Observation Eval

目标：生成可用于人工 Promotion 决策的正式候选对照报告。

#### 执行前门禁

- [ ] 阶段 B 已通过。
- [ ] 费用硬上限足够覆盖完整执行，并由仓库所有者明确确认。
- [ ] 数据范围授权仍有效。
- [ ] Fixture、Grader 和 ForgeFlow SHA 与冻结计划完全一致。
- [ ] 预留连续运行窗口和断点恢复位置。

#### 执行要求

- [ ] 30 个 Fixture 全部纳入。
- [ ] 固定的三种模式全部纳入。
- [ ] `developer/v1` 和 `developer/v4` 使用相同输入与预算。
- [ ] 失败、拒绝、超时和人工介入全部计入结果，不删除异常样本。
- [ ] 原始结果只保存在 `.forgeflow/evals/`。
- [ ] 公开报告只包含聚合完成率、隐藏测试通过率、回归率、人工介入率、费用和 P95 延迟。

#### 停止条件

- 超过费用硬上限。
- 出现数据越界发送风险。
- Fixture 或 Grader SHA 改变。
- 基础设施错误足以污染样本。
- 连续出现相同确定性故障，继续执行无法增加有效证据。

退出门槛：正式报告完整生成，并由人工审核决定 `APPROVED`、`REJECTED` 或 `REQUIRES NEW CANDIDATE`。

### 阶段 D：Promotion 与 rollback 演练

只有阶段 C 被人工批准后执行。

- [ ] 人工批准 `developer/v4` Promotion。
- [ ] 创建新的不可变 Active Release 记录，不覆盖历史记录。
- [ ] Worker Readiness 与 Active Release 一致。
- [ ] 暂停流量并回滚到 `developer/v1`。
- [ ] 验证 Checkpoint 恢复仍绑定原 Prompt、模型、Policy 和 Tool。
- [ ] 再次恢复批准候选，并留下完整审计记录。

Promotion 是外部治理变更，必须由授权人员人工确认，不能仅凭自动测试结果执行。

### 阶段 E：五类镜像供应链验收

目标：从同一个批准 commit 构建可追溯、不可变的发布镜像。

镜像范围：API、Worker、Web、Caddy、Sandbox。

- [ ] 从批准 commit 构建五类镜像。
- [ ] 生成 SBOM 和 provenance。
- [ ] 运行漏洞扫描；Critical=0、High=0，例外必须有正式风险接受。
- [ ] 使用批准的 OIDC 身份签名。
- [ ] 人工登录 `ghcr.io/wxxsimply`。
- [ ] 人工上传镜像。
- [ ] 记录每个 digest。
- [ ] 在干净环境按 digest 拉取并验证。
- [ ] 生成全部固定到 digest 的 Release manifest。

任何镜像失败时停止，不进入 Staging。

### 阶段 F：真实 HTTPS Staging 部署

目标：使用阶段 E 的同一组 digest 完成真实部署。

#### 必须人工操作

- [ ] 获取服务器或云环境控制权。
- [ ] 配置 DNS 和可信 HTTPS 证书。
- [ ] 从受控 Secret Manager 或本机安全文件注入 Secret。
- [ ] 确认部署动作和可能产生的云资源费用。

#### 自动化验收

- [ ] 执行 Staging preflight。
- [ ] 按 digest 部署 API、Worker、Web、Caddy、Sandbox 和 PostgreSQL 依赖。
- [ ] 验证仅 HTTPS 对公网开放。
- [ ] 验证数据库、Worker、Prometheus、Alertmanager 和 OTLP 未暴露公网端口。
- [ ] 验证 Migration 版本为 5。
- [ ] 验证登录、会话、RBAC、创建任务、执行、审批、重试和报告下载全链路。
- [ ] 验证 Bootstrap Secret 已删除。
- [ ] 验证 Fixture 仓库运行前后不变。

退出门槛：`staging-deploy` 与 `staging-e2e` 均为 `passed`。

### 阶段 G：运维、安全、恢复和性能验收

建议分成两个连续窗口，避免单次任务失控。

#### 窗口 G1：运维和安全

- [ ] 九类告警真实投递并确认脱敏。
- [ ] API/Worker Down 和高错误率告警生效。
- [ ] 路径穿越、符号链接逃逸、Shell 元字符和未知命令被拒绝。
- [ ] Secret、RBAC、Sandbox、mTLS 和执行面边界通过。
- [ ] 备份产生 checksum。
- [ ] 在隔离数据库完成恢复演练并记录 RPO/RTO。
- [ ] 使用旧 manifest 完成版本回滚，不执行 Down Migration。
- [ ] Demo 可由另一名人员按文档重复执行。

#### 窗口 G2：负载和稳定性

- [ ] 明确并发、持续时间、最大费用和中止阈值。
- [ ] 测量 API、Worker、队列和数据库容量。
- [ ] 记录 P50/P95/P99、吞吐、错误率和资源水位。
- [ ] 执行故障恢复和积压恢复。
- [ ] 将实测结果回填 SLO、容量、RPO/RTO 文档。

退出门槛：所有结果达到批准阈值，或明确判定 `NO-GO`。

### 阶段 H：最终证据和 Go/No-Go

- [ ] 汇总脱敏 Eval 报告。
- [ ] 汇总镜像 digest、签名、SBOM、provenance 和扫描结果。
- [ ] 汇总 Staging E2E、告警、安全、恢复、回滚、负载和 Demo 证据。
- [ ] 独立 Security Reviewer 完成签署。
- [ ] Service、Platform、Data、Security 和 Release 角色记录完整。
- [ ] `P8-001`～`P8-007` 全部关闭，或结论保持 `NO-GO`。
- [ ] 阶段 4～8 从 `待集中验收` 更新为 `已完成`。
- [ ] Release Approver 签署最终 `GO`。

没有最终 `GO` 时不得创建 `v1.0.0` Tag 或发布 GitHub Release。

### 阶段 I：手动 GitHub 发布

> 本阶段必须人工操作。

#### 版本提交和签名 Tag

```powershell
git switch main
git pull --ff-only
pwsh ./scripts/verify.ps1
git status --short
git tag -s v1.0.0 -m "ForgeFlow v1.0.0"
git push origin v1.0.0
```

#### GitHub Release

- [ ] Target commit 与最终批准 commit 一致。
- [ ] Tag 为已验证签名的 `v1.0.0`。
- [ ] 上传 Release Notes、升级/回滚说明、SBOM 和脱敏 Eval 汇总。
- [ ] 写明 Migration 版本和镜像 digest。
- [ ] 不上传原始 Evidence、Fixture 源码、Private Grader、隐藏测试、凭据、数据库 dump 或未脱敏日志。
- [ ] 发布后执行健康检查并确认监控正常。

## 6. GitHub PR 节点

以下情况需要建立新的人工 GitHub PR：

1. 候选 smoke 暴露执行器或协议缺陷，需要修改代码。
2. 正式 Eval 需要形成新候选 Prompt。
3. 镜像、部署、安全或运维验收发现必须修复的仓库问题。
4. 最终回填公开脱敏报告、Release Notes 和阶段状态。

每次由自动化准备代码和文档后，人工执行：

```powershell
git status --short
git diff --check
git add <明确列出的文件>
git diff --cached --check
git commit -m "<本阶段提交说明>"
git push -u origin <codex/分支名>
gh pr create --base main --head <codex/分支名>
```

禁止使用未经检查的 `git add .`。`.forgeflow/`、Key、Secret、原始 Eval Evidence 和私有验收计划不得进入提交。

## 7. Evidence 保存规则

| 内容 | 保存位置 | 是否进入 Git |
|---|---|---:|
| 原始模型输出、补丁、隐藏测试结果 | `.forgeflow/evals/` | 否 |
| 私有验收计划与审批记录 | `.forgeflow/acceptance/1.0.0/` | 否 |
| Registry/服务器凭据 | Secret Manager 或仓库外安全文件 | 否 |
| 脱敏 Eval 聚合报告 | `release-reports/` | 是，人工审核后 |
| 工程审计和 Runbook | `docs/` | 是 |
| SBOM、签名和 provenance | 批准的 Release 资产目录 | 是，确认不含 Secret 后 |
| 未脱敏服务器日志和数据库 dump | 私有 Evidence 存储 | 否 |

## 8. 推荐执行窗口

| 窗口 | 内容 | 预计性质 | 前置条件 |
|---|---|---|---|
| W1 | 填充计划、静态预检、2 Observation smoke | 短时、低费用 | 阶段 A 完成 |
| W2 | 180 Observation 正式 Eval | 高耗时、付费 | smoke 通过 |
| W3 | Promotion/rollback、五镜像构建扫描签名 | 高耗时、可能外部写入 | Eval 人工批准 |
| W4 | Registry 上传与真实 Staging 部署 | 人工操作、可能产生云费用 | 镜像全部通过 |
| W5 | E2E、运维、安全、恢复和 Demo | 高耗时 | Staging 稳定 |
| W6 | 负载、容量和稳定性 | 高耗时 | 安全与恢复通过 |
| W7 | 最终签署、Tag 和 GitHub Release | 人工发布 | 所有门禁通过 |

每个窗口开始前重新检查当前 SHA、预算、授权、人员和证据目录。窗口结束时必须明确记录 `passed`、`failed` 或 `blocked`，不得记录为模糊的“基本完成”。

## 9. 当前立即执行顺序

1. 用户提供 Staging origin、签名身份、OIDC issuer、on-call 记录 ID 和独立安全评审记录 ID。
2. 将现有数据范围授权和本轮费用上限固化为本地私有记录。
3. 完成 `.forgeflow/acceptance/1.0.0/plan.json`。
4. 运行填充计划预检；失败则只修复计划或工程契约，不调用模型。
5. 预检通过后，使用新的 Campaign 重跑 2 Observation smoke。
6. smoke 通过后再申请并确认正式 Eval 的独立费用上限，安排 W2。
7. 如果任何步骤需要修改仓库，准备对应分支和验证，停在人工 GitHub 提交处。

## 10. 最终完成定义

只有同时满足以下条件，ForgeFlow 才可标记为完成：

- [ ] 阶段 4～8 的真实后置验收全部完成。
- [ ] 阶段 9 的 11 个门禁全部为 `passed`。
- [ ] 所有发布资产绑定同一个批准 commit 和镜像 digest。
- [ ] 独立安全评审、值班、SLO、容量、RPO/RTO 和数据政策均有真实记录。
- [ ] `v1.0.0` 签名 Tag 与批准 commit 一致。
- [ ] GitHub Release 资产经过脱敏审核。
- [ ] 发布后健康检查、监控和回滚能力正常。

在此之前，项目只能描述为“工程实现完成、发布验收进行中”，不能描述为 Production Ready。
