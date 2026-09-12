# ForgeFlow 个人预览执行计划

> 生效日期：2026-09-09  
> 当前目标：`v0.1.0-preview.1` 个人公开预览（公网 IP HTTPS 扩展准备中）
> 未来目标：正式 `v1.0.0`，暂不执行  
> 服务器：`39.102.136.31`，Ubuntu 22.04.5 LTS，私有入口运行中，公网入口尚未启用
> 实际部署基线：`d5d5f5311bd812f511f009cfbc458f52ccbc6a17`（PR #47）

## 1. 适用范围

本文档是当前个人预览工作的优先执行计划。`FORGEFLOW_REMAINING_EXECUTION_PLAN.md` 和 `FORGEFLOW_POST_IMPLEMENTATION_ROADMAP.md` 继续保存未来 Production 的完整门禁，但其中正式 180 Observation Eval、Promotion、镜像签名、独立安全评审、多人员值班、负载测试和 `v1.0.0` 发布不再阻断本次个人预览。

个人预览不能描述为 Production Ready，不处理真实用户敏感数据，也不提供生产 SLA。

## 2. 已确认的取舍

- [x] 这是个人项目。
- [x] 仓库所有者决定不配置自有域名，并要求其他互联网用户可以访问登录入口。
- [x] 当前不执行正式 Eval、付费模型测试或负载测试。
- [x] 保留编译、最小自动回归、Compose 校验和人工登录验证。
- [x] 使用服务器 `39.102.136.31`。
- [x] 服务器不配置自有域名；公网入口只允许使用受信任的 IP HTTPS，不允许公网 HTTP 登录。
- [x] 当前运行 `mock` Planner、`planning` Workflow，关闭 Docker Sandbox。
- [x] DeepSeek Key 不上传服务器。

## 3. 安全访问方式

默认私有模式继续只发布：

```text
127.0.0.1:8080 -> Caddy -> Web/API
```

用户可在自己的电脑上建立 SSH 隧道：

```powershell
ssh -L 8080:127.0.0.1:8080 <ssh-user>@39.102.136.31
```

保持 SSH 窗口开启，再访问 `http://127.0.0.1:8080`。不要在云安全组或主机防火墙中开放 TCP 8080。

可选公网模式通过独立 overlay 额外发布：

```text
0.0.0.0:443 -> Caddy TLS -> Web/API
127.0.0.1:8080 -> Caddy -> Web/API
```

公网模式使用 Let's Encrypt 短期 IP 证书、Secure Cookie、受限 Origin 和持久证书卷；完整步骤见 `docs/public-ip-https-deployment.md`。

## 4. 当前已完成

- [x] PR #38 已合并，四项必需 CI 通过。
- [x] PR #47 已合并，四项必需 CI 通过，区域 Alpine 镜像配置已在服务器实测。
- [x] ForgeFlow、Fixture 和 Grader 冻结 SHA 已核对。
- [x] 阶段 5～9 静态工程契约通过。
- [x] `go test ./...` 通过。
- [x] Web 类型检查、11 个 Vitest 测试和生产构建通过。
- [x] 已准备独立的个人预览 Compose、Caddy 和环境模板。
- [x] 正式 Staging 和 Production 文件未被降级或覆盖。
- [x] 服务器使用 SSH 密钥认证、专用 `forgeflow` 部署用户，且已禁用 root SSH 登录。
- [x] 个人预览已部署到 `d5d5f5311bd812f511f009cfbc458f52ccbc6a17`，Migration schema version 为 `5`。
- [x] PostgreSQL、API、Worker、Web 和 Caddy 均通过健康检查，原有 TTS 服务未受影响。

## 5. 部署执行记录

### 5.1 提交部署资产

- [x] 部署资产已经人工审核并通过 PR 合并。
- [x] `deploy/personal-preview/secrets/` 中的真实 Secret 未进入 Git。

### 5.2 提供 SSH 连接信息

- [x] SSH 目标确认为 `forgeflow@39.102.136.31:22`。
- [x] 服务器确认为 Ubuntu 22.04.5 LTS，Docker 29.1.3、Docker Compose 2.40.3。
- [x] 私钥内容、密码和云平台 Token 未写入聊天、Git 或普通日志。

部署复核时只记录以下非敏感信息：

```text
SSH 用户名：
SSH 端口：22 或其他端口
服务器 Linux 发行版和版本：
Docker 是否已安装：是/否
```

私钥内容、密码和云平台 Token 不得发送到聊天。私钥应保留在本机；后续可由用户在终端手动执行 SSH 命令。

### 5.3 配置服务器

- [x] 云安全组阻止公网直接访问 ForgeFlow 8080，SSH 使用密钥认证。
- [x] 云安全组 SSH(22) 已从 `0.0.0.0/0` 收紧到可信公网 IP/CIDR，仓库所有者已人工确认新连接成功。
- [x] 云安全组中不再需要的公网入站 3000 和 3389 规则已删除。
- [x] 安装 Git、Docker Engine 和 Docker Compose v2。
- [x] 创建 `/srv/forgeflow/app` 和 `/srv/forgeflow/repositories`。
- [x] 仓库 checkout 到实际部署提交 `d5d5f5311bd812f511f009cfbc458f52ccbc6a17`。
- [x] 创建权限为 `0600` 的 `preview.env`，并使用可信 HTTPS 软件包镜像。
- [x] 按 `deploy/personal-preview/secrets/README.md` 创建并验证数据库 Secret。

### 5.4 启动和清理 Bootstrap

- [x] 首次启动时使用 Bootstrap overlay 创建管理员。
- [x] 管理员完成登录、Mock Run 和注销验收后删除 `bootstrap_admin_password`。
- [x] 服务已使用不含 Bootstrap overlay 的基础 Compose 重新启动并通过健康检查。

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

> 验收日期：2026-09-11。登录、Mock Run、注销和公网检查由仓库所有者人工确认；其余项目由部署检查验证。

- [x] `docker compose ps` 中长期服务为 healthy/running。
- [x] 服务器执行 `curl -fsS http://127.0.0.1:8080/healthz` 成功。
- [x] 用户通过 SSH 隧道打开登录页面。
- [x] 管理员登录成功。
- [x] 创建一个 Mock Planning Run 并看到计划结果。
- [x] 注销后会话失效。
- [x] 从公网无法直接取得 `39.102.136.31:8080` 的 ForgeFlow 响应。
- [x] 服务器没有 DeepSeek/OpenAI Key。
- [x] Bootstrap Secret 已删除，API 已在不含 Bootstrap 配置的基础 Compose 下重新启动。

## 7. 公网 IP HTTPS 扩展

> 当前状态：仓库资产准备中，尚未改变服务器端口或阿里云安全组。

- [x] 已明确只公开 HTTPS 443，继续阻止公网 8080。
- [x] 已准备可回退的 Compose overlay 和专用 Caddy 配置。
- [x] 已把 Secure Cookie、Origin、端口和持久卷契约加入 CI。
- [ ] 公网 HTTPS 资产 PR 已人工审核并合并。
- [ ] 服务器 TCP 443 已确认未被 TTS 或其他服务占用。
- [ ] 阿里云安全组已允许公网 TCP 443，仍拒绝公网 8080。
- [ ] Caddy 已成功取得并自动管理 `39.102.136.31` 的受信任证书。
- [ ] 外部网络已完成健康检查、登录、Mock Run 和注销验证。
- [ ] 浏览器无证书警告，且 HTTP、8080、数据库和内部端口未暴露。

## 8. 停止条件

出现以下任一情况立即停止：

- SSH 仍使用弱密码或允许任意来源登录。
- 8080、PostgreSQL 或内部服务端口暴露公网。
- Secret 被写入 Git、环境模板、聊天或普通日志。
- 源码 commit 不是冻结提交。
- Migration、API、Worker 或 Web 构建失败。
- Bootstrap 管理员未成功创建却删除了 Bootstrap Secret。
- 服务器将被用于真实用户数据或生产流量。

## 9. 未来升级到正式服务

准备公开服务时，必须回到完整 Production 路线，至少补齐：

- 独立域名和可信 HTTPS。
- 正式 Eval 和 Promotion。
- 不可变镜像、SBOM、漏洞扫描、OIDC 签名和 digest。
- 独立安全评审、多人员值班和恢复演练。
- 负载、容量、SLO、RPO/RTO 和数据政策。
- 最终 Go/No-Go、签名 `v1.0.0` Tag 和 GitHub Release。

个人预览的成功不能自动转化为 Production 批准。
