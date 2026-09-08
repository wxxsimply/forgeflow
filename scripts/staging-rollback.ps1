[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)] [string]$Manifest,
    [string]$EnvFile = "deploy/staging/staging.env",
    [switch]$IncludeOpenAI,
    [switch]$DryRun,
    [Parameter(Mandatory = $true)] [switch]$ConfirmRollback
)

$ErrorActionPreference = "Stop"
if (-not $ConfirmRollback) { throw "Rollback requires -ConfirmRollback" }

$workspace = Split-Path -Parent $PSScriptRoot
$deployDirectory = Join-Path $workspace "deploy/staging"
$compose = Join-Path $deployDirectory "compose.yaml"
$manifestPath = [System.IO.Path]::GetFullPath((Join-Path $workspace $Manifest))
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
if (-not (Test-Path -LiteralPath $resolvedEnv -PathType Leaf)) { throw "Staging environment file does not exist" }

& (Join-Path $PSScriptRoot 'validate-release-assets.ps1') -Manifest $Manifest
$releaseManifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if ($releaseManifest.schemaVersion -ne 'forgeflow.release/v2') { throw "Rollback manifest must use forgeflow.release/v2" }
$imageVariables = [ordered]@{
    'forgeflow-api' = 'FORGEFLOW_API_IMAGE'
    'forgeflow-worker' = 'FORGEFLOW_WORKER_IMAGE'
    'forgeflow-web' = 'FORGEFLOW_WEB_IMAGE'
    'forgeflow-caddy' = 'FORGEFLOW_CADDY_IMAGE'
    'forgeflow-sandbox' = 'FORGEFLOW_SANDBOX_IMAGE'
}
$replacement = [ordered]@{
    FORGEFLOW_RELEASE = $releaseManifest.release
    FORGEFLOW_GIT_COMMIT = $releaseManifest.gitCommit
}
foreach ($imageName in $imageVariables.Keys) {
    $entry = @($releaseManifest.images | Where-Object { $_.name -eq $imageName })
    if ($entry.Count -ne 1) { throw "Rollback manifest is missing $imageName" }
    $replacement[$imageVariables[$imageName]] = $entry[0].reference
}

$environment = @{}
$targetLines = foreach ($sourceLine in Get-Content -LiteralPath $resolvedEnv) {
    $trimmed = $sourceLine.Trim()
    if ($trimmed -and -not $trimmed.StartsWith('#') -and $trimmed.Contains('=')) {
        $name, $value = $trimmed.Split('=', 2)
        $name = $name.Trim()
        if ($environment.ContainsKey($name)) { throw "Duplicate environment variable: $name" }
        $environment[$name] = $value.Trim()
        if ($replacement.Contains($name)) { "$name=$($replacement[$name])"; continue }
    }
    $sourceLine
}
foreach ($name in $replacement.Keys) {
    if (-not $environment.ContainsKey($name)) { $targetLines += "$name=$($replacement[$name])" }
}
$digestPattern = '^[a-zA-Z0-9._/-]+(?:\:[0-9]+)?/[a-zA-Z0-9._/-]+@sha256:[a-f0-9]{64}$|^[a-zA-Z0-9._/-]+@sha256:[a-f0-9]{64}$'
$retainedDigestNames = @('POSTGRES_IMAGE', 'OTEL_COLLECTOR_IMAGE', 'PROMETHEUS_IMAGE', 'ALERTMANAGER_IMAGE')
if ($IncludeOpenAI) { $retainedDigestNames += 'DOCKER_DIND_IMAGE' }
foreach ($name in $retainedDigestNames) {
    if ($environment[$name] -notmatch $digestPattern) { throw "$name must remain pinned by digest during rollback" }
}

$cacheDirectory = Join-Path $workspace '.forgeflow/deploy/cache'
New-Item -ItemType Directory -Force -Path $cacheDirectory | Out-Null
$targetEnv = Join-Path $cacheDirectory "rollback-$($releaseManifest.release)-$([Guid]::NewGuid().ToString('N')).env"
$targetLines | Set-Content -Encoding utf8 -LiteralPath $targetEnv
$files = @('-f', $compose)
if ($IncludeOpenAI) { $files += @('-f', (Join-Path $deployDirectory 'compose.openai.yaml')) }

function Invoke-Compose {
    param([string[]]$Arguments)
    & docker compose --env-file $targetEnv @files @Arguments
    if ($LASTEXITCODE -ne 0) { throw "docker compose failed: $($Arguments -join ' ')" }
}

function Get-ServiceJSON {
    param([string]$Service, [string]$URL)
    $value = (& docker compose --env-file $targetEnv @files exec -T $Service wget -qO- $URL)
    if ($LASTEXITCODE -ne 0) { throw "$Service health request failed" }
    return (($value -join [Environment]::NewLine) | ConvertFrom-Json)
}

try {
    Push-Location $deployDirectory
    try {
        $renderedJSON = (& docker compose --env-file $targetEnv @files config --format json)
        if ($LASTEXITCODE -ne 0) { throw "Rollback target Compose configuration is invalid" }
        $rendered = ($renderedJSON -join [Environment]::NewLine) | ConvertFrom-Json
    }
    finally { Pop-Location }

    foreach ($service in @('api', 'worker', 'web', 'caddy', 'migrate')) {
        if ($rendered.services.$service.PSObject.Properties['build']) { throw "Rollback may not build $service on the server" }
    }
    $expectedImages = [ordered]@{
        api = $replacement.FORGEFLOW_API_IMAGE
        worker = $replacement.FORGEFLOW_WORKER_IMAGE
        web = $replacement.FORGEFLOW_WEB_IMAGE
        caddy = $replacement.FORGEFLOW_CADDY_IMAGE
        migrate = $replacement.FORGEFLOW_API_IMAGE
    }
    foreach ($service in $expectedImages.Keys) {
        if ($rendered.services.$service.image -ne $expectedImages[$service]) { throw "Rendered $service image does not match the rollback manifest" }
    }
    if ($rendered.services.worker.environment.FORGEFLOW_SANDBOX_IMAGE -ne $replacement.FORGEFLOW_SANDBOX_IMAGE) {
        throw "Rendered Sandbox image does not match the rollback manifest"
    }

    if ($DryRun) {
        [ordered]@{
            schemaVersion = 'forgeflow.rollback-plan/v2'
            targetRelease = $releaseManifest.release
            targetGitCommit = $releaseManifest.gitCommit
            images = $expectedImages
            sandboxImage = $replacement.FORGEFLOW_SANDBOX_IMAGE
            databaseAction = 'compatibility-check-only'
            downMigration = $false
            deliveryAttempted = $false
            executed = $false
        } | ConvertTo-Json -Depth 10
        return
    }

    $gitStatus = (& git -C $workspace status --porcelain --untracked-files=all)
    if ($LASTEXITCODE -ne 0 -or $gitStatus) { throw "Live rollback requires a clean Git worktree" }

    $currentPath = Join-Path $workspace '.forgeflow/deploy/current.json'
    if (-not (Test-Path -LiteralPath $currentPath -PathType Leaf)) { throw "Current deployment record is missing" }
    $current = Get-Content -Raw -LiteralPath $currentPath | ConvertFrom-Json
    if ($current.schemaVersion -ne 'forgeflow.deployment/v2') { throw "Current deployment record must use forgeflow.deployment/v2" }
    if ($current.release -eq $releaseManifest.release) { throw "Rollback target is already the current release" }

    $publicBefore = Invoke-WebRequest -UseBasicParsing "https://$($environment.FORGEFLOW_DOMAIN)/healthz" -TimeoutSec 30
    if ($publicBefore.StatusCode -ne 200) { throw "Current public health check failed before rollback" }
    $beforeMetadata = $publicBefore.Content | ConvertFrom-Json
    if ($beforeMetadata.serviceVersion -ne $current.release -or $beforeMetadata.gitCommit -ne $current.gitCommit) {
        throw "Live deployment does not match the current deployment record"
    }

    Push-Location $deployDirectory
    try {
        Invoke-Compose @('pull', 'api', 'worker', 'web', 'caddy', 'migrate')
        if ($IncludeOpenAI) {
            & docker pull $replacement.FORGEFLOW_SANDBOX_IMAGE
            if ($LASTEXITCODE -ne 0) { throw "Rollback Sandbox image pull failed" }
            Invoke-Compose @('up', '-d', '--no-build', '--wait', '--wait-timeout', '180', 'sandbox-engine')
            $sandboxArchive = Join-Path $cacheDirectory "rollback-sandbox-$($releaseManifest.release).tar"
            if (Test-Path -LiteralPath $sandboxArchive) { Remove-Item -LiteralPath $sandboxArchive -Force }
            try {
                & docker save --output $sandboxArchive $replacement.FORGEFLOW_SANDBOX_IMAGE
                if ($LASTEXITCODE -ne 0) { throw "Rollback Sandbox image export failed" }
                Invoke-Compose @('cp', $sandboxArchive, 'sandbox-engine:/tmp/forgeflow-rollback-sandbox.tar')
                Invoke-Compose @('exec', '-T', 'sandbox-engine', 'docker', 'load', '-i', '/tmp/forgeflow-rollback-sandbox.tar')
                Invoke-Compose @('exec', '-T', 'sandbox-engine', 'docker', 'image', 'inspect', $replacement.FORGEFLOW_SANDBOX_IMAGE)
            }
            finally {
                if (Test-Path -LiteralPath $sandboxArchive) { Remove-Item -LiteralPath $sandboxArchive -Force }
                try { Invoke-Compose @('exec', '-T', 'sandbox-engine', 'rm', '-f', '/tmp/forgeflow-rollback-sandbox.tar') } catch { }
            }
        }

        Invoke-Compose @('run', '--rm', '--no-deps', 'migrate', 'db', 'check')
        # The operator must drain in-flight work first. Stop Worker only after the read-only compatibility check succeeds.
        Invoke-Compose @('stop', 'worker')
        Invoke-Compose @('up', '-d', '--no-build', '--no-deps', '--force-recreate', '--wait', '--wait-timeout', '180', 'api', 'worker', 'web', 'caddy')

        $apiHealth = Get-ServiceJSON 'api' 'http://127.0.0.1:8080/healthz'
        $workerHealth = Get-ServiceJSON 'worker' 'http://127.0.0.1:9091/readyz'
        $webRelease = Get-ServiceJSON 'web' 'http://127.0.0.1:8080/release.json'
        foreach ($metadata in @($apiHealth, $workerHealth, $webRelease)) {
            if ($metadata.serviceVersion -ne $releaseManifest.release -or $metadata.gitCommit -ne $releaseManifest.gitCommit) {
                throw "A rolled-back service does not match the approved manifest"
            }
        }
        if ($workerHealth.status -ne 'ready') { throw "Worker is not ready for the target Prompt/model release" }
        Invoke-Compose @('ps', '--all')
    }
    finally { Pop-Location }

    $publicAfter = Invoke-WebRequest -UseBasicParsing "https://$($environment.FORGEFLOW_DOMAIN)/healthz" -TimeoutSec 30
    if ($publicAfter.StatusCode -ne 200) { throw "Public health check failed after rollback" }
    $publicMetadata = $publicAfter.Content | ConvertFrom-Json
    if ($publicMetadata.serviceVersion -ne $releaseManifest.release -or $publicMetadata.gitCommit -ne $releaseManifest.gitCommit) {
        throw "Public API does not match the rollback manifest"
    }

    $releaseDirectory = Join-Path $workspace '.forgeflow/deploy/releases'
    New-Item -ItemType Directory -Force -Path $releaseDirectory | Out-Null
    $manifestHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $manifestPath).Hash.ToLowerInvariant()
    $deployment = [ordered]@{
        schemaVersion = 'forgeflow.deployment/v2'
        release = $releaseManifest.release
        previousRelease = $current.release
        rolledBackAt = (Get-Date).ToUniversalTime().ToString('o')
        gitCommit = $releaseManifest.gitCommit
        releaseManifest = [System.IO.Path]::GetRelativePath($workspace, $manifestPath).Replace('\', '/')
        releaseManifestSHA256 = $manifestHash
        workerStarted = $true
        bootstrapEnabled = $false
        images = [ordered]@{
            api = $expectedImages.api
            worker = $expectedImages.worker
            web = $expectedImages.web
            caddy = $expectedImages.caddy
            sandbox = $replacement.FORGEFLOW_SANDBOX_IMAGE
        }
        health = [ordered]@{ api = $apiHealth; worker = $workerHealth; web = $webRelease; publicAPI = $publicMetadata }
        databaseRollback = 'not-performed'
    }
    $recordPath = Join-Path $releaseDirectory "$($releaseManifest.release)-rollback-$((Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ')).json"
    $deployment | ConvertTo-Json -Depth 20 | Set-Content -Encoding utf8 -LiteralPath $recordPath
    Copy-Item -LiteralPath $recordPath -Destination $currentPath -Force
    Write-Host "Application rollback completed. Database schema was not downgraded. Record: $recordPath"
}
finally {
    if (Test-Path -LiteralPath $targetEnv) { Remove-Item -LiteralPath $targetEnv -Force }
}
