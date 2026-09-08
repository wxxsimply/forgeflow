[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot

function Assert-Operations {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Read-Required {
    param([string]$RelativePath)
    $path = Join-Path $workspace $RelativePath
    Assert-Operations (Test-Path -LiteralPath $path -PathType Leaf) "Missing operations asset: $RelativePath"
    return Get-Content -Raw -LiteralPath $path
}

$powerShellFiles = @(
    'scripts/staging-alert-test.ps1',
    'scripts/staging-backup.ps1',
    'scripts/staging-restore-drill.ps1',
    'scripts/staging-security-drill.ps1',
    'scripts/staging-rollback.ps1',
    'scripts/demo-staging.ps1'
)
foreach ($relativePath in $powerShellFiles) {
    $tokens = $null
    $errors = $null
    [void][Management.Automation.Language.Parser]::ParseFile((Join-Path $workspace $relativePath), [ref]$tokens, [ref]$errors)
    Assert-Operations ($errors.Count -eq 0) "$relativePath has a PowerShell parse error: $($errors[0].Message)"
}

$alerts = Read-Required 'deploy/staging/prometheus/alerts.yaml'
$alertScript = Read-Required 'scripts/staging-alert-test.ps1'
$operations = Read-Required 'docs/operations.md'
$ruleNames = @([regex]::Matches($alerts, '(?m)^\s*- alert:\s*([A-Za-z0-9]+)\s*$') | ForEach-Object { $_.Groups[1].Value } | Sort-Object)
$catalogNames = @([regex]::Matches($alertScript, "alert = '([A-Za-z0-9]+)'") | ForEach-Object { $_.Groups[1].Value } | Sort-Object)
Assert-Operations ($ruleNames.Count -eq 9) 'Prometheus must define the nine reviewed ForgeFlow alerts'
Assert-Operations (($ruleNames -join ',') -eq ($catalogNames -join ',')) 'Alert dry-run catalog must exactly match Prometheus rules'
Assert-Operations ($alertScript -match 'deliveryAttempted = \$false') 'Alert dry-run must record that delivery was not attempted'
Assert-Operations ($alertScript -match 'ConfirmNotification') 'Live alert injection must require explicit confirmation'
foreach ($match in [regex]::Matches($alerts, 'runbook:\s*docs/operations\.md#([a-z0-9-]+)')) {
    $title = ($match.Groups[1].Value -replace '-', ' ')
    Assert-Operations ($operations -match "(?im)^###\s+$([regex]::Escape($title))\s*$") "Missing alert Runbook heading: $($match.Groups[1].Value)"
}
$alertmanager = Read-Required 'deploy/staging/alertmanager/alertmanager.yaml'
Assert-Operations ($alertmanager -match 'url_file:\s*/run/secrets/alert_webhook_url') 'Alertmanager webhook must come from a Secret file'
Assert-Operations ($alertmanager -notmatch '(?m)^\s*url:\s*https?://') 'Alertmanager webhook URL must not be inline'

$backup = Read-Required 'deploy/staging/ops/backup.sh'
$restore = Read-Required 'deploy/staging/ops/restore-drill.sh'
foreach ($required in @('forgeflow.backup/v1', 'sha256sum', 'migrationVersion', 'sizeBytes', 'BACKUP_RETENTION_DAYS')) {
    Assert-Operations ($backup.Contains($required)) "Backup script is missing $required"
}
foreach ($required in @('forgeflow_restore_', 'restore-staging-drill', 'sha256sum -c', 'manifest_migration', 'manifest_size', 'restored_migration')) {
    Assert-Operations ($restore.Contains($required)) "Restore script is missing $required"
}
Assert-Operations ($restore -notmatch '(?i)\bdown\s+migrat') 'Restore drill must never execute a Down Migration'

$security = Read-Required 'scripts/staging-security-drill.ps1'
foreach ($package in @('internal/repository', 'internal/tool', 'internal/policy', 'internal/sandbox', 'internal/security', 'internal/governance', 'internal/config')) {
    Assert-Operations ($security.Contains($package)) "Security dry-run is missing $package tests"
}
Assert-Operations ($security -match 'ConfirmLiveDrill') 'Live security drill must require explicit confirmation'
Assert-Operations ($security -match 'no Staging service was contacted') 'Security dry-run must remain local'

$rollback = Read-Required 'scripts/staging-rollback.ps1'
foreach ($required in @('forgeflow.release/v2', 'validate-release-assets.ps1', 'forgeflow.rollback-plan/v2', 'forgeflow.deployment/v2', "@('run', '--rm', '--no-deps', 'migrate', 'db', 'check')", "databaseRollback = 'not-performed'")) {
    Assert-Operations ($rollback.Contains($required)) "Rollback script is missing contract: $required"
}
Assert-Operations ($rollback -match 'downMigration = \$false') 'Rollback dry-run must explicitly prohibit a Down Migration'
Assert-Operations ($rollback -notmatch "(?i)@\('run'[^\r\n]+\bdb\b[^\r\n]+\bdown\b") 'Rollback script contains a database-down command'

$demo = Read-Required 'scripts/demo-staging.ps1'
foreach ($required in @('ConfirmApprovals', 'ExpectedRelease', 'ExpectedGitCommit', 'four-approval safety bound', 'repositoryPathSha256', 'forgeflow.demo-evidence/v1')) {
    Assert-Operations ($demo.Contains($required)) "Demo script is missing contract: $required"
}
Assert-Operations ($demo -notmatch 'originalRepositoryPath') 'Demo evidence must not expose the original repository path'
Assert-Operations ($demo -match "Task.Length -lt 10") 'Demo task must have explicit size and secret guards'

Write-Host "ForgeFlow stage 7 operations asset contract passed: 9 alerts, isolated restore, security boundaries, v2 rollback, and sanitized Demo."
