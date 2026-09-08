[CmdletBinding()]
param(
    [string]$EnvFile = "deploy/staging/staging.env",
    [switch]$IncludeOpenAI,
    [switch]$DryRun,
    [switch]$ConfirmLiveDrill
)

$ErrorActionPreference = "Stop"
if ([bool]$DryRun -eq [bool]$ConfirmLiveDrill) {
    throw "Choose exactly one of -DryRun or -ConfirmLiveDrill"
}
$workspace = Split-Path -Parent $PSScriptRoot

if ($DryRun) {
    & (Join-Path $PSScriptRoot 'validate-staging-assets.ps1')
    $packages = @(
        './internal/repository',
        './internal/tool',
        './internal/policy',
        './internal/sandbox',
        './internal/security',
        './internal/governance',
        './internal/config'
    )
    Push-Location $workspace
    try {
        & go test @packages
        if ($LASTEXITCODE -ne 0) { throw "Synthetic security boundary tests failed" }
    }
    finally { Pop-Location }
    Write-Host "Security dry-run passed: path/symlink, command, sandbox, secret, and governance boundaries; no Staging service was contacted."
    return
}

if (-not $IncludeOpenAI) { throw "Live security drill requires -IncludeOpenAI to verify the isolated Sandbox Engine" }
$deployDirectory = Join-Path $workspace "deploy/staging"
$compose = Join-Path $deployDirectory "compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
if (-not (Test-Path -LiteralPath $resolvedEnv -PathType Leaf)) { throw "Staging env file does not exist: $resolvedEnv" }
$files = @('-f', $compose, '-f', (Join-Path $deployDirectory 'compose.openai.yaml'))

Push-Location $deployDirectory
try {
    $apiID = docker compose --env-file $resolvedEnv @files ps -q api
    $workerID = docker compose --env-file $resolvedEnv @files ps -q worker
    $sandboxID = docker compose --env-file $resolvedEnv @files ps -q sandbox-engine
    if (-not $apiID -or -not $workerID -or -not $sandboxID) { throw "API, Worker, and Sandbox Engine must be running" }
    $apiInspect = docker inspect $apiID | ConvertFrom-Json
    $workerInspect = docker inspect $workerID | ConvertFrom-Json
    $sandboxInspect = docker inspect $sandboxID | ConvertFrom-Json
    if (@($apiInspect[0].Mounts | Where-Object { $_.Destination -match 'docker\.sock|openai_api_key|/certs/client' }).Count -gt 0) { throw "API received a forbidden execution-plane mount" }
    if (@($workerInspect[0].Mounts | Where-Object { $_.Destination -match 'docker\.sock' }).Count -gt 0) { throw "Worker received the host Docker Socket" }
    if ($sandboxInspect[0].HostConfig.PortBindings) { throw "Sandbox Engine unexpectedly publishes a host port" }
    foreach ($service in @($apiInspect[0], $workerInspect[0])) {
        if (-not $service.HostConfig.ReadonlyRootfs) { throw "API/Worker read-only root filesystem is not active" }
        if ($service.HostConfig.CapDrop -notcontains 'ALL') { throw "API/Worker capability drop is incomplete" }
        if ($service.HostConfig.SecurityOpt -notcontains 'no-new-privileges:true') { throw "API/Worker no-new-privileges is not active" }
    }
    $workerEnvironment = @($workerInspect[0].Config.Env)
    if ($workerEnvironment -notcontains 'DOCKER_TLS_VERIFY=1' -or $workerEnvironment -notcontains 'DOCKER_HOST=tcp://sandbox-engine:2376') {
        throw "Worker does not use the mTLS Sandbox Engine endpoint"
    }

    docker compose --env-file $resolvedEnv @files exec -T api sh -c 'test ! -e /run/secrets/openai_api_key && test ! -e /certs/client'
    if ($LASTEXITCODE -ne 0) { throw "API can access a model or Sandbox credential" }
    docker compose --env-file $resolvedEnv @files exec -T api sh -c 'wget -q -T 3 -O /dev/null https://example.com'
    if ($LASTEXITCODE -eq 0) { throw "API unexpectedly has public network egress" }
    docker compose --env-file $resolvedEnv @files exec -T worker sh -c 'wget -q -T 3 -O /dev/null http://api:8080/healthz'
    if ($LASTEXITCODE -eq 0) { throw "Worker unexpectedly reaches the API control-plane network" }
}
finally { Pop-Location }

Write-Host "Live Staging control-plane/execution-plane security drill passed. Task-container and secret-redaction evidence must still be captured from a real Sandbox Run."
