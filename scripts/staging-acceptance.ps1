[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [uri]$BaseUri,
    [Parameter(Mandatory = $true)]
    [string]$ExpectedRelease,
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[a-f0-9]{40}$')]
    [string]$ExpectedGitCommit,
    [Parameter(Mandatory = $true)]
    [string]$Manifest,
    [Parameter(Mandatory = $true)]
    [string]$Email,
    [Parameter(Mandatory = $true)]
    [Security.SecureString]$Password,
    [Parameter(Mandatory = $true)]
    [string]$RepositoryHostPath,
    [string]$RepositoryContainerPath = "/repositories/demo",
    [string]$EnvFile = "deploy/staging/staging.env",
    [switch]$IncludeOpenAI
)

$ErrorActionPreference = "Stop"
if ($BaseUri.Scheme -ne 'https') { throw "Staging acceptance requires HTTPS" }
if ($BaseUri.PathAndQuery -ne '/') { throw "BaseUri must not contain a path or query" }
if ($ExpectedRelease -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$') { throw "ExpectedRelease must be a semantic version" }

$workspace = Split-Path -Parent $PSScriptRoot
$repository = (Resolve-Path -LiteralPath $RepositoryHostPath).Path
$origin = $BaseUri.GetLeftPart([System.UriPartial]::Authority).TrimEnd('/')
$manifestPath = [System.IO.Path]::GetFullPath((Join-Path $workspace $Manifest))
$evidenceDirectory = Join-Path $workspace ".forgeflow/staging/acceptance"
New-Item -ItemType Directory -Force -Path $evidenceDirectory | Out-Null

& (Join-Path $PSScriptRoot 'staging-preflight.ps1') -EnvFile $EnvFile -Manifest $Manifest -IncludeOpenAI:$IncludeOpenAI -RequireDigests
$releaseManifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if ($releaseManifest.release -ne $ExpectedRelease -or $releaseManifest.gitCommit -ne $ExpectedGitCommit) {
    throw "Expected release does not match the Release manifest"
}

Push-Location $repository
try {
    $repositoryCommitBefore = (& git rev-parse HEAD)
    if ($LASTEXITCODE -ne 0 -or $repositoryCommitBefore -notmatch '^[a-f0-9]{40}$') { throw "Acceptance repository must contain a Git HEAD" }
    $repositoryStatusBefore = ((& git status --porcelain=v1 --untracked-files=all) -join [Environment]::NewLine)
    if ($LASTEXITCODE -ne 0 -or $repositoryStatusBefore) { throw "Acceptance repository must be clean before the test" }
    $repositoryWorktreesBefore = ((& git worktree list --porcelain) -join [Environment]::NewLine)
    if ($LASTEXITCODE -ne 0) { throw "Could not capture acceptance repository worktrees" }
}
finally { Pop-Location }

function Assert-Metadata {
    param([object]$Metadata, [string]$Service)
    if ($Metadata.serviceVersion -ne $ExpectedRelease -or $Metadata.gitCommit -ne $ExpectedGitCommit) {
        throw "$Service release metadata does not match the expected release"
    }
}

$healthResponse = Invoke-WebRequest -UseBasicParsing "$origin/healthz" -TimeoutSec 30
if ($healthResponse.StatusCode -ne 200) { throw "Public API health check failed" }
$apiMetadata = $healthResponse.Content | ConvertFrom-Json
Assert-Metadata $apiMetadata 'API'

$webResponse = Invoke-WebRequest -UseBasicParsing "$origin/release.json" -TimeoutSec 30
if ($webResponse.StatusCode -ne 200) { throw "Public Web release metadata check failed" }
$webMetadata = $webResponse.Content | ConvertFrom-Json
Assert-Metadata $webMetadata 'Web'

$handler = [System.Net.Http.HttpClientHandler]::new()
$handler.AllowAutoRedirect = $false
$client = [System.Net.Http.HttpClient]::new($handler)
try {
    $httpUri = [uri]("http://" + $BaseUri.Host + "/healthz")
    $redirect = $client.GetAsync($httpUri).GetAwaiter().GetResult()
    if ([int]$redirect.StatusCode -notin @(301, 302, 307, 308) -or -not $redirect.Headers.Location) {
        throw "HTTP does not redirect to HTTPS"
    }
    $redirectTarget = if ($redirect.Headers.Location.IsAbsoluteUri) {
        $redirect.Headers.Location
    }
    else {
        [uri]::new($httpUri, $redirect.Headers.Location)
    }
    if ($redirectTarget.Scheme -ne 'https') { throw "HTTP redirect target is not HTTPS" }
}
finally {
    $client.Dispose()
    $handler.Dispose()
}

$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
$files = @('-f', (Join-Path $workspace 'deploy/staging/compose.yaml'))
if ($IncludeOpenAI) { $files += @('-f', (Join-Path $workspace 'deploy/staging/compose.openai.yaml')) }
$workerRaw = (& docker compose --env-file $resolvedEnv @files exec -T worker wget -qO- http://127.0.0.1:9091/readyz)
if ($LASTEXITCODE -ne 0) { throw "Worker readiness check failed" }
$workerMetadata = ($workerRaw -join [Environment]::NewLine) | ConvertFrom-Json
Assert-Metadata $workerMetadata 'Worker'
if ($workerMetadata.status -ne 'ready') { throw "Worker Prompt/model release is not ready" }

$credential = New-Object System.Management.Automation.PSCredential('unused', $Password)
$plainPassword = $credential.GetNetworkCredential().Password
$savedEnvironment = @{}
$e2eFailure = $null
foreach ($name in @('FORGEFLOW_REAL_E2E', 'FORGEFLOW_STAGING_BASE_URL', 'FORGEFLOW_E2E_EMAIL', 'FORGEFLOW_E2E_PASSWORD', 'FORGEFLOW_E2E_REPOSITORY')) {
    $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
try {
    $env:FORGEFLOW_REAL_E2E = '1'
    $env:FORGEFLOW_STAGING_BASE_URL = $origin
    $env:FORGEFLOW_E2E_EMAIL = $Email
    $env:FORGEFLOW_E2E_PASSWORD = $plainPassword
    $env:FORGEFLOW_E2E_REPOSITORY = $RepositoryContainerPath
    Push-Location (Join-Path $workspace 'web')
    try {
        npm run test:e2e:staging
        if ($LASTEXITCODE -ne 0) { throw "Staging browser E2E failed" }
    }
    finally { Pop-Location }
}
catch { $e2eFailure = $_ }
finally {
    $plainPassword = $null
    foreach ($name in $savedEnvironment.Keys) {
        $value = $savedEnvironment[$name]
        if ($null -eq $value) { Remove-Item "Env:$name" -ErrorAction SilentlyContinue }
        else { Set-Item "Env:$name" $value }
    }
}

Push-Location $repository
try {
    $repositoryCommitAfter = (& git rev-parse HEAD)
    $repositoryStatusAfter = ((& git status --porcelain=v1 --untracked-files=all) -join [Environment]::NewLine)
    $repositoryWorktreesAfter = ((& git worktree list --porcelain) -join [Environment]::NewLine)
}
finally { Pop-Location }
if ($LASTEXITCODE -ne 0 -or $repositoryCommitAfter -ne $repositoryCommitBefore -or $repositoryStatusAfter -ne $repositoryStatusBefore -or $repositoryWorktreesAfter -ne $repositoryWorktreesBefore) {
    throw "Acceptance changed the original repository"
}
if ($e2eFailure) { throw $e2eFailure }

$evidence = [ordered]@{
    schemaVersion = 'forgeflow.staging-acceptance/v1'
    checkedAt = (Get-Date).ToUniversalTime().ToString('o')
    baseUri = $origin
    release = $ExpectedRelease
    gitCommit = $ExpectedGitCommit
    https = 'passed'
    httpRedirect = 'passed'
    api = $apiMetadata
    worker = $workerMetadata
    web = $webMetadata
    browserE2E = 'passed'
    repositoryCommitBefore = $repositoryCommitBefore
    repositoryCommitAfter = $repositoryCommitAfter
    repositoryUnchanged = $true
    repositoryWorktreesUnchanged = $true
}
$evidencePath = Join-Path $evidenceDirectory "$ExpectedRelease-$([DateTimeOffset]::UtcNow.ToString('yyyyMMddTHHmmssZ')).json"
$evidence | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8 -LiteralPath $evidencePath
Write-Host "Staging acceptance passed. Evidence: $evidencePath"
