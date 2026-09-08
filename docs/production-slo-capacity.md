# ForgeFlow Production SLO、容量与恢复目标

> 状态：阶段 8 目标基线；所有标记“待实测”的结果必须在阶段 9 回填
> 负责人：Service Owner（SLO）、Platform Owner（容量）、Data Owner（RPO/RTO）、Security Owner（安全门禁）

## 1. SLI 与 SLO

统计窗口默认滚动 30 天。计划维护仍计入可用性，避免通过扩大维护窗口掩盖不可靠性；明确的用户输入错误、认证拒绝和策略拒绝不计为服务端失败，但必须单独观测。

| 服务指标 | SLI 定义 | Production 目标 | 阶段 9 证据 |
|---|---|---:|---|
| API 可用性 | 通过边缘到达 API 的有效请求中非 5xx 比例；合成 `/healthz` 同时成功 | ≥ 99.90% / 30 天 | 待实测 |
| API 延迟 | 非流式控制面请求的服务端耗时，排除客户端上传时间 | P95 ≤ 500 ms，P99 ≤ 1 s | 待实测 |
| Run 入队 | API 接受 Run 到 Job 可被 Worker claim，不含审批等待 | P95 ≤ 60 s，99% ≤ 5 min | 待实测 |
| 平台执行开销 | 总执行时间减模型调用、测试进程和人工审批等待 | P95 ≤ 120 s / 标准 fixture | 待实测 |
| Report 可读性 | 已完成 Run 的报告在 owner/RBAC 校验后可读取 | ≥ 99.90%，P95 ≤ 1 s | 待实测 |
| 审批一致性 | 过期 ETag、摘要或版本不匹配的批准被拒绝 | 100% | 待实测 |
| Artifact 完整性 | 下载时大小和 SHA-256 与 metadata 相符 | 100% | 待实测 |

模型 Provider 延迟、模型输出有效率、Sandbox 测试时间和人工审批等待分别记录，不从原始数据删除，也不混入 API 延迟来美化指标。端到端 Run 完成率按任务等级和 Provider 单独报告，在阶段 9 基于正式 Eval 决定发布门槛。

99.90% 月可用性的理论 30 天错误预算约为 43 分 49 秒。实际预算以监控按请求和合成探测共同计算，二者任一耗尽都触发发布冻结。

## 2. 错误预算策略

| 预算消耗 | 动作 | 批准人 |
|---|---|---|
| 30 天预算 < 50% | 正常发布；仍需全部门禁 | Release Manager |
| 预算已消耗 50%～75% | 暂停非可靠性变更；增加复核和观察窗口 | Service Owner |
| 预算已消耗 > 75% | 冻结功能发布，只允许事故/安全/可靠性修复 | Incident Commander + Service Owner |
| 预算耗尽或 P0/P1 未关闭 | No-Go；禁止 Promotion、Production 流量和 GitHub Release | Release Approver |

不得通过关闭告警、排除失败请求、降低采样或修改统计窗口恢复预算。例外必须有到期时间、影响范围、补偿控制和独立批准，Critical 安全问题不可风险接受后上线。

## 3. 单 Run 硬预算基线

当前代码的默认上限保留为全局安全天花板，而不是容量承诺：

| 项目 | 当前硬上限 |
|---|---:|
| Iteration | 2（调用者未指定时） |
| Node calls | 100 |
| Model calls | 20 |
| Tool calls | 200 |
| Tool 输出 | 16 MiB |
| 修改文件 | 32 |
| Diff 大小 / 行数 | 1 MiB / 4,000 行 |
| Repair | 1 |
| 输入 / 输出 Token | 200,000 / 40,000 |
| 估算费用 | USD 10 |
| Run 时长 | 30 分钟 |

Production 产品层应按租户和任务类型设置更低配额；提高任一上限属于安全和成本变更，需要 Eval、容量证据和人工审批。阶段 9 必须核对运行配置没有静默覆盖这些上限。

## 4. Worker 容量模型

当前每个 Worker 进程串行 claim 一个 Job，因此并发容量等于 Ready Worker 数。使用以下变量：

- `λ`：峰值到达率（Run/秒）。
- `T95`：排除人工审批后的 P95 Worker 占用秒数。
- `U`：目标利用率，初始固定为 `0.60`。
- `H`：故障与突发余量，至少 `30%`。

最低 Ready Worker：`ceil((λ × T95 / U) × 1.30)`，并且至少跨两个故障域。若模型 Provider 或数据库有更低配额，以最小外部配额为最终上限。自动扩容同时观察 queue depth、最老 Job 年龄、租约丢失和 Provider 429，不能只按 CPU 扩容。

缩容步骤固定为：停止接收新 Run或限制入口 → 标记 Worker drain → 等待当前 Job/审批 checkpoint → 停止 Worker → 确认租约和 worktree 清理。不得强制删除有引用的 workspace。

## 5. 阶段 9 容量测试矩阵

| 场景 | 最短执行 | 通过条件 | 失败即停止 |
|---|---:|---|---|
| 稳态 | 30 分钟 | 目标吞吐下 U≤60%，Queue P95 和 API SLO 达标 | Queue 持续增长、错误预算燃烧 |
| 2 倍突发 | 10 分钟 | 自动扩容后 99% 入队≤5 分钟，无重复副作用 | Dead Letter、租约重复执行 |
| 单 Worker 故障 | 至少 2 个租约周期 | Job 被安全回收，审批/Artifact 不损坏 | 重复 Patch、丢失 Checkpoint |
| API 实例滚动 | 完整滚动一次 | 无 5xx 突增，Session/CSRF 正常 | 登录或审批中断 |
| Provider 限速 | 直到退避完成 | 有界重试、预算不超限、告警可用 | 重试风暴或成本越界 |
| PostgreSQL 故障切换 | 一次受控切换 | 恢复后无双重 claim，RTO 达标 | 数据丢失或 Schema 不一致 |

原始负载日志、任务正文和用户数据不得进入 Git；只提交脱敏聚合值、测试配置摘要和 Evidence 哈希。

## 6. RPO / RTO 目标

| 数据域 | Production RPO | Production RTO | 恢复方法 | 阶段 9 状态 |
|---|---:|---:|---|---|
| PostgreSQL | ≤ 15 分钟 | ≤ 2 小时 | HA + PITR 到新实例，Schema/E2E 核验后切换 | 待实测 |
| Artifact / Evidence | ≤ 15 分钟 | ≤ 4 小时 | 对象版本/跨故障域复制，按 metadata 清单核验 | 待实测 |
| 审计日志 | 0（已接收事件不得丢失） | ≤ 4 小时可查询 | append-only 外部后端与不可变保留 | 待实测 |
| 配置 / Release manifest | 0 | ≤ 1 小时 | Git + Registry digest + 私有配置备份重建 | 待实测 |

数据库和 Artifact 恢复点必须一致；如果不能证明一致，只能进入隔离调查环境，不得恢复 Production 流量。Staging 建议值仍为 RPO 24 小时/RTO 4 小时，不能作为 Production 实测替代品。

## 7. 阶段 9 回填表

| 指标 | 目标 | 实测 | Evidence ID / 哈希 | Owner | 结论 |
|---|---:|---:|---|---|---|
| API availability / latency | 见第 1 节 |  |  | Service Owner | 待定 |
| Queue / platform overhead | 见第 1 节 |  |  | Platform Owner | 待定 |
| Worker 安全容量 | 见第 4 节 |  |  | Platform Owner | 待定 |
| PostgreSQL RPO/RTO | 15 min / 2 h |  |  | Data Owner | 待定 |
| Artifact RPO/RTO | 15 min / 4 h |  |  | Data Owner | 待定 |

任何“待实测”项目未回填时，阶段 8 只能保持 `待集中验收`，不能宣传为 Production Ready。
