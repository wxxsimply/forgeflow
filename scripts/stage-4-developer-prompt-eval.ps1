[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedGitCommit,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedFixtureCommit,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedGraderCommit,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$')]
    [string]$CampaignId,

    [Parameter(Mandatory = $true)]
    [ValidateRange(0.0, 1000000.0)]
    [decimal]$InputUSDPerMillion,

    [Parameter(Mandatory = $true)]
    [ValidateRange(0.0, 1000000.0)]
    [decimal]$CachedInputUSDPerMillion,

    [Parameter(Mandatory = $true)]
    [ValidateRange(0.0, 1000000.0)]
    [decimal]$OutputUSDPerMillion,

    [Parameter(Mandatory = $true)]
    [datetimeoffset]$PricingValidUntil,

    [string]$FixtureRepository = 'D:\fixtures\forgeflow-eval-fixtures',
    [string]$GraderRepository = 'D:\fixtures\forgeflow-eval-grader',
    [string]$KeyFile = 'D:\forgeflow-secrets\deepseek_api_key',
    [string]$EvidenceRoot = '.forgeflow\evals',
    [string]$WorkspaceRoot = '.forgeflow\eval-worktrees',
    [string]$Model = 'deepseek-v4-flash',
    [ValidateSet('low', 'medium', 'high')]
    [string]$ReasoningEffort = 'low',
    [string]$PricingSource = 'https://api-docs.deepseek.com/quick_start/pricing/',
    [ValidateRange(0.01, 1.00)]
    [decimal]$MaxCampaignUSD = 1.00,
    [ValidateRange(1, 1440)]
    [int]$MinimumWindowMinutes = 240,
    [switch]$PreflightOnly,
    [switch]$Resume,
    [switch]$ConfirmPaidEval
)

$ErrorActionPreference = 'Stop'
$InvariantCulture = [System.Globalization.CultureInfo]::InvariantCulture
$RepositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

function Get-CleanCommit {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Repository,
        [Parameter(Mandatory = $true)]
        [string]$ExpectedCommit,
        [Parameter(Mandatory = $true)]
        [string]$Label
    )

    if (-not (Test-Path -LiteralPath $Repository -PathType Container)) {
        throw "$Label repository does not exist: $Repository"
    }

    $actualCommit = (& git -C $Repository rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) {
        throw "Could not read $Label repository commit."
    }
    if ($actualCommit -ne $ExpectedCommit.ToLowerInvariant()) {
        throw "$Label commit mismatch. Expected $ExpectedCommit, found $actualCommit."
    }

    $status = & git -C $Repository status --porcelain
    if ($LASTEXITCODE -ne 0) {
        throw "Could not read $Label repository status."
    }
    if ($status) {
        throw "$Label repository is not clean: $Repository"
    }

    return $actualCommit
}

function ConvertTo-InvariantDecimal {
    param([Parameter(Mandatory = $true)][decimal]$Value)
    return $Value.ToString('0.############################', $InvariantCulture)
}

function Invoke-ForgeFlow {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)

    $output = (& go @Arguments | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) {
        throw "ForgeFlow command failed: go $($Arguments -join ' ')"
    }
    return $output
}

$expectedGit = $ExpectedGitCommit.ToLowerInvariant()
$expectedFixture = $ExpectedFixtureCommit.ToLowerInvariant()
$expectedGrader = $ExpectedGraderCommit.ToLowerInvariant()

$null = Get-CleanCommit -Repository $RepositoryRoot -ExpectedCommit $expectedGit -Label 'ForgeFlow'
$null = Get-CleanCommit -Repository $FixtureRepository -ExpectedCommit $expectedFixture -Label 'Fixture'
$null = Get-CleanCommit -Repository $GraderRepository -ExpectedCommit $expectedGrader -Label 'Private Grader'

if (-not (Test-Path -LiteralPath $KeyFile -PathType Leaf)) {
    throw "DeepSeek API key file does not exist: $KeyFile"
}

$remainingWindow = $PricingValidUntil.ToUniversalTime() - [datetimeoffset]::UtcNow
if ($remainingWindow.TotalMinutes -lt $MinimumWindowMinutes) {
    throw "Pricing window has only $([math]::Floor($remainingWindow.TotalMinutes)) minutes remaining; at least $MinimumWindowMinutes are required."
}

$evidenceDirectory = [System.IO.Path]::GetFullPath((Join-Path $RepositoryRoot $EvidenceRoot))
$workspaceDirectory = [System.IO.Path]::GetFullPath((Join-Path $RepositoryRoot $WorkspaceRoot))
$currentEvidence = Join-Path $evidenceDirectory "$CampaignId-developer-v1.json"
$candidateEvidence = Join-Path $evidenceDirectory "$CampaignId-developer-v2.json"
$currentReport = Join-Path $evidenceDirectory "$CampaignId-developer-v1-report.json"
$candidateReport = Join-Path $evidenceDirectory "$CampaignId-developer-v2-report.json"

if (-not $Resume) {
    foreach ($path in @($currentEvidence, $candidateEvidence, $currentReport, $candidateReport)) {
        if (Test-Path -LiteralPath $path) {
            throw "Refusing to overwrite an existing Eval artifact: $path. Use -Resume only for the same campaign."
        }
    }
}
elseif ((Test-Path -LiteralPath $candidateEvidence) -and -not (Test-Path -LiteralPath $currentEvidence)) {
    throw 'Candidate Evidence exists without current-version Evidence; refusing an incoherent resume.'
}

$inputPrice = ConvertTo-InvariantDecimal $InputUSDPerMillion
$cachedInputPrice = ConvertTo-InvariantDecimal $CachedInputUSDPerMillion
$outputPrice = ConvertTo-InvariantDecimal $OutputUSDPerMillion
$campaignBudget = ConvertTo-InvariantDecimal $MaxCampaignUSD
$pricingDeadline = $PricingValidUntil.ToUniversalTime().ToString('o', $InvariantCulture)

Write-Host 'Stage 4 Developer Prompt Eval preflight passed.'
Write-Host "ForgeFlow commit: $expectedGit"
Write-Host "Fixture commit: $expectedFixture"
Write-Host "Private Grader commit: $expectedGrader"
Write-Host "Model/reasoning: $Model / $ReasoningEffort"
Write-Host "Campaign budget: USD $campaignBudget"
Write-Host "Pricing valid until: $pricingDeadline"
Write-Host "Current Evidence: $currentEvidence"
Write-Host "Candidate Evidence: $candidateEvidence"

if ($PreflightOnly) {
    Write-Host 'Preflight-only mode: no provider request was sent.'
    return
}
if (-not $ConfirmPaidEval) {
    throw 'Paid Eval is disabled. Re-run with -ConfirmPaidEval after reviewing the preflight output.'
}

New-Item -ItemType Directory -Force -Path $evidenceDirectory, $workspaceDirectory | Out-Null
$previousAPIKey = $env:OPENAI_API_KEY
$previousBaseURL = $env:FORGEFLOW_OPENAI_BASE_URL
$apiKey = $null
$locationPushed = $false

try {
    $apiKey = (Get-Content -Raw -LiteralPath $KeyFile).Trim()
    if ([string]::IsNullOrWhiteSpace($apiKey)) {
        throw 'DeepSeek API key file is empty.'
    }
    $env:OPENAI_API_KEY = $apiKey
    $env:FORGEFLOW_OPENAI_BASE_URL = 'https://api.deepseek.com'

    $commonArguments = @(
        'run', './cmd/forgeflow', 'eval', 'execute',
        '--suite', 'software/v1',
        '--fixture-repository', $FixtureRepository,
        '--grader-repository', $GraderRepository,
        '--modes', 'single_agent,planner_developer,forgeflow',
        '--provider', 'deepseek',
        '--model', $Model,
        '--reasoning-effort', $ReasoningEffort,
        '--max-output-tokens', '16000',
        '--call-timeout', '5m',
        '--command-timeout', '5m',
        '--context-max-bytes', '131072',
        '--pricing-mode', 'cache_hit_miss',
        '--input-usd-per-million', $inputPrice,
        '--cached-input-usd-per-million', $cachedInputPrice,
        '--output-usd-per-million', $outputPrice,
        '--pricing-source', $PricingSource,
        '--pricing-valid-until', $pricingDeadline,
        '--max-total-cost-usd', $campaignBudget,
        '--limit', '0'
    )

    Push-Location $RepositoryRoot
    $locationPushed = $true
    Write-Host 'Running the production current baseline with developer/v1...'
    $currentJSON = Invoke-ForgeFlow -Arguments ($commonArguments + @(
        '--developer-prompt-version', 'developer/v1',
        '--output', $currentEvidence,
        '--workspace-root', (Join-Path $workspaceDirectory "$CampaignId-developer-v1"),
        '--prior-cost-usd', '0'
    ))
    $currentResult = $currentJSON | ConvertFrom-Json
    if ($null -eq $currentResult.campaignCostUsd) {
        throw 'Current baseline result did not include campaignCostUsd.'
    }
    if ($currentResult.complete -ne $true) {
        throw 'Current baseline did not complete all three modes and 30 fixtures.'
    }
    $currentCost = ConvertTo-InvariantDecimal ([decimal]$currentResult.campaignCostUsd)

    Invoke-ForgeFlow -Arguments @(
        'run', './cmd/forgeflow', 'eval',
        '--suite', 'software/v1',
        '--evidence', $currentEvidence,
        '--format', 'json',
        '--output', $currentReport
    ) | Out-Null

    Write-Host "Current baseline complete. Shared campaign cost so far: USD $currentCost"
    Write-Host 'Running the candidate baseline with developer/v2...'
    $candidateJSON = Invoke-ForgeFlow -Arguments ($commonArguments + @(
        '--developer-prompt-version', 'developer/v2',
        '--output', $candidateEvidence,
        '--workspace-root', (Join-Path $workspaceDirectory "$CampaignId-developer-v2"),
        '--prior-cost-usd', $currentCost
    ))
    $candidateResult = $candidateJSON | ConvertFrom-Json
    if ($null -eq $candidateResult.campaignCostUsd) {
        throw 'Candidate baseline result did not include campaignCostUsd.'
    }
    if ($candidateResult.complete -ne $true) {
        throw 'Candidate baseline did not complete all three modes and 30 fixtures.'
    }
    $totalCost = ConvertTo-InvariantDecimal ([decimal]$candidateResult.campaignCostUsd)

    Invoke-ForgeFlow -Arguments @(
        'run', './cmd/forgeflow', 'eval',
        '--suite', 'software/v1',
        '--evidence', $candidateEvidence,
        '--format', 'json',
        '--output', $candidateReport
    ) | Out-Null

    Write-Host 'Both Developer Prompt baselines completed.'
    Write-Host "Shared campaign cost: USD $totalCost"
    Write-Host "Current report: $currentReport"
    Write-Host "Candidate report: $candidateReport"
    Write-Host 'Do not publish raw Evidence or Private Grader material. Promotion remains a separate human decision.'
}
finally {
    $apiKey = $null
    $env:OPENAI_API_KEY = $previousAPIKey
    $env:FORGEFLOW_OPENAI_BASE_URL = $previousBaseURL
    if ($locationPushed) {
        Pop-Location
    }
}
