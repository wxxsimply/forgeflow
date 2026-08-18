param(
    [Parameter(Mandatory = $true)] [string]$BackupFile,
    [string]$RestoreDatabase = "forgeflow_restore_drill",
    [string]$EnvFile = "deploy/staging/staging.env",
    [Parameter(Mandatory = $true)] [switch]$ConfirmRestore
)

$ErrorActionPreference = "Stop"
if ($RestoreDatabase -notmatch '^forgeflow_restore_[a-zA-Z0-9_]+$') { throw "Restore database must start with forgeflow_restore_" }
if ($BackupFile -notmatch '^/backups/forgeflow-[0-9]{8}T[0-9]{6}Z\.dump$') { throw "BackupFile must use /backups/forgeflow-<UTC timestamp>.dump" }
$workspace = Split-Path -Parent $PSScriptRoot
$compose = Join-Path $workspace "deploy/staging/compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
if (-not (Test-Path -LiteralPath $resolvedEnv -PathType Leaf)) { throw "Staging env file does not exist: $resolvedEnv" }

$previousBackup = $env:FORGEFLOW_BACKUP_FILE
$previousDatabase = $env:FORGEFLOW_RESTORE_DATABASE
$previousConfirmation = $env:FORGEFLOW_CONFIRM_RESTORE
try {
    $env:FORGEFLOW_BACKUP_FILE = $BackupFile
    $env:FORGEFLOW_RESTORE_DATABASE = $RestoreDatabase
    $env:FORGEFLOW_CONFIRM_RESTORE = "restore-staging-drill"
    Push-Location (Join-Path $workspace "deploy/staging")
    try {
        docker compose --env-file $resolvedEnv -f $compose --profile ops run --rm restore-drill
        if ($LASTEXITCODE -ne 0) { throw "Staging restore drill failed" }
    }
    finally { Pop-Location }
}
finally {
    $env:FORGEFLOW_BACKUP_FILE = $previousBackup
    $env:FORGEFLOW_RESTORE_DATABASE = $previousDatabase
    $env:FORGEFLOW_CONFIRM_RESTORE = $previousConfirmation
}
