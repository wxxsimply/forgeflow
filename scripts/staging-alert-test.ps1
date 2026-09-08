[CmdletBinding()]
param(
    [ValidateSet('all', 'api-down', 'worker-down', 'http-5xx', 'queue-backlog', 'budget-exhaustion', 'tool-policy-denial', 'approval-wait', 'login-failure', 'rate-limit')]
    [string]$Scenario = 'all',
    [string]$EnvFile = "deploy/staging/staging.env",
    [switch]$DryRun,
    [switch]$ConfirmNotification
)

$ErrorActionPreference = "Stop"
if ([bool]$DryRun -eq [bool]$ConfirmNotification) {
    throw "Choose exactly one of -DryRun or -ConfirmNotification"
}

$catalog = [ordered]@{
    'api-down' = [ordered]@{ alert = 'ForgeFlowAPIDown'; severity = 'critical'; summary = 'ForgeFlow API is unavailable'; runbook = 'docs/operations.md#api-or-worker-down' }
    'worker-down' = [ordered]@{ alert = 'ForgeFlowWorkerDown'; severity = 'critical'; summary = 'ForgeFlow worker is unavailable'; runbook = 'docs/operations.md#api-or-worker-down' }
    'http-5xx' = [ordered]@{ alert = 'ForgeFlowHighHTTP5xxRate'; severity = 'warning'; summary = 'More than 5 percent of API requests are failing'; runbook = 'docs/operations.md#high-api-error-rate' }
    'queue-backlog' = [ordered]@{ alert = 'ForgeFlowQueueBacklog'; severity = 'warning'; summary = 'ForgeFlow worker queue is backlogged'; runbook = 'docs/operations.md#queue-backlog' }
    'budget-exhaustion' = [ordered]@{ alert = 'ForgeFlowBudgetExhaustions'; severity = 'warning'; summary = 'Multiple runs exhausted safety budgets'; runbook = 'docs/operations.md#budget-exhaustion-spike' }
    'tool-policy-denial' = [ordered]@{ alert = 'ForgeFlowToolPolicyDenials'; severity = 'warning'; summary = 'Tool policy denials are elevated'; runbook = 'docs/operations.md#tool-policy-denial-spike' }
    'approval-wait' = [ordered]@{ alert = 'ForgeFlowApprovalWaitTooLong'; severity = 'warning'; summary = 'P95 approval wait exceeds 30 minutes'; runbook = 'docs/operations.md#approval-wait' }
    'login-failure' = [ordered]@{ alert = 'ForgeFlowLoginFailures'; severity = 'warning'; summary = 'Login failures or rate limits are elevated'; runbook = 'docs/operations.md#login-failure-spike' }
    'rate-limit' = [ordered]@{ alert = 'ForgeFlowRateLimitSpike'; severity = 'warning'; summary = 'API rate-limit rejections are elevated'; runbook = 'docs/operations.md#rate-limit-spike' }
}

$selected = if ($Scenario -eq 'all') { @($catalog.Keys) } else { @($Scenario) }
foreach ($name in $selected) {
    $definition = $catalog[$name]
    if ($definition.summary -match '(?i)(password|cookie|api.?key|secret|task body|source code)') {
        throw "Synthetic alert $name contains forbidden sensitive content"
    }
    if ($definition.runbook -notmatch '^docs/operations\.md#[a-z0-9-]+$') {
        throw "Synthetic alert $name has an invalid Runbook link"
    }
}

if ($DryRun) {
    $payloads = foreach ($name in $selected) {
        $definition = $catalog[$name]
        [ordered]@{
            alertname = $definition.alert
            severity = $definition.severity
            drill = 'true'
            scenario = $name
            summary = $definition.summary
            runbook = $definition.runbook
        }
    }
    [ordered]@{
        schemaVersion = 'forgeflow.alert-dry-run/v1'
        deliveryAttempted = $false
        alerts = @($payloads)
    } | ConvertTo-Json -Depth 6
    Write-Host "Alert dry-run passed for $($selected.Count) scenario(s); no notification was sent."
    return
}

$workspace = Split-Path -Parent $PSScriptRoot
$deployDirectory = Join-Path $workspace "deploy/staging"
$compose = Join-Path $deployDirectory "compose.yaml"
$resolvedEnv = [System.IO.Path]::GetFullPath((Join-Path $workspace $EnvFile))
if (-not (Test-Path -LiteralPath $resolvedEnv -PathType Leaf)) { throw "Staging env file does not exist: $resolvedEnv" }

Push-Location $deployDirectory
try {
    foreach ($name in $selected) {
        $definition = $catalog[$name]
        $arguments = @(
            'compose', '--env-file', $resolvedEnv, '-f', $compose,
            'exec', '-T', 'alertmanager',
            'amtool', '--alertmanager.url=http://127.0.0.1:9093', 'alert', 'add', $definition.alert,
            "severity=$($definition.severity)", 'drill=true', "scenario=$name",
            "--annotation=summary=$($definition.summary)", "--annotation=runbook=$($definition.runbook)"
        )
        & docker @arguments
        if ($LASTEXITCODE -ne 0) { throw "Could not submit Alertmanager test alert: $name" }
    }
}
finally { Pop-Location }

Write-Host "Submitted $($selected.Count) synthetic alert(s). Manually confirm receipt and resolution in the private incident channel."
