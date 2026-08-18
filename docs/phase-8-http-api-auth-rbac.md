# 阶段 8：HTTP API、登录与 RBAC

本阶段已经完成 ForgeFlow 控制面的安全 HTTP 基础。完整契约由 `GET /api/openapi.yaml` 和 `internal/httpapi/openapi.yaml` 提供。

## 已完成

- `/api/v1` REST API、统一错误 DTO、请求 ID 和 OpenAPI 3.1 文档。
- PostgreSQL `users`、`sessions`、`repositories`、Run owner/repository 关联、幂等请求和审计日志 Migration。
- Argon2id 密码 Hash；参数可以升级，用户下次成功登录时自动重算。
- 高熵 Session/CSRF Token；数据库只保存 SHA-256 摘要。
- `HttpOnly + Secure + SameSite=Lax` Session Cookie 和双提交 CSRF 防护。
- 登录、退出、当前用户、Session 列表和 Session 撤销。
- 登录按规范化账号和来源 IP 限速，失败信息不区分账号不存在、密码错误或账号禁用。
- `admin`、`operator`、`viewer` RBAC；Repository、Run、Approval 都执行 owner 或 admin SQL 过滤。
- Repository CRUD/校验；仓库路径只能位于 `FORGEFLOW_REPOSITORY_ROOTS`。
- Run 创建、列表、详情、暂停、恢复、取消、事件、SSE、Artifact 元数据和报告接口。
- `Idempotency-Key` 先占位再创建，避免并发重复 Run。
- Approval 查询和决策；决策强制携带当前 Run 版本 `If-Match`，最终保存仍使用乐观锁。
- SSE 支持 `after` 或 `Last-Event-ID`，事件 ID 使用 PostgreSQL 追加事件 sequence。
- 创建 Run 只持久化初始 Checkpoint/Outbox，由独立 Worker 执行，API 不需要模型密钥。

密码找回、公开注册、OIDC、Eval 管理和 Prompt 升级没有加入首版认证接口；它们仍属于后续阶段。

## 本地启动

先准备 PostgreSQL 并执行 Migration：

```powershell
$env:FORGEFLOW_POSTGRES_ENABLED="true"
$env:FORGEFLOW_POSTGRES_DSN="postgres://forgeflow:password@127.0.0.1:5432/forgeflow?sslmode=disable"
go run ./cmd/forgeflow db migrate
```

首次启动时创建唯一的初始管理员。密码至少 12 字节：

```powershell
$env:FORGEFLOW_HTTP_ADDRESS="127.0.0.1:8080"
$env:FORGEFLOW_HTTP_COOKIE_SECURE="false" # 仅本地 HTTP 开发
$env:FORGEFLOW_REPOSITORY_ROOTS="D:\Code"
$env:FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL="admin@example.com"
$env:FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD="replace-with-a-long-password"
go run ./cmd/forgeflow-api
```

日志出现 `bootstrap administrator created` 后，停止进程并删除两个 bootstrap 环境变量，再启动 API：

```powershell
Remove-Item Env:FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL
Remove-Item Env:FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD
go run ./cmd/forgeflow-api
```

另开终端启动 Worker：

```powershell
$env:FORGEFLOW_POSTGRES_ENABLED="true"
$env:FORGEFLOW_POSTGRES_DSN="postgres://forgeflow:password@127.0.0.1:5432/forgeflow?sslmode=disable"
go run ./cmd/forgeflow-worker
```

检查 API：

```powershell
Invoke-RestMethod http://127.0.0.1:8080/healthz
Invoke-WebRequest http://127.0.0.1:8080/api/openapi.yaml
```

生产环境必须使用 HTTPS、`FORGEFLOW_HTTP_COOKIE_SECURE=true`，显式配置 `FORGEFLOW_HTTP_ALLOWED_ORIGINS` 和受限的 `FORGEFLOW_REPOSITORY_ROOTS`。API 只检查 Schema 版本，不会在启动时自动执行 Migration。

## 浏览器调用约定

登录成功响应会设置 `forgeflow_session` 和 `forgeflow_csrf` Cookie，并返回 `csrfToken`。后续所有 POST/DELETE 请求必须：

1. 使用 `credentials: "include"` 发送 Cookie。
2. 把 CSRF 值放入 `X-CSRF-Token` 请求头。
3. 创建 Run 时按客户端操作生成稳定的 `Idempotency-Key`。
4. 审批前先 GET Approval，读取响应 `ETag`，决策时原样发送到 `If-Match`。
5. SSE 重连时把最后收到的 sequence 放入 `Last-Event-ID`。

## 验证覆盖

真实 PostgreSQL E2E 覆盖：正确登录、Session 固定防护、Token 摘要、Cookie 属性、CSRF、登录限速、统一失败、Run 幂等并发、水平越权、Viewer 禁写、审批版本冲突和 SSE sequence。原有 Checkpoint、Outbox、Queue、租约、Artifact 和恢复集成测试也继续通过。
