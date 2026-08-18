param(
    [string]$EnvFile = "deploy/staging/staging.env",
    [Parameter(Mandatory = $true)] [switch]$ConfirmNotification
)

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot
$deployDirectory = Join-Path $workspace "deploy/staging"
$compose = Join-Path $deployDirectory "compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))

Push-Location $deployDirectory
try {
    docker compose --env-file $resolvedEnv -f $compose exec -T alertmanager amtool --alertmanager.url=http://127.0.0.1:9093 alert add ForgeFlowNotificationDrill severity=warning drill=true --annotation=summary="ForgeFlow staging notification drill"
    if ($LASTEXITCODE -ne 0) { throw "Could not submit Alertmanager test alert" }
    Write-Host "Test alert submitted. Confirm receipt in the private incident channel, then silence/expire it."
}
finally { Pop-Location }
