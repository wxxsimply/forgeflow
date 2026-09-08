[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [uri]$BaseUri,
    [Parameter(Mandatory = $true)]
    [string]$Email,
    [Parameter(Mandatory = $true)]
    [Security.SecureString]$Password,
    [string]$EnvFile = "deploy/staging/staging.env",
    [Parameter(Mandatory = $true)]
    [switch]$ConfirmRemoval
)

$ErrorActionPreference = "Stop"
if (-not $ConfirmRemoval) { throw "Bootstrap cleanup requires -ConfirmRemoval" }
if ($BaseUri.Scheme -ne 'https') { throw "Bootstrap verification requires HTTPS" }
if ($BaseUri.PathAndQuery -ne '/') { throw "BaseUri must not contain a path or query" }
$workspace = Split-Path -Parent $PSScriptRoot
$deployDirectory = Join-Path $workspace "deploy/staging"
$secretRoot = [System.IO.Path]::GetFullPath((Join-Path $deployDirectory 'secrets'))
$secretPath = [System.IO.Path]::GetFullPath((Join-Path $secretRoot 'bootstrap_admin_password'))
if (-not $secretPath.StartsWith($secretRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Unsafe bootstrap secret path"
}
if (-not (Test-Path -LiteralPath $secretPath -PathType Leaf)) { throw "Bootstrap secret does not exist" }
if ((Get-Item -LiteralPath $secretPath).LinkType) { throw "Bootstrap secret must not be a symbolic link" }

$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
$environment = @{}
Get-Content -LiteralPath $resolvedEnv | ForEach-Object {
    $line = $_.Trim()
    if ($line -and -not $line.StartsWith('#') -and $line.Contains('=')) {
        $name, $value = $line.Split('=', 2)
        $environment[$name.Trim()] = $value.Trim()
    }
}
if ($environment.FORGEFLOW_RELEASE -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$' -or $environment.FORGEFLOW_GIT_COMMIT -notmatch '^[a-f0-9]{40}$') {
    throw "Staging environment is missing valid release metadata"
}
docker compose --env-file $resolvedEnv -f (Join-Path $deployDirectory 'compose.yaml') config --quiet
if ($LASTEXITCODE -ne 0) { throw "Base Compose validation failed before bootstrap secret removal" }

$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$credential = New-Object System.Management.Automation.PSCredential('unused', $Password)
$plainPassword = $credential.GetNetworkCredential().Password
$origin = $BaseUri.GetLeftPart([System.UriPartial]::Authority).TrimEnd('/')
try {
    $body = @{ email = $Email; password = $plainPassword; remember = $false } | ConvertTo-Json
    $loginResponse = Invoke-WebRequest -Method POST -Uri "$origin/api/v1/auth/login" -WebSession $session -ContentType 'application/json' -Body $body -UseBasicParsing -TimeoutSec 30
    $login = $loginResponse.Content | ConvertFrom-Json
    if ($loginResponse.StatusCode -ne 200 -or -not $login.csrfToken) { throw "Bootstrap administrator login verification failed" }
    try {
        Invoke-WebRequest -Method POST -Uri "$origin/api/v1/auth/logout" -WebSession $session -Headers @{'X-CSRF-Token' = $login.csrfToken} -ContentType 'application/json' -Body '{}' -UseBasicParsing -TimeoutSec 30 | Out-Null
    }
    catch { }
}
finally { $plainPassword = $null }

Remove-Item -LiteralPath $secretPath -Force
if (Test-Path -LiteralPath $secretPath) { throw "Bootstrap secret removal failed" }

Push-Location $deployDirectory
try {
    docker compose --env-file $resolvedEnv -f compose.yaml up -d --no-build --force-recreate --wait --wait-timeout 180 api
    if ($LASTEXITCODE -ne 0) { throw "API recreation without bootstrap override failed" }
    $renderedRaw = docker compose --env-file $resolvedEnv -f compose.yaml config --format json
    if ($LASTEXITCODE -ne 0) { throw "Base Compose validation failed after bootstrap cleanup" }
    $rendered = ($renderedRaw -join [Environment]::NewLine) | ConvertFrom-Json
    $apiJSON = $rendered.services.api | ConvertTo-Json -Depth 20 -Compress
    if ($apiJSON -match 'bootstrap_admin_password|FORGEFLOW_BOOTSTRAP') { throw "Base API configuration still contains bootstrap access" }
    $apiHealthRaw = docker compose --env-file $resolvedEnv -f compose.yaml exec -T api wget -qO- http://127.0.0.1:8080/healthz
    if ($LASTEXITCODE -ne 0) { throw "API health check failed after bootstrap cleanup" }
    $apiHealth = ($apiHealthRaw -join [Environment]::NewLine) | ConvertFrom-Json
    if ($apiHealth.serviceVersion -ne $environment.FORGEFLOW_RELEASE -or $apiHealth.gitCommit -ne $environment.FORGEFLOW_GIT_COMMIT) {
        throw "API release metadata changed after bootstrap cleanup"
    }
}
finally { Pop-Location }

Write-Host "Bootstrap administrator verified; bootstrap secret removed and API recreated without the bootstrap override."
