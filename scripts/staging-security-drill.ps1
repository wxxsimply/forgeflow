param(
    [string]$EnvFile = "deploy/staging/staging.env",
    [switch]$IncludeOpenAI
)

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot
$deployDirectory = Join-Path $workspace "deploy/staging"
$compose = Join-Path $deployDirectory "compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
$files = @('-f', $compose)
if ($IncludeOpenAI) { $files += @('-f', (Join-Path $deployDirectory 'compose.openai.yaml')) }

Push-Location $deployDirectory
try {
    $apiID = docker compose --env-file $resolvedEnv @files ps -q api
    $workerID = docker compose --env-file $resolvedEnv @files ps -q worker
    if (-not $apiID -or -not $workerID) { throw "API and Worker must be running" }
    $apiInspect = docker inspect $apiID | ConvertFrom-Json
    $workerInspect = docker inspect $workerID | ConvertFrom-Json
    if (@($apiInspect[0].Mounts | Where-Object { $_.Destination -match 'docker\.sock|openai_api_key' }).Count -gt 0) { throw "API received a forbidden mount" }
    if (@($workerInspect[0].Mounts | Where-Object { $_.Destination -match 'docker\.sock' }).Count -gt 0) { throw "Worker received the host Docker Socket" }
    if (-not $apiInspect[0].HostConfig.ReadonlyRootfs -or -not $workerInspect[0].HostConfig.ReadonlyRootfs) { throw "Read-only root filesystem is not active" }
    if (-not $apiInspect[0].HostConfig.SecurityOpt -or $apiInspect[0].HostConfig.SecurityOpt -notcontains 'no-new-privileges:true') { throw "API no-new-privileges is not active" }

    docker compose --env-file $resolvedEnv @files exec -T api sh -c 'test ! -e /run/secrets/openai_api_key'
    if ($LASTEXITCODE -ne 0) { throw "API can access the model secret" }
    docker compose --env-file $resolvedEnv @files exec -T api sh -c 'wget -q -T 3 -O /dev/null https://example.com'
    if ($LASTEXITCODE -eq 0) { throw "API unexpectedly has public network egress" }
    docker compose --env-file $resolvedEnv @files exec -T worker sh -c 'wget -q -T 3 -O /dev/null http://api:8080/healthz'
    if ($LASTEXITCODE -eq 0) { throw "Worker unexpectedly reaches the API control-plane network" }
    Write-Host "Staging control-plane/execution-plane security drill passed."
}
finally { Pop-Location }
