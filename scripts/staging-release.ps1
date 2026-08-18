param(
    [Parameter(Mandatory = $true)] [ValidatePattern('^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$')] [string]$Release,
    [string]$EnvFile = "deploy/staging/staging.env",
    [switch]$IncludeOpenAI,
    [switch]$IncludeBootstrap,
    [Parameter(Mandatory = $true)] [switch]$ConfirmDeploy
)

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot
$deployDirectory = Join-Path $workspace "deploy/staging"
$compose = Join-Path $deployDirectory "compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
& (Join-Path $PSScriptRoot 'staging-preflight.ps1') -EnvFile $EnvFile -IncludeOpenAI:$IncludeOpenAI -IncludeBootstrap:$IncludeBootstrap

$files = @('-f', $compose)
if ($IncludeOpenAI) { $files += @('-f', (Join-Path $deployDirectory 'compose.openai.yaml')) }
if ($IncludeBootstrap) { $files += @('-f', (Join-Path $deployDirectory 'compose.bootstrap.yaml')) }
$releaseDirectory = Join-Path $workspace ".forgeflow/deploy/releases"
$currentPath = Join-Path $workspace ".forgeflow/deploy/current.json"
New-Item -ItemType Directory -Force -Path $releaseDirectory | Out-Null
$previousRelease = $null
if (Test-Path -LiteralPath $currentPath) { $previousRelease = (Get-Content -Raw -LiteralPath $currentPath | ConvertFrom-Json).release }

$previousEnvironmentRelease = $env:FORGEFLOW_RELEASE
try {
    $env:FORGEFLOW_RELEASE = $Release
    Push-Location $deployDirectory
    try {
        docker compose --env-file $resolvedEnv @files build api worker web migrate caddy
        if ($LASTEXITCODE -ne 0) { throw "Image build failed" }
        docker compose --env-file $resolvedEnv @files up -d --remove-orphans --wait --wait-timeout 180
        if ($LASTEXITCODE -ne 0) { throw "Staging deployment failed" }
        docker compose --env-file $resolvedEnv @files ps
        $images = @(docker compose --env-file $resolvedEnv @files images --format json)
    }
    finally { Pop-Location }

    $gitCommit = (& git -C $workspace rev-parse HEAD 2>$null)
    if (-not $gitCommit) { throw "Release manifest requires a committed Git HEAD" }
    $manifest = [ordered]@{
        schemaVersion = 'forgeflow.release/v1'
        release = $Release
        previousRelease = $previousRelease
        deployedAt = (Get-Date).ToUniversalTime().ToString('o')
        gitCommit = $gitCommit
        images = $images
        databaseRollback = 'manual-forbidden'
    }
    $manifestPath = Join-Path $releaseDirectory "$Release.json"
    $manifest | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8 -LiteralPath $manifestPath
    Copy-Item -LiteralPath $manifestPath -Destination $currentPath -Force
    Write-Host "Staging release $Release deployed. Manifest: $manifestPath"
    if ($IncludeBootstrap) { Write-Warning "Remove compose.bootstrap.yaml from subsequent starts and delete bootstrap_admin_password now." }
}
finally { $env:FORGEFLOW_RELEASE = $previousEnvironmentRelease }
