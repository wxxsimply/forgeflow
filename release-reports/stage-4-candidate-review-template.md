# 阶段 4 当前候选人工审核模板

> DRAFT / NOT APPROVED。填写模板或完成工程准备不构成真实成绩、Promotion 授权或发布批准。

## 版本与授权

| 项目 | 实际值 |
|---|---|
| UTC 时间 / 审核人 / 签署记录 | 待填写 |
| 当前 / 候选 Prompt version 与 SHA-256 | 待填写；当前默认 developer/v1；候选须按实际填写 |
| ForgeFlow / Fixture / Grader 完整 Git SHA | 待填写 |
| Provider / Model / Reasoning / Policy / Tool / 环境 | 待填写 |
| Campaign ID / 价格来源及有效期 | 待填写 |
| 数据发送授权记录 / 总费用上限 / 已用费用 / 剩余额度 | 待填写；不得包含凭据值 |
| 当前 / 候选 Evidence 摘要与差异报告摘要 | 待填写 SHA-256；原始文件保持私有 |

## 数据完整性与自动门禁

- [ ] 候选快速 smoke 已排除当前阻断问题，且与正式运行的候选配置一致。
- [ ] 当前与候选各有三模式 × 30 Case，共 180 个终态 Observation。
- [ ] 未排除失败、拒绝、超时和人工介入；未执行测试的样本没有伪装为已测试。
- [ ] `forgeflow eval compare` 可比性检查通过，版本、模型、环境和预算链无不允许的漂移。
- [ ] 当前与候选费用共享总上限，候选 prior cost 衔接正确，未超用户剩余授权。
- [ ] 自动 Promotion Gate 允许，且理由逐项复核；阻断时禁止批准 Promotion。
- [ ] 汇总报告已脱敏，不含原始 Evidence、源码、补丁、隐藏测试、Private Grader 或凭据。

## 实测结果

每格填写“当前值 / 候选值 / 候选减当前”，保留失败样本。门限引用实际版本的自动 Gate 配置和结果，不手填估算成绩。

| 模式 | 完成率 | 隐藏测试通过率 | 回归率 | 人工介入率 | 平均费用 USD | P95 延迟 ms |
|---|---|---|---|---|---|---|
| single_agent | 待填写 | 待填写 | 待填写 | 待填写 | 待填写 | 待填写 |
| planner_developer | 待填写 | 待填写 | 待填写 | 待填写 | 待填写 | 待填写 |
| forgeflow | 待填写 | 待填写 | 待填写 | 待填写 | 待填写 | 待填写 |

Gate allowed / reasons / 使用的门限：待填写。

## 人工决定

只选择一项，并记录理由与人工签署链接：

- [ ] REJECTED / RERUN REQUIRED
- [ ] APPROVED AS CANDIDATE ONLY
- [ ] APPROVED FOR PROMOTION（仍须完成镜像与 Staging 前置条件，不等于 Production 发布批准）

理由、审核人、UTC 时间、签署记录：待填写。

## 阶段 9 实际治理演练回填

| 项目 | 实际值 |
|---|---|
| 批准的当前 / 候选 Eval Run ID | 待填写 |
| API / Worker 镜像 digest 与完整 Git SHA | 待填写 |
| Promotion 前的 Active Release ID | 待填写；回滚目标使用此记录 |
| Promotion / rollback 新 Release ID | 待填写 |
| Worker drain / 重启时间与操作者 | 待填写 |
| 匹配 200 / 不匹配 503 / 旧 Worker 停止取 Job | 待填写 |
| 已运行 Run / Checkpoint 版本绑定不变 | 待填写 |
| rollback 后目标 Prompt / 模型 / Readiness | 待填写 |
| 实际演练审核人 / UTC 时间 / 签署记录 | 待填写 |

没有实际环境证据和人工签署时，本表保持草稿，阶段 4 不得标记为“已完成”。
