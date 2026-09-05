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
    [datetimeoffset]$PricingValidFrom,

    [Parameter(Mandatory = $true)]
    [datetimeoffset]$PricingValidUntil,

    [string]$FixtureRepository = 'D:\fixtures\forgeflow-eval-fixtures',
    [string]$GraderRepository = 'D:\fixtures\forgeflow-eval-grader',
    [string]$KeyFile = 'D:\forgeflow-secrets\deepseek_api_key',
    [string]$EvidenceRoot = '.forgeflow\evals',
    [string]$WorkspaceRoot = '.forgeflow\eval-worktrees',
    [string]$Model = 'deepseek-v4-flash',
    [ValidatePattern('^developer/v[1-9][0-9]*$')]
    [string]$CandidatePromptVersion = 'developer/v2',
    [ValidateSet('low', 'medium', 'high')]
    [string]$ReasoningEffort = 'low',
    [string]$PricingSource = 'https://api-docs.deepseek.com/quick_start/pricing/',
    [ValidateRange(0.01, 1.00)]
    [decimal]$MaxCampaignUSD = 1.00,
    [ValidateRange(1, 1440)]
    [int]$MinimumWindowMinutes = 240,
    [ValidateRange(1, 2)]
    [int]$SmokeCaseLimit = 1,
    [switch]$SmokeOnly,
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
if ($CandidatePromptVersion -eq 'developer/v1') {
    throw 'CandidatePromptVersion must differ from the current developer/v1 baseline.'
}
if ($SmokeOnly -and $Resume) {
    throw 'SmokeOnly does not support Resume. Use a new CampaignId or remove the existing smoke artifacts.'
}
$candidatePromptSlug = $CandidatePromptVersion.Replace('/', '-')
$candidatePromptDirectory = Join-Path $RepositoryRoot (Join-Path 'internal\developer\prompts' $CandidatePromptVersion)
foreach ($promptFile in @('system.txt', 'user.tmpl')) {
    if (-not (Test-Path -LiteralPath (Join-Path $candidatePromptDirectory $promptFile) -PathType Leaf)) {
        throw "Candidate Developer Prompt file does not exist: $CandidatePromptVersion/$promptFile"
    }
}

$null = Get-CleanCommit -Repository $RepositoryRoot -ExpectedCommit $expectedGit -Label 'ForgeFlow'
$null = Get-CleanCommit -Repository $FixtureRepository -ExpectedCommit $expectedFixture -Label 'Fixture'
$null = Get-CleanCommit -Repository $GraderRepository -ExpectedCommit $expectedGrader -Label 'Private Grader'

if (-not (Test-Path -LiteralPath $KeyFile -PathType Leaf)) {
    throw "DeepSeek API key file does not exist: $KeyFile"
}

$now = [datetimeoffset]::UtcNow
$pricingStart = $PricingValidFrom.ToUniversalTime()
$pricingEnd = $PricingValidUntil.ToUniversalTime()
if ($pricingEnd -le $pricingStart) {
    throw 'PricingValidUntil must be later than PricingValidFrom.'
}
if ($now -lt $pricingStart) {
    throw "Pricing window has not started. It begins at $($pricingStart.ToString('o', $InvariantCulture))."
}
$remainingWindow = $pricingEnd - $now
if ($remainingWindow.TotalMinutes -lt $MinimumWindowMinutes) {
    throw "Pricing window has only $([math]::Floor($remainingWindow.TotalMinutes)) minutes remaining; at least $MinimumWindowMinutes are required."
}

$evidenceDirectory = [System.IO.Path]::GetFullPath((Join-Path $RepositoryRoot $EvidenceRoot))
$workspaceDirectory = [System.IO.Path]::GetFullPath((Join-Path $RepositoryRoot $WorkspaceRoot))
$currentEvidence = Join-Path $evidenceDirectory "$CampaignId-developer-v1.json"
$candidateEvidence = Join-Path $evidenceDirectory "$CampaignId-$candidatePromptSlug.json"
$currentReport = Join-Path $evidenceDirectory "$CampaignId-developer-v1-report.json"
$candidateReport = Join-Path $evidenceDirectory "$CampaignId-$candidatePromptSlug-report.json"
$candidateComparisonJSON = Join-Path $evidenceDirectory "$CampaignId-candidate-comparison.json"
$candidateComparisonMarkdown = Join-Path $evidenceDirectory "$CampaignId-candidate-comparison.md"
$smokeDirectory = Join-Path $evidenceDirectory "$CampaignId-smoke"
$smokeSummaryJSON = Join-Path $smokeDirectory 'summary.json'

if ($SmokeOnly) {
    if (Test-Path -LiteralPath $smokeDirectory) {
        throw "Refusing to overwrite an existing smoke campaign: $smokeDirectory. Use a new CampaignId."
    }
}
elseif (-not $Resume) {
    foreach ($path in @($currentEvidence, $candidateEvidence, $currentReport, $candidateReport, $candidateComparisonJSON, $candidateComparisonMarkdown)) {
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
$pricingStartText = $pricingStart.ToString('o', $InvariantCulture)
$pricingDeadline = $pricingEnd.ToString('o', $InvariantCulture)

Write-Host 'Stage 4 Developer Prompt Eval preflight passed.'
Write-Host "ForgeFlow commit: $expectedGit"
Write-Host "Fixture commit: $expectedFixture"
Write-Host "Private Grader commit: $expectedGrader"
Write-Host "Model/reasoning: $Model / $ReasoningEffort"
Write-Host "Candidate Developer Prompt: $CandidatePromptVersion"
Write-Host "Campaign budget: USD $campaignBudget"
Write-Host "Pricing valid from: $pricingStartText"
Write-Host "Pricing valid until: $pricingDeadline"
if ($SmokeOnly) {
    Write-Host "Execution profile: fast smoke ($SmokeCaseLimit cases per prompt, planner_developer only)"
    Write-Host "Smoke observations: $($SmokeCaseLimit * 2)"
    Write-Host "Smoke output: $smokeDirectory"
    Write-Host 'Smoke results are screening evidence only and cannot be used for Promotion.'
}
else {
    Write-Host 'Execution profile: formal Eval (30 cases x 3 modes x 2 prompts)'
    Write-Host "Current Evidence: $currentEvidence"
    Write-Host "Candidate Evidence: $candidateEvidence"
}

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
    $callTimeout = if ($SmokeOnly) { '60s' } else { '5m' }
    $commandTimeout = if ($SmokeOnly) { '60s' } else { '5m' }

    $commonArguments = @(
        'run', './cmd/forgeflow', 'eval', 'execute',
        '--suite', 'software/v1',
        '--fixture-repository', $FixtureRepository,
        '--grader-repository', $GraderRepository,
        '--provider', 'deepseek',
        '--model', $Model,
        '--reasoning-effort', $ReasoningEffort,
        '--max-output-tokens', '16000',
        '--call-timeout', $callTimeout,
        '--command-timeout', $commandTimeout,
        '--context-max-bytes', '131072',
        '--pricing-mode', 'cache_hit_miss',
        '--input-usd-per-million', $inputPrice,
        '--cached-input-usd-per-million', $cachedInputPrice,
        '--output-usd-per-million', $outputPrice,
        '--pricing-source', $PricingSource,
        '--pricing-valid-from', $pricingStartText,
        '--pricing-valid-until', $pricingDeadline,
        '--max-total-cost-usd', $campaignBudget
    )

    Push-Location $RepositoryRoot
    $locationPushed = $true
    if ($SmokeOnly) {
        New-Item -ItemType Directory -Force -Path $smokeDirectory | Out-Null
        $priorCost = [decimal]0
        $smokeRows = @()
        foreach ($promptVersion in @('developer/v1', $CandidatePromptVersion)) {
            $promptSlug = $promptVersion.Replace('/', '-')
            $smokeEvidence = Join-Path $smokeDirectory "$promptSlug-planner-developer.json"
            Write-Host "Running fast smoke for $promptVersion ($SmokeCaseLimit cases)..."
            $smokeJSON = Invoke-ForgeFlow -Arguments ($commonArguments + @(
                '--modes', 'planner_developer',
                '--developer-prompt-version', $promptVersion,
                '--output', $smokeEvidence,
                '--workspace-root', (Join-Path $workspaceDirectory "$CampaignId-smoke-$promptSlug"),
                '--prior-cost-usd', (ConvertTo-InvariantDecimal $priorCost),
                '--limit', $SmokeCaseLimit.ToString($InvariantCulture)
            ))
            $smokeResult = $smokeJSON | ConvertFrom-Json
            if ($null -eq $smokeResult.campaignCostUsd) {
                throw "$promptVersion smoke result did not include campaignCostUsd."
            }
            if ([int]$smokeResult.observations -ne $SmokeCaseLimit) {
                throw "$promptVersion smoke produced $($smokeResult.observations) observations; expected $SmokeCaseLimit."
            }

            $smokeReportJSON = Invoke-ForgeFlow -Arguments @(
                'run', './cmd/forgeflow', 'eval',
                '--suite', 'software/v1',
                '--evidence', $smokeEvidence,
                '--smoke-report',
                '--format', 'json'
            )
            $smokeReport = $smokeReportJSON | ConvertFrom-Json
            if ($smokeReport.schemaVersion -ne 'forgeflow.eval.smoke-report/v1') {
                throw "$promptVersion produced an unexpected smoke report schema."
            }
            $smokeRows += [pscustomobject][ordered]@{
                promptVersion = $promptVersion
                mode = 'planner_developer'
                cases = [int]$smokeReport.total
                passed = [int]$smokeReport.passed
                completionRate = $smokeReport.metrics.completionRate
                hiddenTestPassRate = $smokeReport.metrics.hiddenTestPassRate
                regressionRate = $smokeReport.metrics.regressionRate
                humanInterventionRate = $smokeReport.metrics.humanInterventionRate
                averageCostUsd = $smokeReport.metrics.averageCostUsd
                p95LatencyMs = $smokeReport.metrics.p95LatencyMs
            }
            $priorCost = [decimal]$smokeResult.campaignCostUsd
        }

        $smokeSummary = [ordered]@{
            schemaVersion = 'forgeflow.eval.smoke-campaign/v1'
            promotionEligible = $false
            campaignId = $CampaignId
            currentPromptVersion = 'developer/v1'
            candidatePromptVersion = $CandidatePromptVersion
            casesPerPrompt = $SmokeCaseLimit
            mode = 'planner_developer'
            observations = $SmokeCaseLimit * 2
            campaignCostUsd = $priorCost
            results = $smokeRows
        }
        [System.IO.File]::WriteAllText(
            $smokeSummaryJSON,
            (($smokeSummary | ConvertTo-Json -Depth 8) + "`n"),
            [System.Text.UTF8Encoding]::new($false)
        )

        Write-Host 'Fast smoke completed.'
        Write-Host "Observations: $($SmokeCaseLimit * 2)"
        Write-Host "Shared campaign cost: USD $(ConvertTo-InvariantDecimal $priorCost)"
        Write-Host "Summary: $smokeSummaryJSON"
        Write-Host 'Smoke reports are not Promotion evidence. A formal 180-observation Eval is still required before Promotion.'
        return
    }

    Write-Host 'Running the production current baseline with developer/v1...'
    $currentJSON = Invoke-ForgeFlow -Arguments ($commonArguments + @(
        '--modes', 'single_agent,planner_developer,forgeflow',
        '--developer-prompt-version', 'developer/v1',
        '--output', $currentEvidence,
        '--workspace-root', (Join-Path $workspaceDirectory "$CampaignId-developer-v1"),
        '--prior-cost-usd', '0',
        '--limit', '0'
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
    Write-Host "Running the candidate baseline with $CandidatePromptVersion..."
    $candidateJSON = Invoke-ForgeFlow -Arguments ($commonArguments + @(
        '--modes', 'single_agent,planner_developer,forgeflow',
        '--developer-prompt-version', $CandidatePromptVersion,
        '--output', $candidateEvidence,
        '--workspace-root', (Join-Path $workspaceDirectory "$CampaignId-$candidatePromptSlug"),
        '--prior-cost-usd', $currentCost,
        '--limit', '0'
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

    Invoke-ForgeFlow -Arguments @(
        'run', './cmd/forgeflow', 'eval', 'compare',
        '--current', $currentReport,
        '--candidate', $candidateReport,
        '--format', 'json',
        '--output', $candidateComparisonJSON
    ) | Out-Null
    Invoke-ForgeFlow -Arguments @(
        'run', './cmd/forgeflow', 'eval', 'compare',
        '--current', $currentReport,
        '--candidate', $candidateReport,
        '--format', 'markdown',
        '--output', $candidateComparisonMarkdown
    ) | Out-Null

    Write-Host 'Both Developer Prompt baselines completed.'
    Write-Host "Shared campaign cost: USD $totalCost"
    Write-Host "Current report: $currentReport"
    Write-Host "Candidate report: $candidateReport"
    Write-Host "Candidate comparison JSON: $candidateComparisonJSON"
    Write-Host "Candidate comparison Markdown: $candidateComparisonMarkdown"
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
