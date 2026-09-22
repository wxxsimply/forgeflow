# ForgeFlow 个人预览备份与隔离恢复

> 状态：PERSONAL-004 实施资产已就绪，真实备份、定时器启用和隔离恢复尚未执行，因此 PERSONAL-004 仍未完成。
>
> 安全边界：数据库导出包含用户数据，不得提交 Git、上传到公开位置或粘贴到聊天。执行真实备份或恢复前，必须由项目所有者单独确认。

## 1. 已实现的保护

个人预览使用独立的 `compose.backup.yaml`，不会改变长期运行服务。备份服务：

- 使用 PostgreSQL custom format，并在发布文件前执行 `pg_restore --list`；
- 同时生成 SHA-256 和 `forgeflow.preview-backup/v1` manifest；
- manifest 记录 UTC 时间、源数据库、Migration 版本、校验和与字节数；
- 临时文件验证成功后才原子重命名，文件权限设为 `0600`；
- 只清理符合 `forgeflow-preview-*.dump` 命名且超过保留期的本项目备份。

恢复服务只接受 `/backups/forgeflow-preview-<UTC>.dump`，并要求目标数据库匹配 `forgeflow_preview_restore_*`。它会先验证 checksum、manifest、大小与 Migration 版本，再创建隔离数据库；不能覆盖在线 `forgeflow` 数据库，也不执行 down migration。

## 2. 首次启用前检查

以下命令在服务器 `/srv/forgeflow/app` 执行。先确认 checkout 是已经审核合并的提交，并确认 `preview.env` 与 PostgreSQL Secret 仍为服务器私有文件。服务器不要求安装 PowerShell。

先查询部署用户的数字 UID/GID，把真实结果写入被 Git 忽略的 `deploy/personal-preview/preview.env`；不要直接假定示例值 `1000`：

```bash
id -u forgeflow
id -g forgeflow
printf '\nFORGEFLOW_BACKUP_UID=%s\nFORGEFLOW_BACKUP_GID=%s\n' "$(id -u forgeflow)" "$(id -g forgeflow)" >> deploy/personal-preview/preview.env
```

如果文件中已经存在这两个键，应编辑原值而不是重复追加。备份和恢复容器用同一 UID/GID 运行，因而只能读取部署用户持有的 `postgres_password`，生成的 `0600` 备份也继续由部署用户持有。

```bash
sudo install -d -o forgeflow -g forgeflow -m 0700 /srv/forgeflow/backups
export FORGEFLOW_BACKUP_PATH=/srv/forgeflow/backups
export FORGEFLOW_PREVIEW_BACKUP_FILE=/backups/forgeflow-preview-20260922T000000Z.dump
export FORGEFLOW_PREVIEW_RESTORE_DATABASE=forgeflow_preview_restore_drill
export FORGEFLOW_PREVIEW_CONFIRM_RESTORE=restore-personal-preview-drill
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.backup.yaml \
  --profile ops config --quiet
```

上述 dry-run 只校验 Compose，不连接数据库、不创建导出、不删除或创建数据库。Windows 工作站和 GitHub CI 还可使用 `scripts/personal-preview-backup.ps1 -DryRun` 与 `scripts/personal-preview-restore-drill.ps1 -ConfirmRestore -DryRun` 校验输入保护。

## 3. 创建并核对真实备份

获得项目所有者明确授权后执行：

```bash
export FORGEFLOW_BACKUP_PATH=/srv/forgeflow/backups
export BACKUP_RETENTION_DAYS=14
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.backup.yaml \
  --profile ops run --rm backup
ls -l /srv/forgeflow/backups/forgeflow-preview-*.dump*
cd /srv/forgeflow/backups
sha256sum -c forgeflow-preview-<UTC>.dump.sha256
```

把 `<UTC>` 替换成命令输出的实际时间戳。检查 `.manifest` 中的 `databaseName=forgeflow`、Migration 版本、SHA-256 和大小；不要把 manifest 或文件名当作备份内容已恢复成功的证据。

至少保留一份不在同一主机上的加密副本，或启用云平台的加密磁盘快照。离线副本的位置、加密方式和恢复密钥只记录在私有发布记录中。

## 4. 启用每日自动备份

先审核 unit 中的用户、工作目录、Docker 路径和备份目录，再安装：

```bash
sudo install -m 0644 deploy/personal-preview/systemd/forgeflow-preview-backup.service /etc/systemd/system/
sudo install -m 0644 deploy/personal-preview/systemd/forgeflow-preview-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now forgeflow-preview-backup.timer
systemctl list-timers forgeflow-preview-backup.timer
sudo systemctl start forgeflow-preview-backup.service
sudo systemctl status forgeflow-preview-backup.service --no-pager
```

定时器默认每天 UTC 03:15 运行，并加入最多 15 分钟随机延迟。只有在手动触发成功、目录中出现三件套文件且 checksum 通过后，才能记录为“自动备份已启用”。

## 5. 隔离恢复演练

使用刚生成且已核对的文件；该命令会删除并重建同名的 `forgeflow_preview_restore_*` 隔离数据库，因此必须显式传入确认开关：

```bash
export FORGEFLOW_BACKUP_PATH=/srv/forgeflow/backups
export FORGEFLOW_PREVIEW_BACKUP_FILE=/backups/forgeflow-preview-<UTC>.dump
export FORGEFLOW_PREVIEW_RESTORE_DATABASE=forgeflow_preview_restore_20260922
export FORGEFLOW_PREVIEW_CONFIRM_RESTORE=restore-personal-preview-drill
docker compose \
  --env-file deploy/personal-preview/preview.env \
  -f deploy/personal-preview/compose.yaml \
  -f deploy/personal-preview/compose.backup.yaml \
  --profile ops run --rm restore-drill
```

成功输出必须包含隔离数据库名和与 manifest 相同的 Migration 版本。随后只读核对预期测试数据；不得把应用切换到该数据库，也不得将在线数据库作为 `RestoreDatabase`。

## 6. PERSONAL-004 完成记录

只有以下项目都具备真实结果后，才把计划中的 PERSONAL-004 改为“已完成”：

- 候选完整 Git SHA 与执行 UTC 日期；
- 自动定时器为 active，最近一次执行成功；
- 备份文件、checksum 和 manifest 均存在且权限正确；
- 备份文件由 `forgeflow` 部署用户持有，未因容器 root 产生不可管理文件；
- 至少一份异机加密副本或加密快照；
- 隔离数据库恢复成功，Migration 版本和预期测试数据核对通过；
- 全程未覆盖在线数据库、未执行 down migration、未泄露 Secret 或备份内容。

本次仓库变更只证明工具与防护已准备好，不证明上述真实操作已经发生。
