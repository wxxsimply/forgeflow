# PostgreSQL 与 Worker 操作说明

## 初始化与版本检查

Migration 必须由发布步骤显式执行：

```powershell
$env:FORGEFLOW_POSTGRES_ENABLED="true"
$env:FORGEFLOW_POSTGRES_DSN="从 Secret 注入"
go run ./cmd/forgeflow db migrate
go run ./cmd/forgeflow db check
```

API/CLI/Worker 正常启动只检查版本，不自动迁移。

## 启动 Worker

```powershell
go run ./cmd/forgeflow-worker
```

生产关停应先停止发布新 Outbox，再向 Worker 发送中断信号并等待当前 Handler 退出。未完成 Job 的租约过期后可由其他 Worker 回收。

## 暂停与恢复

```powershell
go run ./cmd/forgeflow pause --run <runId> --reason "maintenance"
go run ./cmd/forgeflow resume --run <runId>
```

恢复失败时先检查 worktree 是否存在、HEAD 是否仍为记录的 base commit，以及 Prompt、Policy 或 Pending Approval 是否变化。不要手工绕过 ResumeGuard。

## 备份与恢复

主机安装 PostgreSQL client 后：

```powershell
./scripts/postgres-backup.ps1 -Dsn $env:FORGEFLOW_POSTGRES_DSN
./scripts/postgres-restore.ps1 -Dsn $env:FORGEFLOW_POSTGRES_RESTORE_DSN -Backup <dump> -ConfirmRestore
```

Restore 脚本只允许目标数据库名包含 `test` 或 `staging`，避免误覆盖 Production。生产恢复应按事故流程创建新数据库，完成一致性检查后再切换连接，不直接覆盖在线数据库。
