[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$Plan,

    [switch]$Execute,
    [switch]$ConfirmApprovedDataScope,
    [switch]$ConfirmPaidSmoke
)

$ErrorActionPreference = 'Stop'
$InvariantCulture = [Globalization.CultureInfo]::InvariantCulture
$RepositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$PrivateRoot = [IO.Path]::GetFullPath((Join-Path $RepositoryRoot '.forgeflow'))

function Assert-Smoke {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Read-RequiredString {
    param([object]$Value, [string]$Name)
    $text = ([string]$Value).Trim()
    Assert-Smoke (-not [string]::IsNullOrWhiteSpace($text)) "$Name must not be empty"
    Assert-Smoke ($text -notmatch '[\r\n]') "$Name must be a single line"
    Assert-Smoke (-not $text.Contains('$' + '{')) "$Name contains an unresolved placeholder"
    Assert-Smoke ($text -notmatch '^REPLACE_WITH_') "$Name contains an unresolved example value"
    return $text
}

function Read-Decimal {
    param([object]$Value, [string]$Name)
    $parsed = [decimal]0
    Assert-Smoke ([decimal]::TryParse([string]$Value, [Globalization.NumberStyles]::Number, $InvariantCulture, [ref]$parsed)) "$Name must be an invariant decimal"
    return $parsed
}

function Read-Time {
    param([object]$Value, [string]$Name)
    $parsed = [datetimeoffset]::MinValue
    Assert-Smoke ([datetimeoffset]::TryParse([string]$Value, $InvariantCulture, [Globalization.DateTimeStyles]::RoundtripKind, [ref]$parsed)) "$Name must be an RFC3339 timestamp"
    return $parsed
}

if (-not $Execute -and ($ConfirmApprovedDataScope -or $ConfirmPaidSmoke)) {
    throw 'Confirmation switches may only be used together with -Execute'
}
if ($Execute) {
    Assert-Smoke $ConfirmApprovedDataScope 'Paid smoke is blocked until -ConfirmApprovedDataScope is supplied'
    Assert-Smoke $ConfirmPaidSmoke 'Paid smoke is blocked until -ConfirmPaidSmoke is supplied'
}

$PlanPath = if ([IO.Path]::IsPathRooted($Plan)) {
    [IO.Path]::GetFullPath($Plan)
}
else {
    [IO.Path]::GetFullPath((Join-Path $RepositoryRoot $Plan))
}
Assert-Smoke ($PlanPath.StartsWith($PrivateRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) 'The populated plan must stay under .forgeflow and remain private'
Assert-Smoke (Test-Path -LiteralPath $PlanPath -PathType Leaf) "Candidate smoke plan does not exist: $PlanPath"
$RelativePlan = [IO.Path]::GetRelativePath($RepositoryRoot, $PlanPath).Replace([IO.Path]::DirectorySeparatorChar, '/')
& git -C $RepositoryRoot check-ignore -q -- $RelativePlan
Assert-Smoke ($LASTEXITCODE -eq 0) 'The populated candidate smoke plan is not covered by Git ignore rules'

try {
    $Configuration = Get-Content -Raw -LiteralPath $PlanPath | ConvertFrom-Json
}
catch {
    throw 'Candidate smoke plan is not valid JSON'
}
Assert-Smoke ($Configuration.schemaVersion -eq 'forgeflow.personal-candidate-smoke-plan/v1') 'Unexpected candidate smoke plan schema'

$ExpectedGitCommit = Read-RequiredString $Configuration.candidateGitCommit 'candidateGitCommit'
$CampaignId = Read-RequiredString $Configuration.campaignId 'campaignId'
$CandidatePromptVersion = Read-RequiredString $Configuration.candidatePromptVersion 'candidatePromptVersion'
$FixtureRepository = Read-RequiredString $Configuration.fixture.repository 'fixture.repository'
$ExpectedFixtureCommit = Read-RequiredString $Configuration.fixture.commit 'fixture.commit'
$GraderRepository = Read-RequiredString $Configuration.grader.repository 'grader.repository'
$ExpectedGraderCommit = Read-RequiredString $Configuration.grader.commit 'grader.commit'
$Provider = Read-RequiredString $Configuration.provider.name 'provider.name'
$Model = Read-RequiredString $Configuration.provider.model 'provider.model'
$ReasoningEffort = Read-RequiredString $Configuration.provider.reasoningEffort 'provider.reasoningEffort'
$KeyFile = Read-RequiredString $Configuration.provider.keyFile 'provider.keyFile'
$PricingSource = Read-RequiredString $Configuration.pricing.source 'pricing.source'
$InputPrice = Read-Decimal $Configuration.pricing.inputUsdPerMillion 'pricing.inputUsdPerMillion'
$CachedInputPrice = Read-Decimal $Configuration.pricing.cachedInputUsdPerMillion 'pricing.cachedInputUsdPerMillion'
$OutputPrice = Read-Decimal $Configuration.pricing.outputUsdPerMillion 'pricing.outputUsdPerMillion'
$PricingValidFrom = Read-Time $Configuration.pricing.validFrom 'pricing.validFrom'
$PricingValidUntil = Read-Time $Configuration.pricing.validUntil 'pricing.validUntil'
$MaximumCost = Read-Decimal $Configuration.maxCampaignUsd 'maxCampaignUsd'

Assert-Smoke ($ExpectedGitCommit -cmatch '^[0-9a-f]{40}$') 'candidateGitCommit must be a lowercase full Git SHA'
Assert-Smoke ($ExpectedFixtureCommit -cmatch '^[0-9a-f]{40}$') 'fixture.commit must be a lowercase full Git SHA'
Assert-Smoke ($ExpectedGraderCommit -cmatch '^[0-9a-f]{40}$') 'grader.commit must be a lowercase full Git SHA'
Assert-Smoke ($CampaignId -match '^personal-preview-[0-9]{8}-[0-9]{2}$') 'campaignId must match personal-preview-YYYYMMDD-NN'
Assert-Smoke ($Configuration.baselinePromptVersion -eq 'developer/v1') 'baselinePromptVersion must remain developer/v1'
Assert-Smoke ($CandidatePromptVersion -eq 'developer/v4') 'candidatePromptVersion must remain the reviewed developer/v4 candidate'
Assert-Smoke ($Configuration.caseId -eq 'feature-01') 'The personal smoke must use the fixed feature-01 Fixture'
Assert-Smoke ($Configuration.mode -eq 'planner_developer') 'The personal smoke must use planner_developer mode'
Assert-Smoke ($Provider -eq 'deepseek') 'The current personal smoke runner only permits the reviewed deepseek provider'
Assert-Smoke ($Model -eq 'deepseek-v4-flash') 'The personal smoke model must remain deepseek-v4-flash'
Assert-Smoke ($ReasoningEffort -eq 'low') 'The personal smoke reasoning effort must remain low'
Assert-Smoke ($PricingSource -eq 'https://api-docs.deepseek.com/quick_start/pricing/') 'pricing.source must be the reviewed official DeepSeek pricing page'
Assert-Smoke ($InputPrice -ge 0 -and $CachedInputPrice -ge 0 -and $OutputPrice -ge 0) 'Pricing values must not be negative'
Assert-Smoke ($PricingValidUntil -gt $PricingValidFrom) 'pricing.validUntil must be later than pricing.validFrom'
Assert-Smoke ($MaximumCost -ge [decimal]'0.01' -and $MaximumCost -le [decimal]'0.10') 'maxCampaignUsd must be between 0.01 and 0.10'

$Head = (& git -C $RepositoryRoot rev-parse HEAD | Out-String).Trim().ToLowerInvariant()
Assert-Smoke ($LASTEXITCODE -eq 0 -and $Head -eq $ExpectedGitCommit) 'Current ForgeFlow HEAD does not match candidateGitCommit'
$TrackedChanges = (& git -C $RepositoryRoot status --porcelain --untracked-files=no | Out-String).Trim()
Assert-Smoke ($LASTEXITCODE -eq 0 -and [string]::IsNullOrWhiteSpace($TrackedChanges)) 'Tracked ForgeFlow files must be clean before the smoke preflight or execution'

$Arguments = @{
    ExpectedGitCommit        = $ExpectedGitCommit
    ExpectedFixtureCommit    = $ExpectedFixtureCommit
    ExpectedGraderCommit     = $ExpectedGraderCommit
    CampaignId               = $CampaignId
    InputUSDPerMillion       = $InputPrice
    CachedInputUSDPerMillion = $CachedInputPrice
    OutputUSDPerMillion      = $OutputPrice
    PricingValidFrom         = $PricingValidFrom
    PricingValidUntil        = $PricingValidUntil
    FixtureRepository        = $FixtureRepository
    GraderRepository         = $GraderRepository
    KeyFile                  = $KeyFile
    Model                    = $Model
    CandidatePromptVersion   = $CandidatePromptVersion
    ReasoningEffort          = $ReasoningEffort
    PricingSource            = $PricingSource
    MaxCampaignUSD           = $MaximumCost
    SmokeCaseLimit           = 1
    SmokeOnly                = $true
}

if (-not $Execute) {
    $Arguments.PreflightOnly = $true
    & (Join-Path $PSScriptRoot 'stage-4-developer-prompt-eval.ps1') @Arguments
    Write-Host 'Personal candidate smoke preflight passed. No provider request was sent.'
    Write-Host 'A paid run still requires -Execute -ConfirmApprovedDataScope -ConfirmPaidSmoke.'
    return
}

$Arguments.ConfirmPaidEval = $true
$SmokeDirectory = Join-Path $RepositoryRoot (Join-Path '.forgeflow/evals' "$CampaignId-smoke")
$SummaryPath = Join-Path $SmokeDirectory 'summary.json'
$DecisionPath = Join-Path $SmokeDirectory 'personal-decision.json'
$ReviewerArguments = @(
    '-B',
    (Join-Path $PSScriptRoot 'personal_candidate_smoke.py'),
    '--summary', $SummaryPath,
    '--decision-output', $DecisionPath,
    '--campaign-id', $CampaignId,
    '--candidate-git-commit', $ExpectedGitCommit,
    '--max-campaign-usd', ($MaximumCost.ToString($InvariantCulture))
)

try {
    & (Join-Path $PSScriptRoot 'stage-4-developer-prompt-eval.ps1') @Arguments
}
catch {
    $PreviousNativePreference = $PSNativeCommandUseErrorActionPreference
    try {
        $PSNativeCommandUseErrorActionPreference = $false
        & python @($ReviewerArguments + '--execution-error')
    }
    finally {
        $PSNativeCommandUseErrorActionPreference = $PreviousNativePreference
    }
    throw "Candidate smoke execution failed and is NO-GO. Keep private evidence for diagnosis: $DecisionPath"
}

$PreviousNativePreference = $PSNativeCommandUseErrorActionPreference
try {
    $PSNativeCommandUseErrorActionPreference = $false
    & python @ReviewerArguments
    $ReviewExitCode = $LASTEXITCODE
}
finally {
    $PSNativeCommandUseErrorActionPreference = $PreviousNativePreference
}

if ($ReviewExitCode -eq 2) {
    throw "Candidate smoke produced NO-GO. Keep the private evidence and do not widen the preview. Decision: $DecisionPath"
}
if ($ReviewExitCode -ne 0) {
    throw 'Candidate smoke decision review failed; treat the campaign as NO-GO'
}

Write-Host 'PERSONAL-006 candidate smoke produced GO screening evidence.'
Write-Host "Private decision: $DecisionPath"
Write-Host 'This one-case smoke is not Promotion evidence and does not authorize deployment or broader access.'
