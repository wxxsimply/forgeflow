[CmdletBinding()]
param(
    [string]$EnvFile = "deploy/staging/staging.env",
    [string]$Manifest,
    [switch]$IncludeOpenAI,
    [switch]$IncludeBootstrap,
    [switch]$RequireDigests,
    [switch]$AllowPlaceholderDigests
)

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot
$deployDirectory = Join-Path $workspace "deploy/staging"
$compose = Join-Path $deployDirectory "compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))

function Assert-Staging {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Read-EnvironmentFile {
    param([string]$Path)
    Assert-Staging (Test-Path -LiteralPath $Path -PathType Leaf) "Copy staging.env.example to staging.env and configure it first"
    $values = @{}
    Get-Content -LiteralPath $Path | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith('#') -and $line.Contains('=')) {
            $name, $value = $line.Split('=', 2)
            $name = $name.Trim()
            Assert-Staging (-not $values.ContainsKey($name)) "Duplicate environment variable: $name"
            $values[$name] = $value.Trim()
        }
    }
    return $values
}

function Assert-DigestReference {
    param([string]$Name, [string]$Value)
    Assert-Staging ($Value -match '^[a-zA-Z0-9._/-]+(?:\:[0-9]+)?/[a-zA-Z0-9._/-]+@sha256:[a-f0-9]{64}$|^[a-zA-Z0-9._/-]+@sha256:[a-f0-9]{64}$') "$Name must be an image@sha256 reference"
    if (-not $AllowPlaceholderDigests) {
        Assert-Staging ($Value -notmatch '@sha256:0{64}$') "$Name still contains the CI placeholder digest"
    }
}

$environment = Read-EnvironmentFile $resolvedEnv
$required = @(
    'FORGEFLOW_RELEASE', 'FORGEFLOW_GIT_COMMIT', 'FORGEFLOW_DOMAIN', 'ACME_EMAIL', 'FORGEFLOW_REPOSITORY_PATH',
    'FORGEFLOW_API_IMAGE', 'FORGEFLOW_WORKER_IMAGE', 'FORGEFLOW_WEB_IMAGE', 'FORGEFLOW_CADDY_IMAGE',
    'POSTGRES_IMAGE', 'OTEL_COLLECTOR_IMAGE', 'PROMETHEUS_IMAGE', 'ALERTMANAGER_IMAGE'
)
if ($RequireDigests -or $IncludeOpenAI) { $required += 'FORGEFLOW_SANDBOX_IMAGE' }
if ($IncludeOpenAI) { $required += 'DOCKER_DIND_IMAGE' }
if ($IncludeBootstrap) { $required += 'FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL' }
foreach ($name in $required) {
    Assert-Staging (-not [string]::IsNullOrWhiteSpace($environment[$name])) "$name is required in $resolvedEnv"
}

Assert-Staging ($environment.FORGEFLOW_RELEASE -match '^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$') "FORGEFLOW_RELEASE must be a semantic version"
Assert-Staging ($environment.FORGEFLOW_GIT_COMMIT -match '^[a-f0-9]{40}$') "FORGEFLOW_GIT_COMMIT must be a lowercase 40-character SHA"
Assert-Staging ($environment.FORGEFLOW_DOMAIN -notmatch '^(localhost|127\.0\.0\.1|\[?::1\]?)$') "FORGEFLOW_DOMAIN must not be localhost"
Assert-Staging ($environment.FORGEFLOW_DOMAIN -notmatch '://') "FORGEFLOW_DOMAIN must not include a URL scheme"
Assert-Staging ($environment.ACME_EMAIL -match '^[^@\s]+@[^@\s]+\.[^@\s]+$') "ACME_EMAIL is invalid"
if ($IncludeBootstrap) {
    Assert-Staging ($environment.FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL -match '^[^@\s]+@[^@\s]+\.[^@\s]+$') "FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL is invalid"
}
if ($AllowPlaceholderDigests) {
    Assert-Staging ($environment.FORGEFLOW_RELEASE -match '(?:^|[.-])ci(?:[.-]|$)') "Placeholder digests are restricted to explicit CI releases"
    Assert-Staging (-not $Manifest) "Placeholder digests cannot be used with a Release manifest"
}

$sourceCommit = (& git -C $workspace rev-parse HEAD 2>$null)
Assert-Staging ($LASTEXITCODE -eq 0 -and $sourceCommit -match '^[a-f0-9]{40}$') "ForgeFlow source must contain a committed Git HEAD"
Assert-Staging ($sourceCommit -eq $environment.FORGEFLOW_GIT_COMMIT) "FORGEFLOW_GIT_COMMIT does not match the checked-out ForgeFlow source"

$repository = [System.IO.Path]::GetFullPath($environment.FORGEFLOW_REPOSITORY_PATH)
Assert-Staging (Test-Path -LiteralPath $repository -PathType Container) "Repository mount does not exist: $repository"

$secretNames = @('postgres_password', 'postgres_dsn', 'alert_webhook_url')
if ($IncludeOpenAI) { $secretNames += 'openai_api_key' }
if ($IncludeBootstrap) { $secretNames += 'bootstrap_admin_password' }
foreach ($name in $secretNames) {
    $path = Join-Path $deployDirectory "secrets/$name"
    Assert-Staging (Test-Path -LiteralPath $path -PathType Leaf) "Missing staging secret: $path"
    $item = Get-Item -LiteralPath $path
    Assert-Staging ($item.Length -gt 0) "Staging secret is empty: $path"
    Assert-Staging (-not $item.LinkType) "Staging secret must not be a symbolic link: $path"
    if (-not $IsWindows) {
        $mode = [System.IO.File]::GetUnixFileMode($path)
        $disallowed = [System.IO.UnixFileMode]::GroupRead -bor [System.IO.UnixFileMode]::GroupWrite -bor [System.IO.UnixFileMode]::GroupExecute -bor [System.IO.UnixFileMode]::OtherRead -bor [System.IO.UnixFileMode]::OtherWrite -bor [System.IO.UnixFileMode]::OtherExecute
        Assert-Staging (($mode -band $disallowed) -eq 0) "Staging secret must not grant group/other permissions: $path"
    }
}

if ($RequireDigests) {
    $digestNames = @(
        'FORGEFLOW_API_IMAGE', 'FORGEFLOW_WORKER_IMAGE', 'FORGEFLOW_WEB_IMAGE', 'FORGEFLOW_CADDY_IMAGE',
        'FORGEFLOW_SANDBOX_IMAGE', 'POSTGRES_IMAGE', 'OTEL_COLLECTOR_IMAGE', 'PROMETHEUS_IMAGE', 'ALERTMANAGER_IMAGE'
    )
    if ($IncludeOpenAI) { $digestNames += 'DOCKER_DIND_IMAGE' }
    foreach ($name in $digestNames) { Assert-DigestReference $name $environment[$name] }
}

if ($Manifest) {
    & (Join-Path $PSScriptRoot 'validate-release-assets.ps1') -Manifest $Manifest
    $manifestPath = [System.IO.Path]::GetFullPath((Join-Path $workspace $Manifest))
    $releaseManifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
    Assert-Staging ($releaseManifest.release -eq $environment.FORGEFLOW_RELEASE) "Release manifest version does not match staging.env"
    Assert-Staging ($releaseManifest.gitCommit -eq $environment.FORGEFLOW_GIT_COMMIT) "Release manifest Git SHA does not match staging.env"
    $imageVariables = [ordered]@{
        'forgeflow-api' = 'FORGEFLOW_API_IMAGE'
        'forgeflow-worker' = 'FORGEFLOW_WORKER_IMAGE'
        'forgeflow-web' = 'FORGEFLOW_WEB_IMAGE'
        'forgeflow-caddy' = 'FORGEFLOW_CADDY_IMAGE'
        'forgeflow-sandbox' = 'FORGEFLOW_SANDBOX_IMAGE'
    }
    foreach ($imageName in $imageVariables.Keys) {
        $entry = @($releaseManifest.images | Where-Object { $_.name -eq $imageName })
        Assert-Staging ($entry.Count -eq 1) "Release manifest is missing $imageName"
        Assert-Staging ($environment[$imageVariables[$imageName]] -eq $entry[0].reference) "$($imageVariables[$imageName]) does not match the Release manifest"
    }
}

$files = @('-f', $compose)
if ($IncludeOpenAI) { $files += @('-f', (Join-Path $deployDirectory 'compose.openai.yaml')) }
if ($IncludeBootstrap) { $files += @('-f', (Join-Path $deployDirectory 'compose.bootstrap.yaml')) }
Push-Location $deployDirectory
try {
    $json = docker compose --env-file $resolvedEnv @files config --format json
    Assert-Staging ($LASTEXITCODE -eq 0) "docker compose config validation failed"
    $configuration = ($json -join [Environment]::NewLine) | ConvertFrom-Json
}
finally { Pop-Location }

foreach ($serviceProperty in $configuration.services.PSObject.Properties) {
    $serviceName = $serviceProperty.Name
    $service = $serviceProperty.Value
    Assert-Staging (-not $service.PSObject.Properties['build']) "Staging service $serviceName must not build images on the server"
    $serialized = $service | ConvertTo-Json -Depth 20 -Compress
    Assert-Staging ($serialized -notmatch 'docker\.sock') "Docker Socket exposure is forbidden: $serviceName"
    if ($serviceName -ne 'caddy') { Assert-Staging (-not $service.ports) "Only Caddy may publish host ports: $serviceName" }
}

foreach ($serviceName in @('api', 'worker', 'web', 'caddy')) {
    Assert-Staging ($configuration.services.$serviceName.read_only) "$serviceName must use a read-only root filesystem"
}
Assert-Staging ($configuration.services.migrate.image -eq $configuration.services.api.image) "Migration must reuse the API image"
Assert-Staging ($configuration.services.migrate.entrypoint[0] -eq '/usr/local/bin/forgeflow') "Migration must use the CLI embedded in the API image"
Assert-Staging ($configuration.services.api.image -eq $environment.FORGEFLOW_API_IMAGE) "Rendered API image differs from staging.env"
Assert-Staging ($configuration.services.worker.image -eq $environment.FORGEFLOW_WORKER_IMAGE) "Rendered Worker image differs from staging.env"
Assert-Staging ($configuration.services.web.image -eq $environment.FORGEFLOW_WEB_IMAGE) "Rendered Web image differs from staging.env"
Assert-Staging ($configuration.services.caddy.image -eq $environment.FORGEFLOW_CADDY_IMAGE) "Rendered Caddy image differs from staging.env"
Assert-Staging ($configuration.services.worker.environment.FORGEFLOW_SANDBOX_IMAGE -eq $environment.FORGEFLOW_SANDBOX_IMAGE) "Rendered Sandbox image differs from staging.env"
Assert-Staging ($configuration.services.api.environment.FORGEFLOW_SERVICE_VERSION -eq $environment.FORGEFLOW_RELEASE) "API release version mismatch"
Assert-Staging ($configuration.services.worker.environment.FORGEFLOW_SERVICE_VERSION -eq $environment.FORGEFLOW_RELEASE) "Worker release version mismatch"
Assert-Staging ($configuration.services.worker.environment.FORGEFLOW_GOVERNANCE_ENFORCE_ACTIVE_RELEASES -eq 'true') "Worker active release enforcement must be enabled"

$apiJSON = $configuration.services.api | ConvertTo-Json -Depth 20 -Compress
Assert-Staging ($apiJSON -notmatch 'openai_api_key|OPENAI_API_KEY|DOCKER_HOST') "API service must not receive model or Docker credentials"
Assert-Staging ([bool]$configuration.services.caddy.ports) "Caddy must publish HTTPS edge ports"
$caddyPorts = @($configuration.services.caddy.ports | ForEach-Object { "$($_.published)/$($_.protocol)" })
Assert-Staging ($caddyPorts -contains '80/tcp' -and $caddyPorts -contains '443/tcp') "Caddy must publish TCP 80 and 443"
Assert-Staging (@($caddyPorts | Where-Object { $_ -notin @('80/tcp', '443/tcp', '443/udp') }).Count -eq 0) "Caddy publishes a port outside 80/443"
if ($IncludeOpenAI) {
    $workerJSON = $configuration.services.worker | ConvertTo-Json -Depth 20 -Compress
    Assert-Staging ($workerJSON -match 'DOCKER_HOST' -and $configuration.services.'sandbox-engine') "Development workflow requires the isolated sandbox-engine"
    Assert-Staging ($configuration.services.'sandbox-engine'.image -eq $environment.DOCKER_DIND_IMAGE) "Rendered Docker engine image differs from staging.env"
}

Write-Host "ForgeFlow staging preflight passed for release $($environment.FORGEFLOW_RELEASE) at $sourceCommit."
