# 阶段 7：PostgreSQL、Queue 与生命周期

阶段 7 把 File Store 验证过的控制面状态扩展为可由独立 Worker 使用的 PostgreSQL 持久层。File Store 继续保留给本地开发和快速单元测试。

## 事务边界

`checkpoint.PostgresStore.Save` 在一个 Serializable 事务中提交：

- `runs` 当前投影与乐观锁版本；
- 不可变 `checkpoints`；
- 仅追加的 `run_events`，并校验已有事件前缀；
- 当前 Approval、NodeExecution、ModelCall 和 ToolCall 投影；
- 非终态 Run 的 `run.wakeup` Outbox 消息。

Checkpoint 插入、事件或 Outbox 任一步失败，Run 投影都会回滚。应用启动只执行 Schema 版本检查，不会隐式执行生产 Migration。

## Queue 和 Worker

Outbox Dispatcher 与 Job Queue 共用 PostgreSQL。领取使用 `FOR UPDATE SKIP LOCKED`；Job 记录 lease ID、owner、截止时间、尝试次数和最大次数。租约过期可以被其他 Worker 回收，超过最大尝试次数进入 `dead`。

Worker 在处理期间发送 Heartbeat，并轮询持久化 Cancellation。取消后 Handler Context 会被取消，模型和 Docker Runner 继续使用同一 Context 传播终止信号。

## 生命周期

Pause 请求持久化后，只在 Runtime 主循环的安全 Checkpoint 生效。暂停时记录 ResumeGuard，包括：

- Workspace ID、路径和 base commit；
- 已使用 Prompt version 与 SHA-256；
- Policy version；
- Pending Approval ID、输入摘要与策略版本。

Resume 前重新核对 Guard，并使用 `git rev-parse HEAD` 确认 worktree base commit。Workspace 丢失或证据变化会明确拒绝恢复。Paused Run 仍可被取消。

## Artifact

Artifact body 写入受管文件目录，PostgreSQL 只保存 ID、Run ID、类型、storage key、SHA-256、大小、Content-Type 和小型属性。读取时拒绝路径逃逸、符号链接和大小不符，并在流读取结束时校验 SHA-256。

## 验收

- PostgreSQL Migration 可重复执行，并提供配对 Down SQL；
- Checkpoint/Event/Outbox 事务回滚测试通过；
- 新进程可以从 PostgreSQL Checkpoint 继续审批流程；
- 两个 Worker 同时竞争时只有一个获得租约；
- 租约过期由其他 Worker 回收；
- 持久化取消传播到 Handler Context；
- Pause/Resume、兼容性篡改和 Paused Cancel 测试通过；
- 真实 PostgreSQL 集成测试连续 5 轮通过；
- custom-format 备份恢复到 `forgeflow_restore_test`，核对 migration/run/checkpoint 为 `1/1/2`。
