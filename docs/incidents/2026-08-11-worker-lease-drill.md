# Incident Review：Worker 租约恢复演练

- Incident ID / severity：DRILL-2026-08-11 / SEV-3（演练）
- 范围：本地 PostgreSQL + 两个 Worker fixture；无真实租户、无真实 Secret。
- 场景：Worker 在持有 Job 租约时被终止。

## Timeline (UTC)

- T+00:00：提交 fixture Run，Worker A 获得租约。
- T+00:05：在安全测试点终止 Worker A；未直接修改 jobs/checkpoint。
- T+00:35：租约过期，Worker B 使用 `SKIP LOCKED` 重新领取。
- T+00:38：Checkpoint 乐观锁和 NodeExecution 幂等记录阻止重复副作用。
- T+00:50：Run 继续到审批/终态；原仓库工作文件保持不变。

## 结论

租约恢复、Checkpoint 和幂等控制按设计工作。观测缺口是早期版本没有 Queue depth/lease lost 指标；阶段 10 已补充 Metrics，阶段 11 已加入 Worker Down 与 Queue Backlog 告警。

## Action items

| Action | 状态 | 验证 |
|---|---|---|
| Worker 暴露内部 `/healthz` 和 `/metrics` | 完成 | Worker handler 测试 |
| Worker Down / Queue Backlog 告警 | 完成 | Prometheus rules + 通知演练 |
| Production 专用 Worker 主机故障演练 | 待完成 | Production 阻断项 |

本记录是合成故障演练复盘，不代表 Production 事故或真实客户影响。
