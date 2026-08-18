param(
    [string]$EnvFile = "deploy/staging/staging.env"
)

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot
$compose = Join-Path $workspace "deploy/staging/compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
if (-not (Test-Path -LiteralPath $resolvedEnv -PathType Leaf)) { throw "Staging env file does not exist: $resolvedEnv" }

Push-Location (Join-Path $workspace "deploy/staging")
try {
    docker compose --env-file $resolvedEnv -f $compose --profile ops run --rm backup
    if ($LASTEXITCODE -ne 0) { throw "Staging backup failed" }
}
finally { Pop-Location }
