# ForgeFlow v1.0.0 集中验收公开摘要模板

> 模板本身不代表通过，不得在最终 `GO` 前作为发布证明。

## 1. 冻结候选

- ForgeFlow Git SHA：`<40 位批准 commit>`
- Fixture commit：`<40 位 commit>`
- Private Grader commit：`<仅 commit，不提供地址或源码>`
- Prompt：`<planner/developer baseline/candidate/reviewer/security>`
- Provider / 模型 / Reasoning：`<值>`
- Policy / Tool / Migration：`<值>`
- Release manifest SHA-256：`<值>`
- 验收窗口（UTC）：`<开始>` ～ `<结束>`

## 2. 门禁摘要

| 门禁 | 结论 | 脱敏指标或公开证据 | 私有记录校验值 |
|---|---|---|---|
| Candidate smoke | `<PASS/FAIL>` | `<无原始输出>` | `<私有 Evidence SHA-256>` |
| Formal Eval | `<PASS/FAIL>` | `<完成率、隐藏测试通过率、回归率、人工介入率、成本、P95>` | `<SHA-256>` |
| Image supply chain | `<PASS/FAIL>` | `<五镜像 digest、SBOM/签名/扫描链接>` | `<SHA-256>` |
| Staging deployment/E2E | `<PASS/FAIL>` | `<批准 SHA、HTTPS、版本一致性>` | `<SHA-256>` |
| Governance drill | `<PASS/FAIL>` | `<Promotion/readiness/rollback ID 的脱敏摘要>` | `<SHA-256>` |
| Operations/security | `<PASS/FAIL>` | `<告警、边界、Demo 结论>` | `<SHA-256>` |
| Load/recovery | `<PASS/FAIL>` | `<P95、容量、RPO/RTO>` | `<SHA-256>` |
| Production blockers | `<CLOSED/OPEN>` | `<P8-001～P8-007 状态>` | `<SHA-256>` |

失败、拒绝、超时和人工介入必须计入指标，不能只报告成功样本。未执行项目填写 `NOT RUN`，不得填写 `PASS`。

## 3. 风险与限制

- Open Critical：`<数量与公开摘要>`
- Open High：`<数量与公开摘要>`
- 获批风险接受：`<公开记录或无>`
- 已知限制：`<限制及用户影响>`
- 数据/Region/子处理者结论：`<公开摘要>`

## 4. 人工签署

| 角色 | 结论 | 公开签署记录 |
|---|---|---|
| Service Owner | `<APPROVE/REJECT>` | `<PR/记录>` |
| Platform Owner | `<APPROVE/REJECT>` | `<PR/记录>` |
| Data Owner | `<APPROVE/REJECT>` | `<PR/记录>` |
| Independent Security Reviewer | `<APPROVE/REJECT>` | `<PR/记录>` |
| Release Approver | `<GO/NO-GO>` | `<PR/记录>` |

最终结论：`<GO/NO-GO>`

只有五个角色记录完整、所有阻断门禁通过且 Release Approver 填写 `GO` 时，才允许创建 `v1.0.0` Tag 和 GitHub Release。
