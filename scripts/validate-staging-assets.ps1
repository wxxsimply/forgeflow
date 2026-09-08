[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot

function Assert-StagingAsset {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

$scriptNames = @(
    'staging-preflight.ps1',
    'staging-release.ps1',
    'staging-acceptance.ps1',
    'staging-bootstrap-cleanup.ps1'
)
foreach ($name in $scriptNames) {
    $path = Join-Path $PSScriptRoot $name
    Assert-StagingAsset (Test-Path -LiteralPath $path -PathType Leaf) "Missing staging script: $name"
    $tokens = $null
    $parseErrors = $null
    [System.Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$parseErrors) | Out-Null
    Assert-StagingAsset ($parseErrors.Count -eq 0) "$name contains PowerShell parser errors"
}

$compose = Get-Content -Raw -LiteralPath (Join-Path $workspace 'deploy/staging/compose.yaml')
Assert-StagingAsset ($compose -notmatch '(?m)^\s+build\s*:') 'Staging Compose must never build on the server'
Assert-StagingAsset ($compose -notmatch 'docker\.sock') 'Staging Compose must not mount the host Docker socket'
foreach ($variable in @('FORGEFLOW_API_IMAGE', 'FORGEFLOW_WORKER_IMAGE', 'FORGEFLOW_WEB_IMAGE', 'FORGEFLOW_CADDY_IMAGE', 'POSTGRES_IMAGE', 'OTEL_COLLECTOR_IMAGE', 'PROMETHEUS_IMAGE', 'ALERTMANAGER_IMAGE')) {
    Assert-StagingAsset ($compose.Contains("${$variable:?$variable is required}")) "Compose does not require $variable"
}
Assert-StagingAsset ($compose -match 'FORGEFLOW_GOVERNANCE_ENFORCE_ACTIVE_RELEASES') 'Worker governance readiness gate is missing'

$openAICompose = Get-Content -Raw -LiteralPath (Join-Path $workspace 'deploy/staging/compose.openai.yaml')
Assert-StagingAsset ($openAICompose.Contains('${DOCKER_DIND_IMAGE:?DOCKER_DIND_IMAGE is required}')) 'OpenAI overlay must require a digest-pinned DIND image'
Assert-StagingAsset ($openAICompose -match 'OPENAI_API_KEY_FILE') 'OpenAI key must be injected through a Worker-only secret file'
Assert-StagingAsset ($openAICompose -notmatch 'docker\.sock') 'OpenAI overlay must not mount the host Docker socket'

$environmentTemplate = Get-Content -Raw -LiteralPath (Join-Path $workspace 'deploy/staging/staging.env.example')
foreach ($variable in @('FORGEFLOW_API_IMAGE', 'FORGEFLOW_WORKER_IMAGE', 'FORGEFLOW_WEB_IMAGE', 'FORGEFLOW_CADDY_IMAGE', 'FORGEFLOW_SANDBOX_IMAGE', 'POSTGRES_IMAGE', 'OTEL_COLLECTOR_IMAGE', 'PROMETHEUS_IMAGE', 'ALERTMANAGER_IMAGE', 'DOCKER_DIND_IMAGE')) {
    Assert-StagingAsset ($environmentTemplate -match "(?m)^$variable=[^\r\n]+@sha256:<digest>$") "$variable template must require an immutable digest"
}
Assert-StagingAsset ($environmentTemplate -match '(?m)^FORGEFLOW_GIT_COMMIT=<approved-40-character-git-sha>$') 'Staging template must bind the approved Git SHA'
Assert-StagingAsset ($environmentTemplate -match 'FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL') 'Staging template must document the one-time bootstrap email'

$releaseScript = Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot 'staging-release.ps1')
foreach ($contract in @('$Manifest', '-RequireDigests', '--no-build', 'staging-preflight.ps1', 'FORGEFLOW_SANDBOX_IMAGE')) {
    Assert-StagingAsset ($releaseScript.Contains($contract)) "Staging release contract is missing $contract"
}

$acceptanceScript = Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot 'staging-acceptance.ps1')
foreach ($contract in @('release.json', 'test:e2e:staging', 'repositoryUnchanged', 'FORGEFLOW_E2E_PASSWORD')) {
    Assert-StagingAsset ($acceptanceScript.Contains($contract)) "Staging acceptance contract is missing $contract"
}
Assert-StagingAsset ($acceptanceScript -match "workerMetadata\.status -ne 'ready'") 'Acceptance must verify Prompt/model Worker readiness'
Assert-StagingAsset ($acceptanceScript -match 'git worktree list --porcelain') 'Acceptance must verify Fixture worktree metadata is unchanged'
Assert-StagingAsset ($acceptanceScript -match 'staging-preflight\.ps1') 'Acceptance must rerun immutable staging preflight'

$cleanupScript = Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot 'staging-bootstrap-cleanup.ps1')
foreach ($contract in @('ConfirmRemoval', 'bootstrap_admin_password', 'Remove-Item', 'FORGEFLOW_BOOTSTRAP', 'API health check failed after bootstrap cleanup')) {
    Assert-StagingAsset ($cleanupScript.Contains($contract)) "Bootstrap cleanup contract is missing $contract"
}

$webDockerfile = Get-Content -Raw -LiteralPath (Join-Path $workspace 'web/Dockerfile')
Assert-StagingAsset ($webDockerfile -match 'release\.json') 'Web image must expose immutable release metadata'
Assert-StagingAsset ($webDockerfile -match 'FORGEFLOW_GIT_COMMIT') 'Web image must embed the approved Git SHA'
$package = Get-Content -Raw -LiteralPath (Join-Path $workspace 'web/package.json') | ConvertFrom-Json
Assert-StagingAsset ($package.scripts.'test:e2e:staging' -eq 'playwright test --config playwright.staging.config.ts') 'Staging browser E2E command is missing'
$playwrightConfig = Get-Content -Raw -LiteralPath (Join-Path $workspace 'web/playwright.staging.config.ts')
Assert-StagingAsset ($playwrightConfig -match 'FORGEFLOW_STAGING_BASE_URL') 'Staging E2E must use an explicit public base URL'
Assert-StagingAsset ($playwrightConfig -match "scheme !== 'https:'|startsWith\('https://'\)") 'Staging E2E must reject non-HTTPS targets'

Write-Host 'ForgeFlow staging asset contract passed.'
