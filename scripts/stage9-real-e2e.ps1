param(
    [Parameter(Mandatory = $true)]
    [string]$Repository,
    [string]$Email = "stage9-admin@example.com",
    [string]$Password = "Stage9-Admin-Password-Only-For-E2E!",
    [int]$PostgresPort = 55439
)

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot
$repositoryPath = (Resolve-Path -LiteralPath $Repository).Path
$container = "forgeflow-stage9-pg-e2e"
$apiProcess = $null
$workerProcess = $null
$cache = Join-Path $workspace ".cache"
$bin = Join-Path $workspace "bin"
$apiErrorLog = Join-Path $cache "stage9-api.err.log"
$workerErrorLog = Join-Path $cache "stage9-worker.err.log"

Push-Location $repositoryPath
try {
    git rev-parse --verify HEAD | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Repository must contain at least one commit" }
}
finally { Pop-Location }

New-Item -ItemType Directory -Force -Path $cache, $bin | Out-Null
$existing = docker ps -a --filter "name=^/$container$" --format "{{.Names}}"
if ($existing -eq $container) { docker rm -f $container | Out-Null }

try {
    docker run -d --name $container `
        -e POSTGRES_USER=forgeflow `
        -e POSTGRES_PASSWORD=forgeflow `
        -e POSTGRES_DB=forgeflow `
        -p "${PostgresPort}:5432" postgres:17-alpine | Out-Null

    $databaseReady = $false
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        docker exec $container pg_isready -U forgeflow -d forgeflow 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { $databaseReady = $true; break }
        Start-Sleep -Seconds 1
    }
    if (-not $databaseReady) { throw "PostgreSQL did not become ready" }

    $env:GOCACHE = Join-Path $cache "go-build"
    $env:FORGEFLOW_ENV = "test"
    $env:FORGEFLOW_POSTGRES_ENABLED = "true"
    $env:FORGEFLOW_POSTGRES_DSN = "postgres://forgeflow:forgeflow@127.0.0.1:$PostgresPort/forgeflow?sslmode=disable"
    $env:FORGEFLOW_HTTP_ADDRESS = "127.0.0.1:8080"
    $env:FORGEFLOW_HTTP_COOKIE_SECURE = "false"
    $env:FORGEFLOW_HTTP_ALLOWED_ORIGINS = "http://127.0.0.1:5173"
    $env:FORGEFLOW_REPOSITORY_ROOTS = $repositoryPath
    $env:FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL = $Email
    $env:FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD = $Password
    $env:FORGEFLOW_PLANNER_MODE = "mock"
    $env:FORGEFLOW_WORKER_POLL_INTERVAL = "100ms"
    $env:FORGEFLOW_REAL_E2E = "1"
    $env:FORGEFLOW_E2E_EMAIL = $Email
    $env:FORGEFLOW_E2E_PASSWORD = $Password
    $env:FORGEFLOW_E2E_REPOSITORY = $repositoryPath
    Remove-Item Env:OPENAI_API_KEY -ErrorAction SilentlyContinue

    Push-Location $workspace
    try {
        go build -trimpath -o (Join-Path $bin "forgeflow.exe") ./cmd/forgeflow
        go build -trimpath -o (Join-Path $bin "forgeflow-api.exe") ./cmd/forgeflow-api
        go build -trimpath -o (Join-Path $bin "forgeflow-worker.exe") ./cmd/forgeflow-worker
    }
    finally { Pop-Location }

    $migrated = $false
    for ($attempt = 0; $attempt -lt 10; $attempt++) {
        & (Join-Path $bin "forgeflow.exe") db migrate
        if ($LASTEXITCODE -eq 0) { $migrated = $true; break }
        Start-Sleep -Seconds 2
    }
    if (-not $migrated) { throw "Database migration failed after retries" }

    $apiProcess = Start-Process -FilePath (Join-Path $bin "forgeflow-api.exe") -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $cache "stage9-api.log") -RedirectStandardError $apiErrorLog
    $workerProcess = Start-Process -FilePath (Join-Path $bin "forgeflow-worker.exe") -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $cache "stage9-worker.log") -RedirectStandardError $workerErrorLog
    $apiReady = $false
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:8080/healthz" -TimeoutSec 2
            if ($response.StatusCode -eq 200) { $apiReady = $true; break }
        }
        catch { }
        Start-Sleep -Seconds 1
    }
    if (-not $apiReady) { throw "API did not become ready" }

    Push-Location (Join-Path $workspace "web")
    try { npm run test:e2e:real }
    finally { Pop-Location }
    if ($LASTEXITCODE -ne 0) { throw "Real browser E2E failed" }
    Write-Host "Stage 9 real PostgreSQL/API/Worker/browser E2E passed."
}
catch {
    if (Test-Path $apiErrorLog) { Get-Content $apiErrorLog -Tail 60 }
    if (Test-Path $workerErrorLog) { Get-Content $workerErrorLog -Tail 60 }
    throw
}
finally {
    if ($apiProcess -and -not $apiProcess.HasExited) { Stop-Process -Id $apiProcess.Id -Force }
    if ($workerProcess -and -not $workerProcess.HasExited) { Stop-Process -Id $workerProcess.Id -Force }
    $exact = docker ps -a --filter "name=^/$container$" --format "{{.Names}}"
    if ($exact -eq $container) { docker rm -f $container | Out-Null }
}
