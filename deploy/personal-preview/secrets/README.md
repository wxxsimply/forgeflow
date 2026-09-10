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

API、Migration 和 Worker 镜像以 UID `10001` 运行。保持 Secret 为 `0600`，并只把应用需要读取的两个文件交给该 UID；`postgres_password` 继续由部署用户持有：

```bash
docker run --rm \
  --mount type=bind,src="$(pwd)/deploy/personal-preview/secrets",dst=/secrets \
  alpine:3.22 \
  chown 10001:10001 /secrets/postgres_dsn /secrets/bootstrap_admin_password
```

轮换或重新创建这两个文件后必须重新执行上述命令。不要通过 `chmod 644` 绕过容器读取错误。

确认首次管理员登录成功后删除：

```bash
rm deploy/personal-preview/secrets/bootstrap_admin_password
```

不要在个人预览服务器上创建 DeepSeek/OpenAI Key。不要提交此目录中的真实文件。
