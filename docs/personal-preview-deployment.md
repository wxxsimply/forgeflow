# ForgeFlow 个人预览部署手册

> 服务器：`39.102.136.31`  
> 访问方式：SSH 隧道  
> 适用范围：个人演示，不是 Production

## 1. 人工前置条件

准备 SSH 用户名、SSH 端口和本机私钥路径。不要把私钥、密码或云平台 Token 发送到聊天。

服务器需要 Linux/amd64、Git、Docker Engine、Docker Compose v2、OpenSSL，以及足够的磁盘空间用于本地构建。

云安全组不应开放 8080。SSH 应限制为可信来源并使用密钥认证。

## 2. 获取冻结源码

在服务器执行：

```bash
sudo mkdir -p /srv/forgeflow/app /srv/forgeflow/repositories
sudo chown -R "$USER":"$USER" /srv/forgeflow
git clone https://github.com/wxxsimply/forgeflow.git /srv/forgeflow/app
cd /srv/forgeflow/app
git checkout <approved-40-character-git-sha>
git status --short
```

`git status --short` 必须为空。个人预览只向 `/srv/forgeflow/repositories` 放置不含敏感数据的测试仓库副本。

## 3. 配置非敏感环境变量

```bash
cd /srv/forgeflow/app
cp deploy/personal-preview/preview.env.example deploy/personal-preview/preview.env
```

把 `FORGEFLOW_GIT_COMMIT` 改为本批部署资产 PR 合并后的 40 位 commit，并把 `FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL` 改为自己的管理员邮箱。`preview.env` 不得包含密码或 API Key。

默认使用 Go 官方模块代理与校验数据库。如果服务器无法连接官方端点，可以在 `preview.env` 中显式设置受信任的镜像，例如：

```dotenv
GOPROXY=https://goproxy.cn,direct
GOSUMDB="sum.golang.org https://goproxy.cn/sumdb/sum.golang.org"
```

不得使用 `GOSUMDB=off`；代理选择属于部署环境配置，不应改变仓库默认供应链策略。

个人预览保持 `FORGEFLOW_INSTALL_DOCKER_CLI=false`，与已关闭的 Docker Sandbox 一致；只有明确启用 Docker 执行面时才允许改为 `true`。

## 4. 手动创建 Secret

严格按照 `deploy/personal-preview/secrets/README.md` 操作。Secret 文件必须为普通文件、权限 `0600`，且不能提交 Git。

## 5. 验证 Compose 配置

```bash
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.bootstrap.yaml \
  config --quiet
```

失败时不要启动服务。

## 6. 首次启动

```bash
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.bootstrap.yaml \
  up -d --build

docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.bootstrap.yaml \
  ps

curl -fsS http://127.0.0.1:8080/healthz
```

## 7. 从本机访问

在 Windows PowerShell 执行：

```powershell
ssh -L 8080:127.0.0.1:8080 <ssh-user>@39.102.136.31
```

保持终端开启，在浏览器访问 `http://127.0.0.1:8080`，然后使用 Bootstrap 管理员登录。

## 8. 删除 Bootstrap Secret

成功登录后在服务器停止 Bootstrap 配置、删除一次性密码，再启动普通配置：

```bash
cd /srv/forgeflow/app
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

## 9. 停止和查看日志

```bash
docker compose --env-file deploy/personal-preview/preview.env -f deploy/personal-preview/compose.yaml ps
docker compose --env-file deploy/personal-preview/preview.env -f deploy/personal-preview/compose.yaml logs --tail 200
docker compose --env-file deploy/personal-preview/preview.env -f deploy/personal-preview/compose.yaml down
```

日志不得复制到公开 Issue 或 Release，除非已经确认不含用户数据和敏感信息。

## 10. 限制

- 未配置 HTTPS，只允许 SSH 隧道访问。
- 镜像在服务器从源码构建，没有正式 SBOM、签名和 Registry digest。
- Planner 为 Mock，不调用 DeepSeek。
- Docker Sandbox 关闭，不执行真实代码任务。
- 没有 Production SLO、值班、独立安全评审或恢复保证。
