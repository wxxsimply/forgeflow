# ForgeFlow 个人预览执行计划

> 生效日期：2026-09-09  
> 当前目标：`v0.1.0-preview.1` 个人预览  
> 未来目标：正式 `v1.0.0`，暂不执行  
> 服务器：`39.102.136.31`，尚未配置  
> 计划基线：`0cf6ded008d8bba4f7f7bc1ff4f391131467739f`；实际部署提交在本批 PR 合并后重新填写

## 1. 适用范围

本文档是当前个人预览工作的优先执行计划。`FORGEFLOW_REMAINING_EXECUTION_PLAN.md` 和 `FORGEFLOW_POST_IMPLEMENTATION_ROADMAP.md` 继续保存未来 Production 的完整门禁，但其中正式 180 Observation Eval、Promotion、镜像签名、独立安全评审、多人员值班、负载测试和 `v1.0.0` 发布不再阻断本次个人预览。

个人预览不能描述为 Production Ready，不处理真实用户敏感数据，不承担生产流量。

## 2. 已确认的取舍

- [x] 这是个人项目。
- [x] 当前不执行正式 Eval、付费模型测试或负载测试。
- [x] 保留编译、最小自动回归、Compose 校验和人工登录验证。
- [x] 使用服务器 `39.102.136.31`。
- [x] 服务器尚未配置域名、HTTPS、OIDC、镜像签名、值班或独立安全评审。
- [x] 当前运行 `mock` Planner、`planning` Workflow，关闭 Docker Sandbox。
- [x] DeepSeek Key 不上传服务器。

## 3. 安全访问方式

当前没有域名和可信 HTTPS，因此预览入口不得直接绑定公网地址。Compose 只发布：

```text
127.0.0.1:8080 -> Caddy -> Web/API
```

用户在自己的电脑上建立 SSH 隧道：

```powershell
ssh -L 8080:127.0.0.1:8080 <ssh-user>@39.102.136.31
```

保持 SSH 窗口开启，再访问 `http://127.0.0.1:8080`。不要在云安全组或主机防火墙中开放 TCP 8080。

## 4. 当前已完成

- [x] PR #38 已合并，四项必需 CI 通过。
- [x] ForgeFlow、Fixture 和 Grader 冻结 SHA 已核对。
- [x] 阶段 5～9 静态工程契约通过。
- [x] `go test ./...` 通过。
- [x] Web 类型检查、11 个 Vitest 测试和生产构建通过。
- [x] 已准备独立的个人预览 Compose、Caddy 和环境模板。
- [x] 正式 Staging 和 Production 文件未被降级或覆盖。

## 5. 需要人工完成的下一步

### 5.1 提交部署资产

审核本次新增文件后，人工提交 GitHub PR。不要提交 `deploy/personal-preview/secrets/` 下未来生成的真实 Secret。

### 5.2 提供 SSH 连接信息

只需提供以下非敏感信息：

```text
SSH 用户名：
SSH 端口：22 或其他端口
服务器 Linux 发行版和版本：
Docker 是否已安装：是/否
```

私钥内容、密码和云平台 Token 不得发送到聊天。私钥应保留在本机；后续可由用户在终端手动执行 SSH 命令。

### 5.3 配置服务器

- [ ] 云安全组只允许受控来源访问 SSH。
- [ ] 安装 Git、Docker Engine 和 Docker Compose v2。
- [ ] 创建 `/srv/forgeflow/app` 和 `/srv/forgeflow/repositories`。
- [ ] 拉取仓库并 checkout 冻结提交。
- [ ] 复制 `preview.env.example` 为 `preview.env`。
- [ ] 按 `deploy/personal-preview/secrets/README.md` 手动创建 Secret。

### 5.4 启动和清理 Bootstrap

首次启动使用 Bootstrap overlay：

```bash
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.bootstrap.yaml \
  up -d --build
```

确认管理员可以登录后：

```bash
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.bootstrap.yaml \
  down

rm deploy/personal-preview/secrets/bootstrap_admin_password

docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  up -d
```

删除 Bootstrap Secret 属于人工安全操作。删除前必须确认首次管理员已经创建成功。

## 6. 最小验收

- [ ] `docker compose ps` 中长期服务为 healthy/running。
- [ ] 服务器执行 `curl -fsS http://127.0.0.1:8080/healthz` 成功。
- [ ] 用户通过 SSH 隧道打开登录页面。
- [ ] 管理员登录成功。
- [ ] 创建一个 Mock Planning Run 并看到计划结果。
- [ ] 注销后会话失效。
- [ ] 从公网无法直接访问 `39.102.136.31:8080`。
- [ ] 服务器没有 DeepSeek/OpenAI Key。
- [ ] Bootstrap Secret 已删除。

## 7. 停止条件

出现以下任一情况立即停止：

- SSH 仍使用弱密码或允许任意来源登录。
- 8080、PostgreSQL 或内部服务端口暴露公网。
- Secret 被写入 Git、环境模板、聊天或普通日志。
- 源码 commit 不是冻结提交。
- Migration、API、Worker 或 Web 构建失败。
- Bootstrap 管理员未成功创建却删除了 Bootstrap Secret。
- 服务器将被用于真实用户数据或生产流量。

## 8. 未来升级到正式服务

准备公开服务时，必须回到完整 Production 路线，至少补齐：

- 独立域名和可信 HTTPS。
- 正式 Eval 和 Promotion。
- 不可变镜像、SBOM、漏洞扫描、OIDC 签名和 digest。
- 独立安全评审、多人员值班和恢复演练。
- 负载、容量、SLO、RPO/RTO 和数据政策。
- 最终 Go/No-Go、签名 `v1.0.0` Tag 和 GitHub Release。

个人预览的成功不能自动转化为 Production 批准。
