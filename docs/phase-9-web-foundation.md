# 阶段 9：Web 控制台基础批次

阶段 9 的第一批 Web 功能已经完成：登录、安全会话恢复、受保护应用壳、Session 管理、Run 列表、Run 只读详情、Graph 摘要、追加事件时间线和 SSE 恢复。

## 技术栈与契约

- React 19、TypeScript 5.9、Vite 8。
- React Router 7 负责受保护路由和站内跳转。
- TanStack Query 负责 Auth、Run、Session 和 Event 服务端缓存。
- `openapi-typescript` 从 `internal/httpapi/openapi.yaml` 生成 `web/src/api/schema.d.ts`。
- `openapi-fetch` 使用生成的 `paths` 类型，不维护第二套手写 API DTO。
- Vitest、Testing Library、Playwright Chromium 和 Axe 负责测试。

API Client 固定使用 `credentials: include`。Session Token 位于 HttpOnly Cookie，前端无法读取；所有写请求自动从 `forgeflow_csrf` Cookie恢复 CSRF 值并写入 `X-CSRF-Token`。登录后的 `next` 只允许同源绝对路径，拒绝 `//host`、外站 URL 和反斜杠路径。

## 已完成页面

- `/login`：语义化 label、email/password autocomplete、保持登录、提交中状态、401/429/网络错误和安全跳转。
- `/runs`：cursor 分页、Loading、Empty、Error、Forbidden 和 Offline 状态。
- `/runs/:runId`：状态、当前节点、耗时、成本、Graph 摘要和追加事件时间线。
- `/sessions`：当前 Session 标识、设备信息和其他 Session 撤销。
- App Shell：导航、角色、当前用户、退出、路由守卫和错误边界。

SSE 首次连接使用已加载事件 sequence；刷新或重连时从 `sessionStorage` 恢复最后 sequence，并通过 `after` 请求参数续传。服务端使用标准 SSE `message`，业务类型放在 JSON `type` 字段，因此未来新增事件不需要前端维护白名单。

## 本地运行

先按 [阶段 8 运行说明](./phase-8-http-api-auth-rbac.md)启动 PostgreSQL、API 和 Worker，然后启动 Web：

```powershell
cd web
npm ci
npm run generate:api
npm run dev
```

访问 `http://127.0.0.1:5173`。Vite 会把 `/api` 和 `/healthz` 代理到 `http://127.0.0.1:8080`，浏览器仍按同源 Cookie 模式调用。

生产构建：

```powershell
cd web
npm run build
```

输出位于 `web/dist`。本阶段不上传服务器，也没有把静态文件嵌入 API 进程。

## 验证

```powershell
cd web
npm run check
npm run test:e2e
```

`npm run check` 会重新生成 OpenAPI 类型、执行 TypeScript 检查、运行组件测试并完成生产构建。Playwright E2E 覆盖登录、恶意重定向拦截、刷新恢复、Viewer 只读、不可访问 Run 和退出；Axe 对登录页和 Run 列表执行 serious/critical 无障碍扫描。真实 PostgreSQL 下的双用户隔离、Session、CSRF 和 RBAC 继续由 Go API E2E 覆盖。

## 下一批

阶段 9 尚未整体结束。下一批实现创建 Run、仓库选择、审批列表/详情、`If-Match` 冲突处理、暂停/恢复/取消、Diff/Artifact、Trace 和报告，再用真实浏览器串起“登录 → 创建 Run → 审批 → 查看报告”。Eval 和管理后台仍后置。
