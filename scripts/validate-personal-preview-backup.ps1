[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot

function Assert-PersonalPreviewBackup {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

$assets = [ordered]@{
    compose = 'deploy/personal-preview/compose.backup.yaml'
    backup = 'deploy/personal-preview/ops/backup.sh'
    restore = 'deploy/personal-preview/ops/restore-drill.sh'
    backupWrapper = 'scripts/personal-preview-backup.ps1'
    restoreWrapper = 'scripts/personal-preview-restore-drill.ps1'
    integrationTest = 'scripts/test-personal-preview-backup.sh'
    service = 'deploy/personal-preview/systemd/forgeflow-preview-backup.service'
    timer = 'deploy/personal-preview/systemd/forgeflow-preview-backup.timer'
    ignore = 'deploy/personal-preview/backups/.gitignore'
    runbook = 'docs/personal-preview-backup-restore.md'
    plan = 'FORGEFLOW_FORMAL_LAUNCH_PLAN.md'
    workflow = '.github/workflows/deployment.yml'
}
foreach ($entry in $assets.GetEnumerator()) {
    $path = Join-Path $workspace $entry.Value
    Assert-PersonalPreviewBackup (Test-Path -LiteralPath $path -PathType Leaf) "Personal preview backup asset is missing: $($entry.Value)"
}

$compose = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.compose)
foreach ($contract in @('profiles: ["ops"]', 'FORGEFLOW_BACKUP_UID is required', 'FORGEFLOW_BACKUP_GID is required', 'POSTGRES_PASSWORD_FILE: /run/secrets/postgres_password', '${FORGEFLOW_BACKUP_PATH:-./backups}:/backups', 'read_only: true', 'no-new-privileges:true', 'cap_drop:', '/backups:ro')) {
    Assert-PersonalPreviewBackup ($compose.Contains($contract)) "Personal preview backup Compose is missing contract: $contract"
}
Assert-PersonalPreviewBackup ($compose -notmatch '(?m)^\s+ports:') 'Backup services must not publish ports'

$backup = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.backup)
foreach ($contract in @('pg_dump --format=custom', 'pg_restore --list', 'forgeflow.preview-backup/v1', 'databaseName=forgeflow', 'migrationVersion=', 'sha256=', 'sizeBytes=', 'chmod 600', 'BACKUP_RETENTION_DAYS', 'forgeflow-preview-*.dump')) {
    Assert-PersonalPreviewBackup ($backup.Contains($contract)) "Personal preview backup script is missing contract: $contract"
}

$restore = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.restore)
foreach ($contract in @('^forgeflow_preview_restore_', 'restore-personal-preview-drill', 'sha256sum -c', 'forgeflow.preview-backup/v1', 'manifest_migration', 'DROP DATABASE IF EXISTS', 'CREATE DATABASE', 'pg_restore --no-owner --no-privileges --exit-on-error')) {
    Assert-PersonalPreviewBackup ($restore.Contains($contract)) "Personal preview restore script is missing contract: $contract"
}
foreach ($forbidden in @('DROP DATABASE forgeflow', 'CREATE DATABASE forgeflow;', 'migrate down')) {
    Assert-PersonalPreviewBackup (-not $restore.Contains($forbidden)) "Personal preview restore script contains forbidden online/down-migration operation: $forbidden"
}

$backupWrapper = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.backupWrapper)
foreach ($contract in @('compose.backup.yaml', '[switch]$DryRun', '--profile ops config --quiet', '--profile ops run --rm backup')) {
    Assert-PersonalPreviewBackup ($backupWrapper.Contains($contract)) "Personal preview backup wrapper is missing contract: $contract"
}
$restoreWrapper = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.restoreWrapper)
foreach ($contract in @('forgeflow_preview_restore_', '[switch]$ConfirmRestore', '[switch]$DryRun', 'downMigration = $false', 'executed = $false', 'restore-personal-preview-drill')) {
    Assert-PersonalPreviewBackup ($restoreWrapper.Contains($contract)) "Personal preview restore wrapper is missing contract: $contract"
}

$service = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.service)
foreach ($contract in @('User=forgeflow', 'WorkingDirectory=/srv/forgeflow/app', 'FORGEFLOW_BACKUP_PATH=/srv/forgeflow/backups', 'compose.backup.yaml', '--profile ops run --rm backup', 'NoNewPrivileges=true', 'ReadWritePaths=/srv/forgeflow/backups')) {
    Assert-PersonalPreviewBackup ($service.Contains($contract)) "Personal preview backup service is missing contract: $contract"
}
$timer = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.timer)
foreach ($contract in @('OnCalendar=', 'Persistent=true', 'RandomizedDelaySec=', 'forgeflow-preview-backup.service')) {
    Assert-PersonalPreviewBackup ($timer.Contains($contract)) "Personal preview backup timer is missing contract: $contract"
}

$ignore = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.ignore)
Assert-PersonalPreviewBackup ($ignore -match '(?m)^\*$') 'Personal preview backup directory must ignore generated files'
Assert-PersonalPreviewBackup ($ignore -match '(?m)^!\.gitignore$') 'Personal preview backup directory must retain only its ignore rule'
$envExample = Get-Content -Raw -LiteralPath (Join-Path $workspace 'deploy/personal-preview/preview.env.example')
foreach ($contract in @('FORGEFLOW_BACKUP_UID=1000', 'FORGEFLOW_BACKUP_GID=1000')) {
    Assert-PersonalPreviewBackup ($envExample.Contains($contract)) "Personal preview env example is missing backup ownership contract: $contract"
}

$runbook = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.runbook)
foreach ($contract in @('PERSONAL-004 实施资产已就绪', 'PERSONAL-004 仍未完成', '尚未执行', '不得提交 Git', '不能覆盖在线 `forgeflow`', '不执行 down migration', '异机加密副本', 'id -u forgeflow', '容器 root')) {
    Assert-PersonalPreviewBackup ($runbook.Contains($contract)) "Personal preview backup runbook is missing evidence boundary: $contract"
}
$plan = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.plan)
Assert-PersonalPreviewBackup ($plan.Contains('| PERSONAL-004 | 仓库功能回归就绪，待服务器实测 |')) 'Formal launch plan must keep PERSONAL-004 pending real execution'

$workflow = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.workflow)
foreach ($contract in @('validate-personal-preview-backup.ps1', 'bash -n deploy/personal-preview/ops/backup.sh', 'personal-preview-backup.ps1', 'personal-preview-restore-drill.ps1', 'test-personal-preview-backup.sh')) {
    Assert-PersonalPreviewBackup ($workflow.Contains($contract)) "Deployment validation is missing personal preview backup check: $contract"
}

Write-Host 'ForgeFlow personal preview backup and isolated restore contract passed.'
