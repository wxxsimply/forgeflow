[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$')]
    [string]$Release,
    [Parameter(Mandatory = $true)]
    [string]$Manifest,
    [string]$EnvFile = "deploy/staging/staging.env",
    [switch]$IncludeOpenAI,
    [switch]$IncludeBootstrap,
    [Parameter(Mandatory = $true)]
    [switch]$ConfirmDeploy
)

$ErrorActionPreference = "Stop"
if (-not $ConfirmDeploy) { throw "Staging release requires -ConfirmDeploy" }
if ($IncludeBootstrap -and $IncludeOpenAI) { throw "Bootstrap and the paid model workflow must be deployed in separate steps" }

$workspace = Split-Path -Parent $PSScriptRoot
$deployDirectory = Join-Path $workspace "deploy/staging"
$compose = Join-Path $deployDirectory "compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
$manifestPath = [System.IO.Path]::GetFullPath((Join-Path $workspace $Manifest))
$gitCommit = (& git -C $workspace rev-parse HEAD 2>$null)
if ($LASTEXITCODE -ne 0 -or $gitCommit -notmatch '^[0-9a-f]{40}$') { throw "Release requires a committed 40-character Git HEAD" }
$gitStatus = (& git -C $workspace status --porcelain --untracked-files=all)
if ($LASTEXITCODE -ne 0 -or $gitStatus) { throw "Release requires a clean Git worktree" }
if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw "Release manifest does not exist: $manifestPath" }

& (Join-Path $PSScriptRoot 'staging-preflight.ps1') -EnvFile $EnvFile -Manifest $Manifest -IncludeOpenAI:$IncludeOpenAI -IncludeBootstrap:$IncludeBootstrap -RequireDigests
$releaseManifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if ($releaseManifest.release -ne $Release) { throw "The requested release does not match the Release manifest" }
if ($releaseManifest.gitCommit -ne $gitCommit) { throw "The checked-out source does not match the Release manifest" }

$environment = @{}
Get-Content -LiteralPath $resolvedEnv | ForEach-Object {
    $line = $_.Trim()
    if ($line -and -not $line.StartsWith('#') -and $line.Contains('=')) {
        $name, $value = $line.Split('=', 2)
        $environment[$name.Trim()] = $value.Trim()
    }
}

$files = @('-f', $compose)
if ($IncludeOpenAI) { $files += @('-f', (Join-Path $deployDirectory 'compose.openai.yaml')) }
if ($IncludeBootstrap) { $files += @('-f', (Join-Path $deployDirectory 'compose.bootstrap.yaml')) }
$releaseDirectory = Join-Path $workspace ".forgeflow/deploy/releases"
$cacheDirectory = Join-Path $workspace ".forgeflow/deploy/cache"
$currentPath = Join-Path $workspace ".forgeflow/deploy/current.json"
New-Item -ItemType Directory -Force -Path $releaseDirectory, $cacheDirectory | Out-Null
$previousRelease = $null
if (Test-Path -LiteralPath $currentPath) { $previousRelease = (Get-Content -Raw -LiteralPath $currentPath | ConvertFrom-Json).release }

function Invoke-Compose {
    param([string[]]$Arguments)
    & docker compose --env-file $resolvedEnv @files @Arguments
    if ($LASTEXITCODE -ne 0) { throw "docker compose failed: $($Arguments -join ' ')" }
}

function Get-ServiceJSON {
    param([string]$Service, [string]$URL)
    $value = (& docker compose --env-file $resolvedEnv @files exec -T $Service wget -qO- $URL)
    if ($LASTEXITCODE -ne 0) { throw "$Service health request failed" }
    return (($value -join [Environment]::NewLine) | ConvertFrom-Json)
}

Push-Location $deployDirectory
try {
    Invoke-Compose @('pull')

    if ($IncludeOpenAI) {
        $sandboxImage = $environment.FORGEFLOW_SANDBOX_IMAGE
        & docker pull $sandboxImage
        if ($LASTEXITCODE -ne 0) { throw "Sandbox image pull failed" }
        Invoke-Compose @('up', '-d', '--no-build', '--wait', '--wait-timeout', '180', 'sandbox-engine')

        $sandboxArchive = Join-Path $cacheDirectory "forgeflow-sandbox-$Release.tar"
        if (Test-Path -LiteralPath $sandboxArchive) { Remove-Item -LiteralPath $sandboxArchive -Force }
        try {
            & docker save --output $sandboxArchive $sandboxImage
            if ($LASTEXITCODE -ne 0) { throw "Sandbox image export failed" }
            Invoke-Compose @('cp', $sandboxArchive, 'sandbox-engine:/tmp/forgeflow-sandbox.tar')
            Invoke-Compose @('exec', '-T', 'sandbox-engine', 'docker', 'load', '-i', '/tmp/forgeflow-sandbox.tar')
            Invoke-Compose @('exec', '-T', 'sandbox-engine', 'docker', 'image', 'inspect', $sandboxImage)
        }
        finally {
            if (Test-Path -LiteralPath $sandboxArchive) { Remove-Item -LiteralPath $sandboxArchive -Force }
            try { Invoke-Compose @('exec', '-T', 'sandbox-engine', 'rm', '-f', '/tmp/forgeflow-sandbox.tar') } catch { }
        }
    }

    if ($IncludeBootstrap) {
        Invoke-Compose @('up', '-d', '--no-build', '--remove-orphans', '--wait', '--wait-timeout', '180', 'postgres', 'otel-collector', 'prometheus', 'alertmanager', 'api', 'web', 'caddy')
    }
    else {
        Invoke-Compose @('up', '-d', '--no-build', '--remove-orphans', '--wait', '--wait-timeout', '180')
    }

    $apiHealth = Get-ServiceJSON 'api' 'http://127.0.0.1:8080/healthz'
    $webRelease = Get-ServiceJSON 'web' 'http://127.0.0.1:8080/release.json'
    foreach ($metadata in @($apiHealth, $webRelease)) {
        if ($metadata.serviceVersion -ne $Release -or $metadata.gitCommit -ne $gitCommit) { throw "Service release metadata does not match the approved release" }
    }

    $workerHealth = $null
    if (-not $IncludeBootstrap) {
        $workerHealth = Get-ServiceJSON 'worker' 'http://127.0.0.1:9091/readyz'
        if ($workerHealth.status -ne 'ready' -or $workerHealth.serviceVersion -ne $Release -or $workerHealth.gitCommit -ne $gitCommit) {
            throw "Worker is not ready for the approved Prompt/model release"
        }
    }

    $publicHealth = Invoke-WebRequest -UseBasicParsing "https://$($environment.FORGEFLOW_DOMAIN)/healthz" -TimeoutSec 30
    if ($publicHealth.StatusCode -ne 200) { throw "Public HTTPS health check failed" }
    $publicMetadata = $publicHealth.Content | ConvertFrom-Json
    if ($publicMetadata.serviceVersion -ne $Release -or $publicMetadata.gitCommit -ne $gitCommit) { throw "Public API metadata does not match the approved release" }

    Invoke-Compose @('ps', '--all')
    $renderedJSON = (& docker compose --env-file $resolvedEnv @files config --format json)
    if ($LASTEXITCODE -ne 0) { throw "Could not record rendered Compose configuration" }
    $rendered = ($renderedJSON -join [Environment]::NewLine) | ConvertFrom-Json
}
finally { Pop-Location }

$manifestHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $manifestPath).Hash.ToLowerInvariant()
$deployment = [ordered]@{
    schemaVersion = 'forgeflow.deployment/v2'
    release = $Release
    previousRelease = $previousRelease
    deployedAt = (Get-Date).ToUniversalTime().ToString('o')
    gitCommit = $gitCommit
    releaseManifest = [System.IO.Path]::GetRelativePath($workspace, $manifestPath).Replace('\', '/')
    releaseManifestSHA256 = $manifestHash
    workerStarted = -not $IncludeBootstrap
    bootstrapEnabled = [bool]$IncludeBootstrap
    images = [ordered]@{
        api = $rendered.services.api.image
        worker = $rendered.services.worker.image
        web = $rendered.services.web.image
        caddy = $rendered.services.caddy.image
        sandbox = $environment.FORGEFLOW_SANDBOX_IMAGE
    }
    health = [ordered]@{ api = $apiHealth; worker = $workerHealth; web = $webRelease; publicAPI = $publicMetadata }
    databaseRollback = 'manual-forbidden'
}
$deploymentFile = if ($IncludeBootstrap) { "$Release-bootstrap-deployment.json" } else { "$Release-deployment.json" }
$deploymentPath = Join-Path $releaseDirectory $deploymentFile
$deployment | ConvertTo-Json -Depth 20 | Set-Content -Encoding utf8 -LiteralPath $deploymentPath
if (-not $IncludeBootstrap) {
    Copy-Item -LiteralPath $deploymentPath -Destination $currentPath -Force
}
Write-Host "Staging release $Release deployed from immutable digests. Deployment record: $deploymentPath"
if ($IncludeBootstrap) {
    Write-Warning "Worker was intentionally not started. Verify administrator login, remove bootstrap_admin_password, then deploy the same manifest again without -IncludeBootstrap."
}
