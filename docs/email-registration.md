# 邮箱验证码注册（个人预览）

新用户先在 `/register` 输入邮箱并获取 8 位验证码，收到邮件后在 10 分钟内输入验证码、设置至少 12 字符的密码。验证码一次性使用；同一邮箱 60 秒内不重复发送，连续错误 5 次后须重新获取。网页不会自动登录。现有账号仍可照常登录，不会追溯要求重新验证。

服务端始终要求验证码；直接调用 `POST /api/v1/auth/register` 不能绕过。验证码仅以独立密钥 HMAC 摘要形式存入 PostgreSQL，过期记录在后续发码时清理。没有配置 SMTP 时，发码和注册接口返回 503，不会退回到无验证码注册。

## 部署前准备

1. 准备一个支持 SMTP AUTH 且启用 TLS 的发信邮箱。只支持 465（隐式 TLS）或 587（STARTTLS）；不允许明文 SMTP。发件地址须与服务商允许的地址一致。确认服务器能连通该端口，并取得该邮箱的 SMTP 授权码；不要把授权码发到聊天、工单或 Git 仓库。
2. 在服务器的私有目录创建 `registration-smtp-password` 和 `registration-code-key` 两个文件。前者存放 SMTP 授权码，后者存放独立随机 32 字节密钥的标准 Base64 编码。密钥可在服务器私下用 `umask 077; openssl rand -base64 32 > /srv/forgeflow/secrets/registration-code-key` 生成。文件型 Compose secret 在个人预览中由非 root 的 API 用户 `10001:10001` 读取；挂载前核对实际权限，建议文件归 `10001:10001` 且权限为 `0400`，不要只让宿主机的 `forgeflow` 用户可读。不要复用 MFA 或审计密钥，也不要在部署之间轮换，否则未使用的验证码会失效。
3. 在私有 `deploy/personal-preview/preview.env` 中设置 `FORGEFLOW_SMTP_HOST`、`FORGEFLOW_SMTP_PORT`、`FORGEFLOW_SMTP_FROM`、`FORGEFLOW_SMTP_USER`、`FORGEFLOW_SMTP_PASSWORD_PATH`、`FORGEFLOW_REGISTRATION_CODE_KEY_PATH`，参照 `preview.env.example`。私密文件路径仅供 Compose 读取，文件内容不得写入 env 或提交。
4. 发布时在原有 Compose 文件后追加 `-f deploy/personal-preview/compose.email.yaml`。这个可选 overlay 只给 API 提供 SMTP 配置和私有文件，不向 worker/web 暴露授权码。

此次新增 `000008_registration_codes` 数据库迁移。上线前先备份并校验数据库，然后用新镜像显式运行 Compose 的 `migrate` 服务（其命令为 `db migrate`），确认 schema version 为 8，再启动新 API。不能在未运行迁移时直接切换，因为 API 会拒绝旧 schema。服务器内存较小，镜像应在本机或 CI 构建后传入，不要在服务器运行 `docker compose build`。

部署后检查：API、worker、web、Caddy、PostgreSQL 健康；公网 HTTPS 的 `/healthz` 与 `/release.json` 提交号一致；向自己控制的邮箱发送一次验证码并完成注册/登录；错误验证码、过期验证码、重复使用和未配置 SMTP 均不能创建账号。不要把实际验证码、SMTP 授权码、完整 env 文件或数据库连接串贴到 PR。

回滚注意：旧版 API 只接受 schema version 7，不能直接运行在迁移后的 version 8 上。若新版本异常，优先停止新 API 并修复/前进发布；若必须回滚到 version 7，先保留迁移后数据备份，再由维护者审查并执行 `000008_registration_codes.down.sql`，删除 `schema_migrations` 中的 version 8 记录后恢复旧镜像。降级仅会丢弃尚未使用的验证码，已创建用户不受影响；不得自动回滚数据库。
