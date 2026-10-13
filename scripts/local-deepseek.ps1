param(
    [ValidateSet('init', 'start', 'stop', 'status')]
    [string]$Action = 'start',
    [string]$RepositoryPath = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if (-not [OperatingSystem]::IsWindows()) {
    throw 'This local launcher currently supports Windows PowerShell 7 only.'
}

$workspace = Split-Path -Parent $PSScriptRoot
$localRoot = Join-Path $workspace '.forgeflow/local'
$secrets = Join-Path $localRoot 'secrets'
$compose = Join-Path $workspace 'deploy/local/compose.yaml'
$keyFile = Join-Path $secrets 'deepseek_api_key'
$postgresPasswordFile = Join-Path $secrets 'postgres_password'
$postgresDSNFile = Join-Path $secrets 'postgres_dsn'
$adminPasswordFile = Join-Path $secrets 'bootstrap_admin_password'
$processFile = Join-Path $localRoot 'processes.json'
$adminEmail = 'admin@forgeflow.local'

function Invoke-Checked([string]$Label, [scriptblock]$Command) {
    & $Command
    if ($LASTEXITCODE -ne 0) { throw "$Label failed (exit $LASTEXITCODE)." }
}

function Initialize-LocalSecrets {
    New-Item -ItemType Directory -Force -Path $secrets | Out-Null
    if (-not (Test-Path -LiteralPath $postgresPasswordFile)) {
        $password = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(24)).ToLowerInvariant()
        [IO.File]::WriteAllText($postgresPasswordFile, $password)
    }
    if (-not (Test-Path -LiteralPath $postgresDSNFile)) {
        $password = [IO.File]::ReadAllText($postgresPasswordFile).Trim()
        if ($password -notmatch '^[a-f0-9]{48}$') { throw 'Local PostgreSQL password file has an unexpected format.' }
        [IO.File]::WriteAllText($postgresDSNFile, "postgres://forgeflow:${password}@127.0.0.1:55432/forgeflow?sslmode=disable")
    }
    if (-not (Test-Path -LiteralPath $adminPasswordFile)) {
        $password = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(24)).ToLowerInvariant()
        [IO.File]::WriteAllText($adminPasswordFile, $password)
    }
    if (-not (Test-Path -LiteralPath $keyFile)) {
        [IO.File]::WriteAllText($keyFile, '')
    }
    Write-Host "Local secrets are Git-ignored: $secrets"
    Write-Host "Paste your own low-balance DeepSeek API key into: $keyFile"
    Write-Host "Local login: $adminEmail; password is in: $adminPasswordFile"
    Write-Host 'Do not commit, paste into chat, or share these files.'
}

function Test-LocalProcess([object]$record) {
    try {
        $process = Get-Process -Id ([int]$record.id) -ErrorAction Stop
        $path = $process.Path
        return $path -and [IO.Path]::GetFullPath($path).Equals([IO.Path]::GetFullPath([string]$record.path), [StringComparison]::OrdinalIgnoreCase) -and
            $process.StartTime.ToUniversalTime() -eq ([datetime]$record.startedAt).ToUniversalTime()
    } catch { return $false }
}

function Stop-LocalProcesses {
    if (Test-Path -LiteralPath $processFile) {
        $records = @(Get-Content -Raw -LiteralPath $processFile | ConvertFrom-Json)
        foreach ($record in $records) {
            if (Test-LocalProcess $record) { Stop-Process -Id ([int]$record.id) -ErrorAction Stop }
        }
        Remove-Item -LiteralPath $processFile
    }
}

function Wait-LocalHTTP([string]$Uri, [string]$Label) {
    for ($attempt = 0; $attempt -lt 45; $attempt++) {
        try {
            $response = Invoke-WebRequest -Uri $Uri -TimeoutSec 2 -UseBasicParsing
            if ($response.StatusCode -eq 200) { return }
        } catch { }
        Start-Sleep -Seconds 1
    }
    throw "$Label did not become ready. Inspect logs in $localRoot/logs."
}

if ($Action -eq 'init') { Initialize-LocalSecrets; return }
if ($Action -eq 'status') {
    if (Test-Path -LiteralPath $processFile) {
        @(Get-Content -Raw -LiteralPath $processFile | ConvertFrom-Json) | ForEach-Object {
            Write-Host "$($_.name): $(if (Test-LocalProcess $_) { 'running' } else { 'stopped' })"
        }
    } else { Write-Host 'No local ForgeFlow processes recorded.' }
    return
}
if ($Action -eq 'stop') {
    Stop-LocalProcesses
    if (Get-Command docker -ErrorAction SilentlyContinue) {
        Invoke-Checked 'Stop local PostgreSQL' { docker compose -f $compose down }
    }
    Write-Host 'Local processes stopped; database volume and secret files were kept.'
    return
}

foreach ($name in @('docker', 'go', 'node', 'npm')) {
    if (-not (Get-Command $name -ErrorAction SilentlyContinue)) { throw "$name is required. Install it and retry." }
}
Invoke-Checked 'Docker Desktop preflight' { docker info --format '{{.ServerVersion}}' }
Invoke-Checked 'Docker Compose preflight' { docker compose version }
foreach ($file in @($keyFile, $postgresPasswordFile, $postgresDSNFile, $adminPasswordFile)) {
    if (-not (Test-Path -LiteralPath $file)) { throw "Missing $file. Run ./scripts/local-deepseek.ps1 -Action init first." }
}
$apiKey = [IO.File]::ReadAllText($keyFile).Trim()
if ($apiKey.Length -lt 8 -or $apiKey -match '[\r\n\x00]') { throw 'DeepSeek key file must contain exactly one nonempty key line.' }
Remove-Variable apiKey

if (Test-Path -LiteralPath $processFile) {
    $existing = @(Get-Content -Raw -LiteralPath $processFile | ConvertFrom-Json)
    if (@($existing | Where-Object { Test-LocalProcess $_ }).Count -gt 0) {
        throw 'Local ForgeFlow is already running. Use -Action stop before restarting.'
    }
    Remove-Item -LiteralPath $processFile
}

$repository = if ($RepositoryPath) { $RepositoryPath } else { $workspace }
$repository = (Resolve-Path -LiteralPath $repository).Path
Invoke-Checked 'Git repository preflight' { git -C $repository rev-parse --verify HEAD | Out-Null }
$gitStatus = @(git -C $repository status --porcelain)
if ($LASTEXITCODE -ne 0 -or $gitStatus.Count -gt 0) { throw 'The selected repository must have a clean worktree before running development tasks.' }

$logs = Join-Path $localRoot 'logs'
$bin = Join-Path $localRoot 'bin'
$workspaces = Join-Path $localRoot 'workspaces'
foreach ($directory in @($logs, $bin, $workspaces, (Join-Path $localRoot 'artifacts'))) {
    New-Item -ItemType Directory -Force -Path $directory | Out-Null
}

Push-Location $workspace
$savedEnvironment = @{}
$apiProcess = $null
$workerProcess = $null
$webProcess = $null
$names = @(
    'FORGEFLOW_ENV', 'FORGEFLOW_POSTGRES_ENABLED', 'FORGEFLOW_POSTGRES_DSN', 'FORGEFLOW_POSTGRES_DSN_FILE',
    'FORGEFLOW_CONTROLLED_ADMIN_EMAIL', 'FORGEFLOW_CONTROLLED_REPOSITORY_PATH', 'FORGEFLOW_REPOSITORY_ROOTS',
    'FORGEFLOW_HTTP_ADDRESS', 'FORGEFLOW_HTTP_COOKIE_SECURE', 'FORGEFLOW_HTTP_ALLOWED_ORIGINS',
    'FORGEFLOW_DATA_DIR', 'FORGEFLOW_ARTIFACT_ROOT', 'FORGEFLOW_ARTIFACT_BACKEND', 'FORGEFLOW_AUDIT_BACKEND',
    'FORGEFLOW_OTEL_ENDPOINT', 'FORGEFLOW_ADMIN_MFA_REQUIRED', 'FORGEFLOW_GOVERNANCE_ENFORCE_ACTIVE_RELEASES',
    'FORGEFLOW_WORKFLOW_MODE', 'FORGEFLOW_PLANNER_MODE',
    'FORGEFLOW_CONTROLLED_DEVELOPMENT_API', 'FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL', 'FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD',
    'FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD_FILE', 'FORGEFLOW_DOCKER_ENABLED', 'FORGEFLOW_SANDBOX_IMAGE',
    'FORGEFLOW_SANDBOX_WORKSPACE_ROOT', 'FORGEFLOW_MODEL_PROVIDER', 'FORGEFLOW_OPENAI_BASE_URL',
    'FORGEFLOW_OPENAI_MAX_RETRIES', 'FORGEFLOW_PLANNER_MODEL', 'FORGEFLOW_DEVELOPER_MODEL',
    'FORGEFLOW_REVIEWER_MODEL', 'FORGEFLOW_SECURITY_MODEL', 'FORGEFLOW_PLANNER_INPUT_USD_PER_MTOK',
    'FORGEFLOW_PLANNER_OUTPUT_USD_PER_MTOK', 'FORGEFLOW_PLANNER_MAX_OUTPUT_TOKENS',
    'FORGEFLOW_DEVELOPER_MAX_OUTPUT_TOKENS', 'FORGEFLOW_REVIEWER_MAX_OUTPUT_TOKENS',
    'FORGEFLOW_SECURITY_MAX_OUTPUT_TOKENS', 'OPENAI_API_KEY', 'OPENAI_API_KEY_FILE',
    'VITE_LOCAL_CONTROLLED_DEVELOPMENT'
)
foreach ($name in $names) { $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
try {
    Invoke-Checked 'Start local PostgreSQL' { docker compose -f $compose up -d --wait }
    $sandboxTag = 'forgeflow-local-sandbox:dev'
    docker image inspect $sandboxTag --format '{{.Id}}' *> $null
    if ($LASTEXITCODE -ne 0) {
        Invoke-Checked 'Build local sandbox image' {
            docker build --build-arg GO_IMAGE=public.ecr.aws/docker/library/golang:1.26.9-alpine -f deploy/sandbox/Dockerfile -t $sandboxTag .
        }
    }
    $sandboxID = (docker image inspect $sandboxTag --format '{{.Id}}').Trim()
    if ($LASTEXITCODE -ne 0 -or $sandboxID -notmatch '^sha256:[a-f0-9]{64}$') { throw 'Local sandbox image ID is invalid.' }

    $env:FORGEFLOW_ENV = 'development'
    $env:FORGEFLOW_POSTGRES_ENABLED = 'true'
    $env:FORGEFLOW_POSTGRES_DSN = ''
    $env:FORGEFLOW_POSTGRES_DSN_FILE = $postgresDSNFile
    $env:FORGEFLOW_CONTROLLED_ADMIN_EMAIL = $adminEmail
    $env:FORGEFLOW_CONTROLLED_REPOSITORY_PATH = $repository
    $env:FORGEFLOW_REPOSITORY_ROOTS = $repository
    $env:FORGEFLOW_HTTP_ADDRESS = '127.0.0.1:8080'
    $env:FORGEFLOW_HTTP_COOKIE_SECURE = 'false'
    $env:FORGEFLOW_HTTP_ALLOWED_ORIGINS = 'http://127.0.0.1:5173'
    $env:FORGEFLOW_DATA_DIR = $localRoot
    $env:FORGEFLOW_ARTIFACT_ROOT = Join-Path $localRoot 'artifacts'
    $env:FORGEFLOW_ARTIFACT_BACKEND = 'file'
    $env:FORGEFLOW_AUDIT_BACKEND = 'postgres'
    $env:FORGEFLOW_OTEL_ENDPOINT = ''
    $env:FORGEFLOW_ADMIN_MFA_REQUIRED = 'false'
    $env:FORGEFLOW_GOVERNANCE_ENFORCE_ACTIVE_RELEASES = 'false'
    $env:FORGEFLOW_WORKFLOW_MODE = 'planning'
    $env:FORGEFLOW_PLANNER_MODE = 'mock'
    $env:FORGEFLOW_CONTROLLED_DEVELOPMENT_API = 'false'
    $env:FORGEFLOW_DOCKER_ENABLED = 'false'
    $env:FORGEFLOW_MODEL_PROVIDER = 'deepseek'
    $env:FORGEFLOW_OPENAI_BASE_URL = 'https://api.deepseek.com'
    $env:FORGEFLOW_OPENAI_MAX_RETRIES = '0'
    $env:FORGEFLOW_PLANNER_MODEL = 'deepseek-flash'
    $env:FORGEFLOW_DEVELOPER_MODEL = 'deepseek-flash'
    $env:FORGEFLOW_REVIEWER_MODEL = 'deepseek-flash'
    $env:FORGEFLOW_SECURITY_MODEL = 'deepseek-flash'
    $env:FORGEFLOW_PLANNER_INPUT_USD_PER_MTOK = '0.3'
    $env:FORGEFLOW_PLANNER_OUTPUT_USD_PER_MTOK = '1.2'
    $env:FORGEFLOW_PLANNER_MAX_OUTPUT_TOKENS = '4096'
    $env:FORGEFLOW_DEVELOPER_MAX_OUTPUT_TOKENS = '8000'
    $env:FORGEFLOW_REVIEWER_MAX_OUTPUT_TOKENS = '3000'
    $env:FORGEFLOW_SECURITY_MAX_OUTPUT_TOKENS = '3000'
    $env:OPENAI_API_KEY = ''
    $env:OPENAI_API_KEY_FILE = ''

    $cli = Join-Path $bin 'forgeflow.exe'
    $api = Join-Path $bin 'forgeflow-api.exe'
    $worker = Join-Path $bin 'forgeflow-worker.exe'
    foreach ($build in @(@($cli, './cmd/forgeflow'), @($api, './cmd/forgeflow-api'), @($worker, './cmd/forgeflow-worker'))) {
        Invoke-Checked "Build $($build[1])" { go build -trimpath -o $build[0] $build[1] }
    }
    Invoke-Checked 'Migrate local database' { & $cli db migrate }

    $env:FORGEFLOW_CONTROLLED_DEVELOPMENT_API = 'true'
    $env:FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL = $adminEmail
    $env:FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD = ''
    $env:FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD_FILE = $adminPasswordFile
    $apiProcess = Start-Process -FilePath $api -WorkingDirectory $workspace -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $logs 'api.out.log') -RedirectStandardError (Join-Path $logs 'api.err.log')
    Wait-LocalHTTP 'http://127.0.0.1:8080/healthz' 'API'

    $env:FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL = ''
    $env:FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD_FILE = ''
    $env:FORGEFLOW_WORKFLOW_MODE = 'controlled_development'
    $env:FORGEFLOW_PLANNER_MODE = 'openai'
    $env:FORGEFLOW_CONTROLLED_DEVELOPMENT_API = 'false'
    $env:FORGEFLOW_DOCKER_ENABLED = 'true'
    $env:FORGEFLOW_SANDBOX_IMAGE = $sandboxID
    $env:FORGEFLOW_SANDBOX_WORKSPACE_ROOT = $workspaces
    $env:OPENAI_API_KEY_FILE = $keyFile
    $workerProcess = Start-Process -FilePath $worker -WorkingDirectory $workspace -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $logs 'worker.out.log') -RedirectStandardError (Join-Path $logs 'worker.err.log')
    Wait-LocalHTTP 'http://127.0.0.1:9091/healthz' 'Worker'

    $env:OPENAI_API_KEY_FILE = ''
    $env:VITE_LOCAL_CONTROLLED_DEVELOPMENT = 'true'
    Push-Location (Join-Path $workspace 'web')
    try {
        if (-not (Test-Path -LiteralPath 'node_modules')) { Invoke-Checked 'Install web dependencies' { npm ci --no-audit --no-fund } }
        $node = (Get-Command node).Source
        $vite = Join-Path (Get-Location).Path 'node_modules/vite/bin/vite.js'
        $webProcess = Start-Process -FilePath $node -ArgumentList @($vite, '--host', '127.0.0.1') `
            -WorkingDirectory (Get-Location).Path -WindowStyle Hidden -PassThru `
            -RedirectStandardOutput (Join-Path $logs 'web.out.log') -RedirectStandardError (Join-Path $logs 'web.err.log')
    } finally { Pop-Location }
    Wait-LocalHTTP 'http://127.0.0.1:5173/' 'Web'

    $records = @(
        @{ name = 'api'; id = $apiProcess.Id; path = $api; startedAt = $apiProcess.StartTime.ToUniversalTime().ToString('o') },
        @{ name = 'worker'; id = $workerProcess.Id; path = $worker; startedAt = $workerProcess.StartTime.ToUniversalTime().ToString('o') },
        @{ name = 'web'; id = $webProcess.Id; path = $node; startedAt = $webProcess.StartTime.ToUniversalTime().ToString('o') }
    )
    $records | ConvertTo-Json | Set-Content -LiteralPath $processFile -Encoding utf8
    Write-Host 'Local ForgeFlow is ready at http://127.0.0.1:5173'
    Write-Host "Login: $adminEmail; password file: $adminPasswordFile"
    Write-Host 'Only the Worker receives the DeepSeek key file. This setup is for your own computer, not a public server.'
} catch {
    foreach ($candidate in @($webProcess, $workerProcess, $apiProcess)) {
        if ($null -ne $candidate) { Stop-Process -Id $candidate.Id -ErrorAction SilentlyContinue }
    }
    throw
} finally {
    foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process') }
    Pop-Location
}
