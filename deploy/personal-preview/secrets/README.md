# Personal preview secrets

此目录只提交说明文件。真实 Secret 必须由用户在服务器上手动创建，并保持 Git 忽略。

在仓库根目录执行：

```bash
umask 077
mkdir -p deploy/personal-preview/secrets
openssl rand -hex 24 > deploy/personal-preview/secrets/postgres_password
preview_password="$(cat deploy/personal-preview/secrets/postgres_password)"
printf 'postgres://forgeflow:%s@postgres:5432/forgeflow?sslmode=disable' "$preview_password" > deploy/personal-preview/secrets/postgres_dsn
unset preview_password
openssl rand -base64 32 > deploy/personal-preview/secrets/bootstrap_admin_password
chmod 600 deploy/personal-preview/secrets/postgres_password deploy/personal-preview/secrets/postgres_dsn deploy/personal-preview/secrets/bootstrap_admin_password
```

确认首次管理员登录成功后删除：

```bash
rm deploy/personal-preview/secrets/bootstrap_admin_password
```

不要在个人预览服务器上创建 DeepSeek/OpenAI Key。不要提交此目录中的真实文件。
