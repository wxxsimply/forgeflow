# 公网 IP HTTPS 部署记录

日期：2026-09-12。入口：https://39.102.136.31 。适用于个人 Mock 预览。

## 部署范围

- 用户确认安全组允许 TCP 443、UFW 为 inactive。
- 服务器源码 checkout 为 PR #49 的 `3ff97639df45728508ab814432f345daeab0cba4`。
- 应用代码未变化，复用 `0.1.0-preview.1` 镜像；健康接口的 `gitCommit` 仍为 `d5d5f5311bd812f511f009cfbc458f52ccbc6a17`，这是应用构建版本，不是部署配置版本。
- 为服务器忽略文件 `preview.env` 增加公网 IP 与 ACME 联系邮箱；原文件备份在服务器 `.cache/preview.env.before-public-ip`，权限为 0600。
- 以基础 Compose 加 `compose.public-ip.yaml` 启动，只重建 API 和 Caddy；PostgreSQL、Worker、Web 及 TTS 未重建。
- 新增证书持久卷 `caddy-public-data`、`caddy-public-config`，继续只在回环地址发布 8080。

## 已修复的问题

首次证书签发成功，但 Windows curl、Linux curl 和不带 SNI 的 OpenSSL 都收到 TLS internal error。显式发送 SNI 的握手证书验证通过，说明问题在证书选择，而不是证书链或安全组。

已在 Caddy 全局选项中增加 `default_sni {$FORGEFLOW_PUBLIC_IP}`，配置校验通过后重建 Caddy。服务器保留该文件的未提交补丁，本地 `codex/public-ip-default-sni` 分支含相同修复；需由仓库所有者手动提交、推送及合并。

这是 [Caddy 官方 default_sni 选项](https://caddyserver.com/docs/caddyfile/options#default-sni) 的适用场景。CI 新增对展开后的 TLS 连接策略的断言，防止只通过语法检查却漏掉 IP 直连失败。

## 自动验收

- 外部 Windows curl 直接访问 HTTPS，未使用 `-k`，健康接口返回 `status: ok`。
- 公网 `/login` 返回 200 与 CSP、X-Frame-Options、X-Content-Type-Options 等响应头。
- OpenSSL `-noservername -verify_ip 39.102.136.31 -verify_return_error` 握手验证通过。
- 证书签发机构：Let's Encrypt YE2；SAN 包含 IP `39.102.136.31`。
- 本次证书有效期（UTC）：2026-09-12 08:45:02 至 2026-09-19 00:45:01。
- Caddy 已记录自动续期窗口并持久化证书；尚未声称实际续期已完成。
- ForgeFlow 长期服务健康；TTS 保持原有运行状态及 80 端口映射。

## 待人工验收

- 在浏览器访问 `https://39.102.136.31`，确认无证书警告。
- 使用原有管理员账号完成登录、Mock Run、注销。
- 公网可打开登录页，但没有自助注册；不共享管理员密码。
- 由所有者手动提交并合并本次 SNI 修复与部署记录。

## 后续同步

合并修复后，先比较服务器工作区的 Caddy 补丁和合并提交对应文件，确认修复相同；保留补丁备份后再同步 checkout。不要直接用强制 reset 丢弃现场改动，也不要把未提交补丁误报为干净发布基线。

以后操作公网服务时，始终同时提供基础和公网两个 Compose 文件。省略公网 overlay 会恢复非 Secure Cookie 等私有模式配置。

```bash
docker compose --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.public-ip.yaml ps
```

如需回退到 SSH 隧道，按 [部署手册](./public-ip-https-deployment.md#9-回退到-ssh-隧道模式) 关闭公网入口并切回基础 Compose。不要删除数据库、Artifact 或证书卷。
