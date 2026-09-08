# ForgeFlow 阶段 9 集中验收窗口手册

> 状态：执行入口已准备；真实验收尚未开始
> 原则：按门禁串行执行，前一步未通过时不得启动后一步

## 1. 当前第一个阻断点

`developer/v4` 在 2026-09-05 的一次付费 smoke 中通过 JSON 解码和变更集校验，但在 `git apply --check` 阶段以 `corrupt patch at line 33` 失败；`developer/v1` 同一 Case 也因源上下文不匹配失败。诊断代码已经进入主分支，但没有新的干净 SHA smoke 证明问题已解除。

因此，阶段 9 必须先从最终候选 SHA 运行 1 Case × 2 Prompt 的候选 smoke。只要出现补丁预检失败、超时、结构错误、数据范围不符或预算不足，就停止；不得启动 180 Observation 正式对照，也不得把旧 smoke 当作通过证据。

## 2. 冻结私有验收计划

阶段 9 准备 PR 合并且四项必需检查通过后，由发布负责人手动更新本地主分支：

```powershell
git switch main
git pull --ff-only
git status --short
git rev-parse HEAD
```

只有工作区干净时才能复制模板。填充后的计划包含内部记录 ID、供应链身份和环境地址，只能留在已忽略的 `.forgeflow/acceptance`：

```powershell
New-Item -ItemType Directory -Force .forgeflow/acceptance/1.0.0
Copy-Item deploy/release/stage-9-acceptance-plan.template.json .forgeflow/acceptance/1.0.0/plan.json
```

人工填写以下冻结项，不得把 Secret 写入计划：

- 精确 ForgeFlow、Fixture 和 Private Grader 40 位 commit；
- Prompt、Provider、模型、Reasoning、Policy、Tool 和 Migration 版本；
- 数据发送授权、费用授权和 Release 审批的私有记录 ID；
- Registry、签名身份、OIDC issuer 和 HTTPS Staging origin；
- Primary/Secondary 值班及独立 Security Reviewer 的私有记录 ID。

运行只读预检：

```powershell
./scripts/stage-9-acceptance-preflight.ps1 -Plan .forgeflow/acceptance/1.0.0/plan.json
```

预检只验证冻结值、当前 HEAD、工作区和工程契约，不读取 API Key，不联系 Provider/Registry/Staging，也不执行 Promotion、Tag 或 Release。不带 `-Plan` 时运行阶段 5～9 的快速静态契约检查；`-SkipEngineeringValidators` 只供已经分别运行阶段 5～8 检查的 CI 使用，填充真实计划时脚本会拒绝该开关。

## 3. 串行门禁

规范门禁 ID 依次为：`freeze`、`candidate-smoke`、`formal-eval`、`image-supply-chain`、`staging-deploy`、`governance-drill`、`staging-e2e`、`operations-security`、`load-recovery`、`final-go-no-go`、`github-release`。私有记录、摘要和自动检查必须使用这些 ID，不得另造别名。

| 顺序 | 门禁 | 现有入口 | 成功条件 | 失败动作 |
|---:|---|---|---|---|
| 1 | Freeze | `stage-9-acceptance-preflight.ps1 -Plan ...` | 版本、授权、负责人、超时和证据路径全部冻结 | 修正计划；不得继续 |
| 2 | Candidate smoke | `stage-4-developer-prompt-eval.ps1 -SmokeOnly -SmokeCaseLimit 1` | 两侧无基础设施/协议错误；候选结果可人工复核 | 修复候选或执行器，形成新 SHA |
| 3 | Formal Eval | `stage-4-developer-prompt-eval.ps1 -ConfirmPaidEval` | 180 Observation 终态、自动 Gate 和脱敏报告完成 | 返回候选修复，不构建镜像 |
| 4 | Image supply chain | `docs/release-images.md`、`validate-release-assets.ps1 -Manifest ...` | 五镜像 digest、SBOM、provenance、签名和扫描通过 | 修复并重建受影响镜像 |
| 5 | Staging deploy | `staging-preflight.ps1`、`staging-release.ps1` | 批准 SHA 与 manifest/digest 一致，服务健康 | 停止流量和治理变更 |
| 6 | Governance drill | `stage-4-governance-drill.ps1` | 人工批准后 Promotion/readiness/rollback 均留痕 | drain Worker，执行批准的恢复方案 |
| 7 | Staging E2E | `staging-acceptance.ps1` | HTTPS、RBAC、Sandbox、报告和 Fixture 不变性通过 | 停止后续演练 |
| 8 | Operations/security | `staging-alert-test.ps1`、`staging-security-drill.ps1`、恢复与 Demo 脚本 | 真实投递、边界、恢复、回滚和 Demo 通过 | 创建事件/缺陷并停止 |
| 9 | Load/recovery | `production-slo-capacity.md` 测试矩阵 | SLO、容量、RPO/RTO 均有实测证据 | 调整容量或目标，重新评审 |
| 10 | Final Go/No-Go | `v1.0.0-go-no-go-template.md` | 所有角色签署且结论为 `GO` | 保持 `NO-GO` |
| 11 | GitHub Release | 路线图 13.6～13.7 | 签名 Tag、Release 和资产与批准 SHA/digest 一致 | 撤销草稿，不发布 |

门禁状态必须从 `pending` 变为 `passed`、`failed` 或 `blocked`，不得使用 `skipped` 绕过。每个门禁的输入摘要、开始/结束 UTC、操作者角色、退出码和 Evidence SHA-256 写入私有 evidence 文件。

## 4. 付费 Eval 边界

执行前必须在当日重新核对官方价格与有效窗口，并取得明确的数据范围和费用上限记录。API Key 仅从受控 Key 文件读取。正式命令和参数以 `docs/stage-4-developer-v2-eval-runbook.md` 为准；不得：

- 把 smoke 结果复制为正式报告；
- 换 Campaign 绕过同一授权的累计预算；
- 发送 Private Grader、隐藏测试、原始 Evidence 或凭据给模型；
- 在候选 SHA、Prompt 或 Fixture/Grader commit 变化后续写旧 Evidence。

## 5. Registry、部署和治理变更

以下动作均为人工操作，并分别使用现有脚本的独立确认开关：

- Registry 登录、镜像 push、签名和上传；
- `staging-release.ps1 -ConfirmDeploy`；
- `stage-4-governance-drill.ps1` 的 Eval Import、Promotion 和 rollback；
- 告警真实投递、隔离恢复、安全演练和 Demo 审批；
- 签名 Tag 和 GitHub Release。

不能用一个总确认开关授权全部动作。任何真实操作前，操作者必须重新核对当前门禁、批准 SHA、目标环境和回滚路径。

## 6. Evidence 与公开摘要

原始资料只保存于 `.forgeflow/acceptance/1.0.0` 或组织批准的私有不可变存储。GitHub 只提交由人工逐项复核的 `release-reports/stage-9-acceptance-summary-template.md` 副本及必要的公开供应链资产。

公开摘要禁止包含：API Key、Cookie、DSN、Webhook、内部地址、用户任务、Fixture 源码、Private Grader、隐藏测试名称/源码、模型原始输出、补丁、数据库 dump 和未脱敏日志。

## 7. 最终停止条件

任一条件成立即保持 `NO-GO`：

- `P8-001`～`P8-007` 任一项仍为 Open；
- v4 smoke 或正式 Eval 未通过；
- Critical/High 漏洞未清零且没有获批风险接受；
- 镜像、manifest、Staging 或 Git SHA 不一致；
- MFA、审计、导出/删除、恢复、值班或安全评审未验收；
- SLO、容量、RPO/RTO 缺少真实测量；
- 最终角色签署不完整。

只有签署结论明确为 `GO` 后，发布负责人才可以手动创建签名 Tag 和 GitHub Release。
