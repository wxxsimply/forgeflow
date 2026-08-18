param(
    [string]$EnvFile = "deploy/staging/staging.env",
    [switch]$IncludeOpenAI,
    [switch]$IncludeBootstrap,
    [switch]$RequireDigests
)

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot
$deployDirectory = Join-Path $workspace "deploy/staging"
$compose = Join-Path $deployDirectory "compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
if (-not (Test-Path -LiteralPath $resolvedEnv -PathType Leaf)) { throw "Copy staging.env.example to staging.env and configure it first" }

$environment = @{}
Get-Content -LiteralPath $resolvedEnv | ForEach-Object {
    $line = $_.Trim()
    if ($line -and -not $line.StartsWith('#') -and $line.Contains('=')) {
        $name, $value = $line.Split('=', 2)
        $environment[$name.Trim()] = $value.Trim()
    }
}
foreach ($required in @('FORGEFLOW_RELEASE','FORGEFLOW_DOMAIN','ACME_EMAIL','FORGEFLOW_REPOSITORY_PATH')) {
    if (-not $environment[$required]) { throw "$required is required in $resolvedEnv" }
}
$repository = [System.IO.Path]::GetFullPath($environment['FORGEFLOW_REPOSITORY_PATH'])
if (-not (Test-Path -LiteralPath $repository -PathType Container)) { throw "Repository mount does not exist: $repository" }
Push-Location $repository
try {
    git rev-parse --verify HEAD | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Repository mount must contain a Git HEAD" }
}
finally { Pop-Location }

$secretNames = @('postgres_password','postgres_dsn','alert_webhook_url')
if ($IncludeOpenAI) { $secretNames += 'openai_api_key' }
if ($IncludeBootstrap) { $secretNames += 'bootstrap_admin_password' }
foreach ($name in $secretNames) {
    $path = Join-Path $deployDirectory "secrets/$name"
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Missing staging secret: $path" }
    if ((Get-Item -LiteralPath $path).Length -eq 0) { throw "Staging secret is empty: $path" }
}

if ($RequireDigests) {
    foreach ($name in @('POSTGRES_IMAGE','CADDY_IMAGE','OTEL_COLLECTOR_IMAGE','PROMETHEUS_IMAGE','ALERTMANAGER_IMAGE')) {
        if ($environment[$name] -notmatch '@sha256:[a-f0-9]{64}$') { throw "$name must be pinned by sha256 for production promotion" }
    }
}

$files = @('-f', $compose)
if ($IncludeOpenAI) { $files += @('-f', (Join-Path $deployDirectory 'compose.openai.yaml')) }
if ($IncludeBootstrap) { $files += @('-f', (Join-Path $deployDirectory 'compose.bootstrap.yaml')) }
Push-Location $deployDirectory
try {
    $json = docker compose --env-file $resolvedEnv @files config --format json
    if ($LASTEXITCODE -ne 0) { throw "docker compose config validation failed" }
    $configuration = ($json -join "`n") | ConvertFrom-Json
}
finally { Pop-Location }

foreach ($serviceName in @('api','worker','web','caddy')) {
    $service = $configuration.services.$serviceName
    if (-not $service.read_only) { throw "$serviceName must use a read-only root filesystem" }
    if ($service.ports) { throw "$serviceName must not publish host ports" }
}
foreach ($serviceProperty in $configuration.services.PSObject.Properties) {
    $serialized = $serviceProperty.Value | ConvertTo-Json -Depth 20 -Compress
    if ($serialized -match 'docker\.sock') { throw "Docker Socket exposure is forbidden: $($serviceProperty.Name)" }
}
$workerEnvironment = $configuration.services.worker.environment | ConvertTo-Json -Depth 10 -Compress
if ($IncludeOpenAI -and ($workerEnvironment -notmatch 'DOCKER_HOST' -or -not $configuration.services.'sandbox-engine')) {
    throw "Development workflow requires the isolated sandbox-engine; the host Docker Socket remains forbidden"
}
$apiJSON = $configuration.services.api | ConvertTo-Json -Depth 20 -Compress
if ($apiJSON -match 'openai_api_key|OPENAI_API_KEY') { throw "API service must not receive the model API key" }
if (-not $configuration.services.caddy.ports -or $configuration.services.postgres.ports -or $configuration.services.prometheus.ports -or $configuration.services.alertmanager.ports) {
    throw "Only Caddy may publish host ports"
}

Write-Host "ForgeFlow staging preflight passed for release $($environment['FORGEFLOW_RELEASE'])."
