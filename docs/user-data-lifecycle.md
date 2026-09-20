# 用户数据导出与账户删除 Runbook

> 状态：代码与自动化测试基线；真实 Staging、备份生命周期和对象存储验收仍待阶段二执行
> 适用版本：Migration 6 及以上

## 能力边界

ForgeFlow 提供以下登录态 API，并在 Web 的“数据与账户”页面暴露用户自助入口：

- `POST /api/v1/account/exports`：创建默认 15 分钟有效、一次性使用的 owner-scoped 导出凭证。
- `GET /api/v1/account/exports/{exportId}/content`：下载 ZIP；凭证与当前用户绑定，已使用、过期或跨租户统一返回不存在。
- `DELETE /api/v1/account`：要求当前密码和固定确认词 `DELETE`，立即冻结账号、撤销全部会话并提交异步删除。
- `GET /api/v1/admin/user-deletions/{deletionId}`：管理员查看删除清单、进度和最近错误。
- `POST /api/v1/admin/user-deletions/{deletionId}/retry`：仅对 `failed` 请求重新入队。

最后一个有效管理员不能删除自己。所有修改端点继续要求 Session、CSRF 和速率限制。

## 导出内容与限制

导出 ZIP 包含 `database.json` 和 `artifacts/{artifactId}`。数据库快照覆盖用户、会话元数据、仓库、Run、Checkpoint、事件、审批、节点执行、Job、Artifact 元数据、模型/工具调用、Outbox、幂等记录、审计及与用户有关的治理记录。Artifact 读取复用对象存储的大小和 SHA-256 校验。

以下字段不会进入导出：密码哈希、标准化邮箱、MFA 密钥密文/待确认密文/恢复码摘要/防重放时间片、Session token/CSRF 哈希、幂等请求哈希和删除 owner 哈希。导出不会包含其他 owner 的记录。

环境变量：

| 变量 | 默认值 | 约束 |
|---|---:|---|
| `FORGEFLOW_USER_DATA_EXPORT_TTL` | `15m` | 1 分钟到 24 小时 |
| `FORGEFLOW_USER_DATA_EXPORT_MAX_BYTES` | `536870912` | 1 MiB 到 2 GiB |
| `FORGEFLOW_USER_DELETION_BACKUP_TTL` | `720h` | 24 小时到 365 天 |

API 在响应前完成整个 ZIP，因此大小上限也保护进程内存。Production 应根据最大单租户数据量下调上限并监控 5xx、延迟和内存；超大租户迁移到流式异步导出属于后续容量工作。

## 删除状态机

1. API 在 Serializable 事务中锁定用户，保护最后一个管理员，写入幂等 tombstone。
2. 用户状态切换为 `deletion_pending`，Session 全部撤销，未租用的 Run Job 终止，并创建 `user.delete` Job。
3. Worker 将首次看到的 Artifact 完整清单持久化。每删除一个对象就追加 `deleted_artifact_ids`，失败时标记 `failed` 并由队列重试。
4. 对象全部删除后，Worker 再次锁定用户；仍有有效 Lease 时安全失败，不执行数据库级联。
5. Worker 删除用户 Outbox 和可关联审计，删除用户并由 FK 级联 Repository、Run 及其全部子记录；Eval/Prompt 发布记录的 actor 被匿名化为空。
6. tombstone 保留 request ID、对象清单、已删除 ID、尝试次数、结果和 `backup_purge_after`，并写入无 actor 的完成审计。重复处理已完成请求是 no-op。

`backup_purge_after` 是备份删除清单的截止元数据，不会改写不可变备份。备份系统必须在恢复开放访问前重放已完成 tombstone，并在保留期结束后按组织流程验证不可恢复；本 PR 不对真实备份介质执行删除。

## 失败恢复

先查询状态：

```http
GET /api/v1/admin/user-deletions/{deletionId}
```

- `processing`：确认 worker 存活及 Job Lease；不要并行人工删库。
- `failed`：检查 `lastError`、`artifactManifest` 和 `deletedArtifactIds`。修复对象存储或数据库故障后调用 retry。
- `completed`：禁止 retry；核对匿名完成审计和备份清单。
- Job 达到最大尝试次数后会进入 `dead`，管理员 retry 会清空 Lease/错误、重置尝试次数并重新排队。

不得手工删除 `user_deletion_requests`，否则失去对象和备份清单。不得先删除用户行再清理对象存储。

## 上线验收清单

- 两个真实租户互相无法读取导出凭证或内容。
- ZIP 的数据库范围、Artifact 数量、大小和 SHA-256 与源一致，过期凭证失效。
- 错误密码、错误确认词和最后管理员删除均失败且账号保持可用。
- 注入对象删除失败后，清单与进度保留；恢复后 retry 完成且不重复误删。
- 删除完成后用户、Session、Repository、Run 和子记录均不存在，治理 actor 已匿名化，其他租户不变。
- 真实 Bucket 的版本化对象、复制副本和备份恢复均重放 tombstone，并在 `backup_purge_after` 后完成不可恢复验证。
- 告警、日志和审计中不出现密码、Session/CSRF、下载包内容或客户源码。

上述真实环境证据完成前，只能将此能力标记为“工程实现完成”，不能关闭 Production 数据治理门禁。
