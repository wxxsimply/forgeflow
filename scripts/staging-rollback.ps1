param(
    [Parameter(Mandatory = $true)] [string]$Manifest,
    [string]$EnvFile = "deploy/staging/staging.env",
    [switch]$IncludeOpenAI,
    [Parameter(Mandatory = $true)] [switch]$ConfirmRollback
)

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot
$manifestPath = [System.IO.Path]::GetFullPath((Join-Path $workspace $Manifest))
if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw "Release manifest does not exist" }
$releaseManifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if ($releaseManifest.schemaVersion -ne 'forgeflow.release/v1' -or $releaseManifest.release -notmatch '^[0-9]+\.[0-9]+\.[0-9]+') { throw "Release manifest is invalid" }

$deployDirectory = Join-Path $workspace "deploy/staging"
$compose = Join-Path $deployDirectory "compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
$files = @('-f', $compose)
if ($IncludeOpenAI) { $files += @('-f', (Join-Path $deployDirectory 'compose.openai.yaml')) }
$previousEnvironmentRelease = $env:FORGEFLOW_RELEASE
try {
    $env:FORGEFLOW_RELEASE = $releaseManifest.release
    Push-Location $deployDirectory
    try {
        docker compose --env-file $resolvedEnv @files run --rm --no-deps migrate db check
        if ($LASTEXITCODE -ne 0) { throw "Rollback blocked: target binary is incompatible with the current database schema" }
        docker compose --env-file $resolvedEnv @files up -d --no-build --no-deps --wait --wait-timeout 180 api worker web
        if ($LASTEXITCODE -ne 0) { throw "Application rollback failed" }
        docker compose --env-file $resolvedEnv @files ps api worker web
    }
    finally { Pop-Location }
    Copy-Item -LiteralPath $manifestPath -Destination (Join-Path $workspace '.forgeflow/deploy/current.json') -Force
    Write-Host "Rolled application containers back to $($releaseManifest.release). Database was not downgraded."
}
finally { $env:FORGEFLOW_RELEASE = $previousEnvironmentRelease }
