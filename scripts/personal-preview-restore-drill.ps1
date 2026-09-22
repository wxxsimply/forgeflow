[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)] [string]$BackupFile,
    [string]$RestoreDatabase = "forgeflow_preview_restore_drill",
    [string]$EnvFile = "deploy/personal-preview/preview.env",
    [Parameter(Mandatory = $true)] [switch]$ConfirmRestore,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
if (-not $ConfirmRestore) { throw "Personal preview restore drill requires -ConfirmRestore" }
if ($RestoreDatabase -notmatch '^forgeflow_preview_restore_[a-zA-Z0-9_]+$') { throw "Restore database must start with forgeflow_preview_restore_" }
if ($BackupFile -notmatch '^/backups/forgeflow-preview-[0-9]{8}T[0-9]{6}Z\.dump$') { throw "BackupFile must use /backups/forgeflow-preview-<UTC timestamp>.dump" }
$workspace = Split-Path -Parent $PSScriptRoot
$compose = Join-Path $workspace "deploy/personal-preview/compose.yaml"
$backupCompose = Join-Path $workspace "deploy/personal-preview/compose.backup.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
if (-not (Test-Path -LiteralPath $resolvedEnv -PathType Leaf)) { throw "Personal preview env file does not exist: $resolvedEnv" }

if ($DryRun) {
    Push-Location (Join-Path $workspace "deploy/personal-preview")
    try {
        docker compose --env-file $resolvedEnv -f $compose -f $backupCompose --profile ops config --quiet
        if ($LASTEXITCODE -ne 0) { throw "Personal preview restore Compose validation failed" }
    }
    finally { Pop-Location }
    [ordered]@{
        schemaVersion = 'forgeflow.preview-restore-dry-run/v1'
        backupFile = $BackupFile
        checksumFile = "$BackupFile.sha256"
        manifestFile = "$BackupFile.manifest"
        restoreDatabase = $RestoreDatabase
        destructiveScope = 'isolated-personal-preview-restore-database-only'
        downMigration = $false
        executed = $false
    } | ConvertTo-Json
    Write-Host "Personal preview restore dry-run passed; no database command was executed."
    return
}

$previousBackup = $env:FORGEFLOW_PREVIEW_BACKUP_FILE
$previousDatabase = $env:FORGEFLOW_PREVIEW_RESTORE_DATABASE
$previousConfirmation = $env:FORGEFLOW_PREVIEW_CONFIRM_RESTORE
try {
    $env:FORGEFLOW_PREVIEW_BACKUP_FILE = $BackupFile
    $env:FORGEFLOW_PREVIEW_RESTORE_DATABASE = $RestoreDatabase
    $env:FORGEFLOW_PREVIEW_CONFIRM_RESTORE = "restore-personal-preview-drill"
    Push-Location (Join-Path $workspace "deploy/personal-preview")
    try {
        docker compose --env-file $resolvedEnv -f $compose -f $backupCompose --profile ops run --rm restore-drill
        if ($LASTEXITCODE -ne 0) { throw "Personal preview restore drill failed" }
    }
    finally { Pop-Location }
}
finally {
    $env:FORGEFLOW_PREVIEW_BACKUP_FILE = $previousBackup
    $env:FORGEFLOW_PREVIEW_RESTORE_DATABASE = $previousDatabase
    $env:FORGEFLOW_PREVIEW_CONFIRM_RESTORE = $previousConfirmation
}
