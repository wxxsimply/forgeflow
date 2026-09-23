# ForgeFlow 个人预览观测与回滚

> 状态：PERSONAL-005 实施资产已就绪，真实定时观测和应用回滚演练尚未执行，因此 PERSONAL-005 仍未完成。
>
> 本手册适用于个人项目受限预览，不代表 24/7 值班、生产 SLO 或高可用保证。启用服务器定时器或执行真实回滚前，必须由项目所有者单独确认。

## 1. 最小观测范围

`scripts/personal_preview_operations.py observe` 每次只生成脱敏聚合快照：

- 服务器受跟踪源码是否干净并匹配私有部署 SHA，API、Worker、Web、Caddy、PostgreSQL 是否同时为 `running/healthy`；
- 本机代理后的 API/Web 版本和 Worker readiness 是否匹配私有 `preview.env` 中的 Release 和完整 Git SHA；
- 最近 15 分钟、最多 500 行日志中的错误行数量，不保存或打印原始日志；
- PostgreSQL 当前数据库的聚合字节数，不读取任何业务行；
- `/srv/forgeflow` 磁盘使用率和最近个人预览备份的新鲜度；
- API/Worker 是否仍为 Planning/Mock、没有模型 Key、关闭 Docker 执行并保持 HTTPS 会话边界。

快照不会包含 Cookie、密码、DSN、模型 Key、任务正文、仓库内容、原始日志或备份文件名。文件使用 `0600`，目录使用 `0700`，且拒绝符号链接输出目录。

观测与回滚计划都只接受普通的私有环境文件；Linux 上环境文件必须为 `0600`（不能被组或其他用户读取），并拒绝符号链接，避免私有部署记录被替换或意外指向其他文件。

当前个人预览为 Mock Planner，工具只能证明“按配置预期外部模型调用 0 次、费用 0 USD”，不能读取第三方账单。如果以后启用真实 Provider，必须先另行实现平台账单核对和费用上限，不能继续把配置预期当作实际费用证据。

## 2. 手动只读检查

在服务器 `/srv/forgeflow/app` 执行：

```bash
python3 -B scripts/personal_preview_operations.py observe \
  --env-file deploy/personal-preview/preview.env \
  --backup-directory /srv/forgeflow/backups \
  --data-path /srv/forgeflow \
  --output-directory /srv/forgeflow/operations/observations
```

返回码 `0` 表示本次瞬时快照全部通过；`1` 表示需要人工处理。原始日志只能在服务器私下查看，不能为了排错把完整日志复制到 PR、Issue 或聊天。没有最近 26 小时备份时，`backup-freshness` 会失败，这是预期的安全阻断，不应删除检查来制造通过。

## 3. 启用 15 分钟定时观测

获得项目所有者明确授权后，先创建私有目录并审核 unit 中的用户名、路径和 Python/Docker 权限：

```bash
sudo install -d -o forgeflow -g forgeflow -m 0700 /srv/forgeflow/operations/observations
sudo install -m 0644 deploy/personal-preview/systemd/forgeflow-preview-observe.service /etc/systemd/system/
sudo install -m 0644 deploy/personal-preview/systemd/forgeflow-preview-observe.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now forgeflow-preview-observe.timer
sudo systemctl start forgeflow-preview-observe.service
systemctl list-timers forgeflow-preview-observe.timer
sudo systemctl status forgeflow-preview-observe.service --no-pager
```

至少观察 24 小时。处理失败时先查看脱敏快照的 `attentionRequired`；只有确有必要时才在服务器私下查看限定时间范围的原始日志。观测记录位于 `/srv/forgeflow/operations/observations`，不得提交 Git。

## 4. 保存可回滚版本

每次成功部署后，在服务器私有目录保存当时的非 Secret 环境文件，并保留三个本地镜像。文件仍可能包含基础设施信息，因此权限保持 `0600`：

```bash
sudo install -d -o forgeflow -g forgeflow -m 0700 /srv/forgeflow/operations/releases
install -m 0600 deploy/personal-preview/preview.env \
  /srv/forgeflow/operations/releases/<release>.env
docker image inspect forgeflow-api:<release> forgeflow-worker:<release> forgeflow-web:<release> >/dev/null
```

不要运行镜像清理命令删除当前版本或上一个已验证版本。私有版本记录必须包含部署 UTC、完整 Git SHA、Release、三个 image ID、Migration 版本以及对应备份时间；这些数据不提交公开仓库。

## 5. 只读生成回滚计划

下面的命令只解析 Compose、检查边界，并核对旧镜像的 OCI version/revision 标签与不可变 image ID；它不会停止或启动服务：

```bash
python3 -B scripts/personal_preview_operations.py rollback-plan \
  --current-env deploy/personal-preview/preview.env \
  --target-env /srv/forgeflow/operations/releases/<previous-release>.env
```

只有 `checksPassed=true` 时才允许进入人工维护窗口。目标环境文件必须为 `0600`，必须保持同一受控仓库挂载，并继续使用 Planning/Mock、Secure Cookie、HTTPS Origin、关闭 Docker 和零模型 Key。

## 6. 应用回滚步骤

真实回滚是有停机影响的外部操作，必须再次获得明确授权。开始前：

1. 运行 `preview_rollout_preflight.py --phase before`，确认没有未结束任务、队列或 Outbox；
2. 确认最近备份成功且 PERSONAL-004 的隔离恢复证据可用；
3. 保存当前 `preview.env`、实际 image ID 和本轮观测快照；
4. 确认目标版本没有要求 down migration。

在批准的维护窗口中使用目标环境文件，但始终保留公网 HTTPS overlay：

```bash
TARGET_ENV=/srv/forgeflow/operations/releases/<previous-release>.env
fftarget() {
  docker compose --env-file "$TARGET_ENV" \
    -f deploy/personal-preview/compose.yaml \
    -f deploy/personal-preview/compose.public-ip.yaml "$@"
}
fftarget config --quiet
fftarget run --rm --no-deps migrate db check
fftarget stop -t 90 worker api
fftarget up -d --no-deps --no-build api worker web
python3 -B scripts/personal_preview_operations.py observe \
  --env-file "$TARGET_ENV" \
  --backup-directory /srv/forgeflow/backups \
  --data-path /srv/forgeflow \
  --output-directory /srv/forgeflow/operations/observations
```

失败时停止扩大操作，使用保存的当前环境文件和同样的 `up -d --no-deps --no-build api worker web` 恢复原应用版本。任何情况下都不得执行 `docker compose down`、`down -v`、卷删除、数据库覆盖恢复或 down migration。回滚只切换 API、Worker、Web 应用镜像，不删除 PostgreSQL、Artifact、Workspace、证书或备份。

## 7. PERSONAL-005 完成记录

只有以下真实结果齐全后，才能把计划状态改为“已完成”：

- 定时器 active，并连续至少 24 小时生成权限正确的脱敏快照；
- 错误、健康、数据库容量、磁盘、备份和 Mock 零费用边界均已人工核对；
- 当前和上一个可用版本的完整 SHA、Release、image ID 与私有环境记录齐全；
- 在维护窗口完成一次应用回滚及恢复原版本演练，健康与版本核对成功；
- 演练没有 down migration、数据库覆盖、卷删除、Secret 或原始日志泄露。

本次 PR 只证明观测和回滚防护已准备好，不证明服务器定时器或回滚演练已经发生。
