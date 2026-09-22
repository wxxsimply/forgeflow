# ForgeFlow 个人预览 HTTPS 与 Secret 就绪记录

> 状态：PERSONAL-002、PERSONAL-003 已完成
>
> 证据日期：2026-09-14；仓库复核日期：2026-09-22
>
> 范围：当前受限个人预览，不代表生产级、高可用或任意仓库执行就绪

## 1. HTTPS 就绪结论

- 当前入口为 https://39.102.136.31，使用公开信任的短期 IP 证书。
- 公网只发布 TCP 443；HTTP 8080 只绑定服务器回环地址。
- HTTPS overlay 强制 Secure Cookie，并只允许当前 HTTPS Origin。
- Caddy 为无 SNI 的 IP 客户端配置 default_sni，证书数据与配置保存在专用持久卷。
- 外部健康检查在不跳过证书验证时成功，登录页面返回 200。
- 用户已经确认浏览器无证书警告、登录、模拟流程、刷新恢复和注销均成功。

个人项目不要求购买自有域名。可信 IP 证书满足当前受限预览的 HTTPS 门禁；若入口 IP、证书方式或 Origin 改变，必须重新验收。

## 2. 预览运行配置

个人预览明确不是 Production，因此保留 FORGEFLOW_ENV=development。它只能在以下限制同时成立时公开：

- Workflow 固定为 Planning；
- Planner 固定为 Mock；
- Docker 执行关闭；
- API 的仓库挂载只读；
- Worker 仅访问项目所有者检查过的演示仓库；
- 服务器不保存模型 API Key。

这是一套显式的预览安全配置，不是允许使用任意开发默认值。任何一项漂移都使本记录失效。

## 3. Secret 隔离

- PostgreSQL 密码和 DSN 通过 Compose Secret 文件提供，不作为普通环境变量提交。
- Bootstrap 密码仅在首次创建管理员时通过 Secret 文件挂载；成功登录后已删除，并且正常启动不再加载 Bootstrap overlay。
- Secret 目录默认忽略全部内容，只允许提交 README 和 .gitignore。
- Secret 文件使用 0600 权限；应用所需文件仅交给容器 UID 10001 读取。
- 仓库模板、镜像构建参数、前端包和公开文档不包含真实密码、Token 或模型 Key。

真实 Secret 内容、账号邮箱、Cookie、数据库连接串和服务器日志不得加入 Issue、PR 或 Git。

## 4. 证据与边界

- docs/preview-rollout-result.md 记录部署版本、健康检查、外部 HTTPS 检查及用户人工确认。
- FORGEFLOW_PERSONAL_PREVIEW_PLAN.md 记录部署方式、安全组和 Bootstrap 清理历史。
- deploy/personal-preview/compose.yaml 使用文件型 PostgreSQL Secret。
- deploy/personal-preview/compose.public-ip.yaml 固定 HTTPS、Secure Cookie、Origin、443 和证书卷。
- deploy/personal-preview/secrets/README.md 记录创建、权限和清理步骤。
- scripts/preview_rollout_preflight.py 提供真实运行时只读复核。
- scripts/validate-personal-preview-readiness.ps1 提供仓库级防漂移检查。

本记录基于已有部署证据，不表示本次 PR 重新连接、部署或修改了服务器。以下变化均要求重新验证：候选 SHA、入口 IP、证书方式、Compose overlay、Secret 注入、账号 Bootstrap、运行模式或仓库挂载。

## 5. 尚未关闭

- PERSONAL-004：PostgreSQL 备份与隔离恢复。
- PERSONAL-005：最小可观测性与回滚演练。
- PERSONAL-006：候选功能 smoke。
- PERSONAL-007：当前候选的受限用户端到端验收。
- PERSONAL-008：不可信仓库执行边界。
