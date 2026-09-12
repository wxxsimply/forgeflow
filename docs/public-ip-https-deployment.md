# ForgeFlow 公网 IP HTTPS 部署手册

> 目标地址：`https://39.102.136.31`
> 不需要自有域名
> 适用范围：个人公开演示，不代表 Production Ready

## 1. 方案说明

公网模式通过 `compose.public-ip.yaml` 叠加在现有个人预览 Compose 之上：

- TCP 443 对公网开放，由 Caddy 终止 TLS。
- TCP 8080 继续只绑定 `127.0.0.1`，用于服务器本机健康检查，并在关闭公网 overlay 后恢复 SSH 隧道模式。
- Caddy 使用 Let's Encrypt `shortlived` ACME profile 为公网 IPv4 申请证书。
- ACME 只使用 TLS-ALPN-01，因此不需要开放或占用 TCP 80。
- API 启用 Secure Cookie，并把允许来源限制为当前 HTTPS 公网 IP。
- `default_sni` 指向该公网 IP，确保不发送 SNI 的 IP 直连客户端也能取得正确证书；不能只验证带 `-servername` 的连接。
- Caddy 的 `/data` 和 `/config` 使用独立持久卷，证书、ACME 账户和续期状态不会因容器重建丢失。

IP 地址证书有效期较短，Caddy 必须持续运行并能从公网接收 443 上的 ACME 验证请求。不得执行带 `-v` 的 `docker compose down`。

## 2. 部署前停止条件

先登录服务器，但不要停止或删除现有 TTS 容器：

```bash
cd /srv/forgeflow/app
git status --short
sudo ss -lntp | grep -E ':(443)[[:space:]]'
timedatectl show -p NTPSynchronized --value
curl -fsS -o /dev/null https://acme-v02.api.letsencrypt.org/directory
```

要求：

- `git status --short` 为空。
- 443 没有监听进程；如果命令显示任何进程，立即停止，不要强行结束该进程。
- 时间同步结果为 `yes`。
- Let's Encrypt directory 请求成功。

如果 443 已被其他服务占用，需要先设计共享入口代理，不能直接启动本 overlay。

## 3. 配置非敏感环境变量

编辑被 Git 忽略的服务器文件：

```bash
nano deploy/personal-preview/preview.env
```

加入：

```dotenv
FORGEFLOW_PUBLIC_IP=39.102.136.31
FORGEFLOW_ACME_EMAIL=<你的有效联系邮箱>
```

邮箱用于 ACME 账户联系，不是 ForgeFlow 密码。不要把密码、API Key 或私钥写入 `preview.env`。

校验合并后的 Compose：

```bash
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.public-ip.yaml \
  config --quiet
```

失败时不得继续。

## 4. 人工配置阿里云安全组

确认第 2 节全部通过后，在阿里云安全组中添加：

```text
授权策略：允许
协议：TCP
来源：0.0.0.0/0
目的端口：443/443
描述：ForgeFlow public HTTPS
```

继续保留：

- 8080 的公网拒绝规则。
- PostgreSQL、Worker metrics 和其他内部端口的公网拒绝状态。
- SSH 22 仅允许可信 IP/CIDR。

本方案不需要调整 TCP 80，也不需要开放 UDP 443。

如果 Ubuntu UFW 已启用，还需要执行：

```bash
sudo ufw status
sudo ufw allow 443/tcp
```

只有 `ufw status` 显示 active 时才需要第二条命令。

## 5. 启动公网 HTTPS

在服务器执行：

```bash
cd /srv/forgeflow/app
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.public-ip.yaml \
  up -d --build
```

查看状态和 Caddy 日志：

```bash
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.public-ip.yaml \
  ps

docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.public-ip.yaml \
  logs --tail 200 caddy
```

首次签发需要短暂等待。Caddy 必须变为 healthy，日志中不能持续出现 ACME authorization 或 certificate obtain 错误。

## 6. 验证

先在服务器验证私有回退入口：

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

再从服务器之外的网络验证公网 HTTPS。Windows PowerShell：

```powershell
Invoke-RestMethod https://39.102.136.31/healthz
```

检查证书确实包含该 IP：

```bash
openssl s_client \
  -connect 39.102.136.31:443 \
  -noservername \
  -verify_ip 39.102.136.31 \
  -verify_return_error </dev/null
```

最后在浏览器打开：

```text
https://39.102.136.31
```

浏览器不应出现证书警告。登录、Mock Run 和注销均应重新验收。

## 7. 账号限制

公网可达不等于开放注册。当前 ForgeFlow 没有访客自助注册页面，未持有账号的人只能看到登录页面。

- 不要把管理员密码共享给其他人。
- 如需让其他人实际登录，应先实现或增加受控的用户创建流程，为每个人创建独立账号。
- 公网 overlay 启用了 Secure Cookie；启用期间只通过 `https://39.102.136.31` 登录，不把 HTTP 8080 当作并行登录入口。
- 个人预览仍不得处理真实用户敏感数据，也不承担生产可用性承诺。

## 8. 自动续期与维护

Caddy 会使用持久化的 `caddy-public-data` 卷保存证书并自动续期。为了避免约 6 天的 IP 证书过期：

- 443 必须持续可从公网访问。
- 不要删除 `caddy-public-data` 和 `caddy-public-config` 卷。
- 不要执行 `docker compose down -v`。
- 定期检查 Caddy 日志是否存在持续续期错误。
- 如果公网 IP 改变，必须更新 `FORGEFLOW_PUBLIC_IP`、API Origin 和证书，并重新完成验收。

## 9. 回退到 SSH 隧道模式

如果证书签发、登录或安全验证失败，先从阿里云安全组删除公网 443 允许规则，然后在服务器执行：

```bash
cd /srv/forgeflow/app
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.public-ip.yaml \
  down

docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  up -d
```

不要添加 `-v`。PostgreSQL、Artifact 和 ACME 数据卷会保留。

回退后确认：

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

并恢复使用 SSH 隧道访问。

## 10. 参考资料

- [Let's Encrypt：IP 地址与六天证书](https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability)
- [Caddy：TLS ACME issuer 与 profile](https://caddyserver.com/docs/caddyfile/directives/tls)
- [Docker Compose：overlay 的 !override 合并规则](https://docs.docker.com/reference/compose-file/merge/)
