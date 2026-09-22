[CmdletBinding()]
param(
    [string]$EnvFile = "deploy/personal-preview/preview.env",
    [ValidateRange(1, 3650)]
    [int]$RetentionDays = 14,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot
$compose = Join-Path $workspace "deploy/personal-preview/compose.yaml"
$backupCompose = Join-Path $workspace "deploy/personal-preview/compose.backup.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
if (-not (Test-Path -LiteralPath $resolvedEnv -PathType Leaf)) { throw "Personal preview env file does not exist: $resolvedEnv" }

$previousRetention = $env:BACKUP_RETENTION_DAYS
Push-Location (Join-Path $workspace "deploy/personal-preview")
try {
    $env:BACKUP_RETENTION_DAYS = $RetentionDays.ToString()
    if ($DryRun) {
        docker compose --env-file $resolvedEnv -f $compose -f $backupCompose --profile ops config --quiet
        if ($LASTEXITCODE -ne 0) { throw "Personal preview backup Compose validation failed" }
        Write-Host "Personal preview backup dry-run passed: custom pg_dump, checksum, manifest, and $RetentionDays-day retention; no backup was created."
        return
    }
    docker compose --env-file $resolvedEnv -f $compose -f $backupCompose --profile ops run --rm backup
    if ($LASTEXITCODE -ne 0) { throw "Personal preview backup failed" }
}
finally {
    Pop-Location
    $env:BACKUP_RETENTION_DAYS = $previousRetention
}
