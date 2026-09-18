[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot
$testId = [Guid]::NewGuid().ToString('N')
$relativeOutput = ".forgeflow/acceptance/1.0.0/initializer-ci-test-$testId.json"
$outputPath = Join-Path $workspace $relativeOutput
$outsidePath = Join-Path $workspace ".cache/initializer-must-not-write-$testId.json"
$createdOutput = $false
$initializer = Join-Path $PSScriptRoot 'initialize-stage-9-acceptance-plan.ps1'

function Assert-Test {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

$parameters = @{
    StagingBaseUri = 'https://staging.example.invalid/'
    SignerIdentity = 'ci-signer@example.invalid'
    OidcIssuer = 'https://issuer.example.invalid/'
    OnCallRosterRecordId = 'ci-oncall-record'
    SecurityReviewerRecordId = 'ci-security-review-record'
    EvalDataScopeRecordId = 'ci-eval-data-scope-record'
    EvalBudgetRecordId = 'ci-eval-budget-record'
    MaxEvalCostUsd = [decimal]'1.25'
    ReleaseApprovalRecordId = 'ci-release-approval-record'
    Output = $relativeOutput
}

try {
    Assert-Test (-not (Test-Path -LiteralPath $outputPath)) 'Initializer test output already exists before the test'
    & $initializer @parameters
    $createdOutput = $true

    Assert-Test (Test-Path -LiteralPath $outputPath -PathType Leaf) 'Initializer did not create the private plan'
    $plan = Get-Content -Raw -LiteralPath $outputPath | ConvertFrom-Json
    $head = (& git -C $workspace rev-parse HEAD | Out-String).Trim().ToLowerInvariant()
    Assert-Test ($plan.candidateGitCommit -eq $head) 'Initializer did not bind the current full Git HEAD'
    Assert-Test ($plan.external.stagingBaseUri -eq $parameters.StagingBaseUri) 'Initializer changed the staging origin'
    Assert-Test ($plan.authorization.maxEvalCostUsd -eq '1.25') 'Initializer did not serialize the invariant USD limit'
    Assert-Test ((Get-Content -Raw -LiteralPath $outputPath) -notmatch '[$][{]') 'Initializer left an unresolved placeholder'

    $overwriteRejected = $false
    try { & $initializer @parameters } catch { $overwriteRejected = $_.Exception.Message -match 'already exists' }
    Assert-Test $overwriteRejected 'Initializer did not reject an existing private plan'

    $outsideRejected = $false
    $outsideParameters = @{} + $parameters
    $outsideParameters.Output = $outsidePath
    try { & $initializer @outsideParameters } catch { $outsideRejected = $_.Exception.Message -match 'must stay under' }
    Assert-Test $outsideRejected 'Initializer did not reject an output outside .forgeflow/acceptance'
    Assert-Test (-not (Test-Path -LiteralPath $outsidePath)) 'Initializer wrote outside .forgeflow/acceptance'
}
finally {
    if ($createdOutput -and (Test-Path -LiteralPath $outputPath)) { Remove-Item -LiteralPath $outputPath -Force }
}

Write-Host 'Stage 9 acceptance plan initializer tests passed.'
