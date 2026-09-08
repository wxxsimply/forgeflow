[CmdletBinding()]
param(
    [string]$Plan,
    [switch]$SkipEngineeringValidators
)

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot
$templatePath = Join-Path $workspace 'deploy/release/stage-9-acceptance-plan.template.json'
$canonicalSteps = @(
    'freeze',
    'candidate-smoke',
    'formal-eval',
    'image-supply-chain',
    'staging-deploy',
    'governance-drill',
    'staging-e2e',
    'operations-security',
    'load-recovery',
    'final-go-no-go',
    'github-release'
)

function Assert-Acceptance {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Read-JSON {
    param([string]$Path)
    Assert-Acceptance (Test-Path -LiteralPath $Path -PathType Leaf) "Acceptance plan does not exist: $Path"
    $raw = Get-Content -Raw -LiteralPath $Path
    try {
        return [pscustomobject]@{ Raw = $raw; Value = ($raw | ConvertFrom-Json) }
    }
    catch {
        throw "Acceptance plan is not valid JSON: $Path"
    }
}

function Assert-PlanShape {
    param([object]$Value, [bool]$AllowPlaceholders)

    Assert-Acceptance ($Value.schemaVersion -eq 'forgeflow.acceptance-plan/v1') 'Unexpected acceptance plan schema'
    Assert-Acceptance ($Value.release -match '^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$') 'Release must be a semantic version'
    Assert-Acceptance ($Value.freeze.migrationVersion -eq 5) 'Migration version must be frozen at 5 for this candidate'
    Assert-Acceptance ($Value.freeze.developerBaselinePrompt -eq 'developer/v1') 'Developer baseline must remain developer/v1'
    Assert-Acceptance ($Value.freeze.developerCandidatePrompt -match '^developer/v[2-9][0-9]*$') 'Developer candidate must be a non-baseline immutable version'
    Assert-Acceptance ($Value.freeze.policyVersion -eq 'eval-policy/v1') 'Unexpected Eval policy version'
    Assert-Acceptance ($Value.freeze.toolVersion -eq 'eval-tools/v1') 'Unexpected Eval tool version'
    Assert-Acceptance ($Value.evidence.privateRoot -eq ".forgeflow/acceptance/$($Value.release)") 'Private evidence root must be release scoped and Git ignored'
    Assert-Acceptance ($Value.evidence.publicSummaryRoot -eq 'release-reports') 'Public summaries must remain under release-reports'

    $steps = @($Value.steps)
    Assert-Acceptance ($steps.Count -eq $canonicalSteps.Count) 'Acceptance plan must contain all canonical steps exactly once'
    for ($index = 0; $index -lt $canonicalSteps.Count; $index++) {
        $step = $steps[$index]
        Assert-Acceptance ($step.order -eq ($index + 1)) "Acceptance step order is invalid at index $index"
        Assert-Acceptance ($step.id -eq $canonicalSteps[$index]) "Acceptance step is invalid at index $index"
        Assert-Acceptance ($step.status -eq 'pending') "Template step $($step.id) must start pending"
        Assert-Acceptance ($step.timeoutMinutes -ge 1 -and $step.timeoutMinutes -le 360) "Acceptance step $($step.id) has an unsafe timeout"
        Assert-Acceptance (-not [string]::IsNullOrWhiteSpace($step.ownerRole)) "Acceptance step $($step.id) is missing an owner role"
        Assert-Acceptance ($step.evidencePath.StartsWith($Value.evidence.privateRoot + '/', [StringComparison]::Ordinal)) "Acceptance step $($step.id) evidence must stay in the private root"
        if ($index -eq 0) {
            Assert-Acceptance (@($step.requires).Count -eq 0) 'Freeze must not depend on another step'
        }
        else {
            Assert-Acceptance (@($step.requires).Count -eq 1 -and $step.requires[0] -eq $canonicalSteps[$index - 1]) "Acceptance step $($step.id) must depend on the previous gate"
        }
    }

    if ($AllowPlaceholders) { return }

    foreach ($commit in @($Value.candidateGitCommit, $Value.freeze.fixtureCommit, $Value.freeze.graderCommit)) {
        Assert-Acceptance ($commit -match '^[0-9a-f]{40}$') 'Frozen commits must be lowercase full Git SHAs'
    }
    foreach ($field in @(
        $Value.freeze.modelProvider,
        $Value.freeze.model,
        $Value.freeze.reasoningEffort,
        $Value.authorization.evalDataScopeRecordId,
        $Value.authorization.evalBudgetRecordId,
        $Value.authorization.releaseApprovalRecordId,
        $Value.external.registry,
        $Value.external.signerIdentity,
        $Value.external.oidcIssuer,
        $Value.external.onCallRosterRecordId,
        $Value.external.securityReviewerRecordId
    )) {
        Assert-Acceptance (-not [string]::IsNullOrWhiteSpace([string]$field)) 'A required frozen field is empty'
    }

    $cost = [decimal]0
    Assert-Acceptance ([decimal]::TryParse([string]$Value.authorization.maxEvalCostUsd, [Globalization.NumberStyles]::Number, [Globalization.CultureInfo]::InvariantCulture, [ref]$cost) -and $cost -gt 0) 'maxEvalCostUsd must be a positive explicit amount'

    $stagingUri = [uri]$Value.external.stagingBaseUri
    Assert-Acceptance ($stagingUri.IsAbsoluteUri -and $stagingUri.Scheme -eq 'https' -and -not $stagingUri.UserInfo -and $stagingUri.PathAndQuery -eq '/') 'stagingBaseUri must be an HTTPS origin without credentials, path, or query'
}

$runbookPath = Join-Path $workspace 'docs/stage-9-acceptance-window-runbook.md'
$summaryTemplatePath = Join-Path $workspace 'release-reports/stage-9-acceptance-summary-template.md'
$auditPath = Join-Path $workspace 'docs/stage-9-acceptance-preflight-audit.md'
foreach ($requiredPath in @($runbookPath, $summaryTemplatePath, $auditPath)) {
    Assert-Acceptance (Test-Path -LiteralPath $requiredPath -PathType Leaf) "Missing stage 9 acceptance asset: $requiredPath"
}
$runbook = Get-Content -Raw -LiteralPath $runbookPath
$summaryTemplate = Get-Content -Raw -LiteralPath $summaryTemplatePath
foreach ($contract in @('candidate-smoke', 'formal-eval', 'image-supply-chain', 'governance-drill', 'load-recovery', 'final-go-no-go', 'P8-001', 'NO-GO')) {
    Assert-Acceptance ($runbook.Contains($contract)) "Stage 9 runbook is missing contract: $contract"
}
foreach ($contract in @('Candidate smoke', 'Formal Eval', 'Private Grader', 'Independent Security Reviewer', 'GO/NO-GO', '私有 Evidence SHA-256')) {
    Assert-Acceptance ($summaryTemplate.Contains($contract)) "Stage 9 public summary template is missing contract: $contract"
}
Assert-Acceptance ($summaryTemplate -match '模板本身不代表通过') 'Stage 9 public summary template must not claim approval'

if ($Plan -and $SkipEngineeringValidators) {
    throw 'A populated acceptance plan cannot skip stage 5-8 engineering validators.'
}
if (-not $SkipEngineeringValidators) {
    foreach ($validator in @(
        'validate-release-assets.ps1',
        'validate-staging-assets.ps1',
        'validate-operations-assets.ps1',
        'validate-production-readiness.ps1'
    )) {
        & (Join-Path $PSScriptRoot $validator)
    }
}

$template = Read-JSON -Path $templatePath
Assert-Acceptance ($template.Raw.Contains('${GIT_COMMIT}')) 'Acceptance template must keep the Git commit placeholder'
Assert-Acceptance ($template.Raw.Contains('${PRIVATE_EVAL_DATA_SCOPE_RECORD_ID}')) 'Acceptance template must keep private authorization placeholders'
Assert-PlanShape -Value $template.Value -AllowPlaceholders $true

if ([string]::IsNullOrWhiteSpace($Plan)) {
    $scope = if ($SkipEngineeringValidators) { 'stage 9 contract only; earlier validators are separate CI steps' } else { 'stage 5-9 engineering contracts' }
    Write-Host "Stage 9 static acceptance preflight passed ($scope). No paid Eval, registry write, deployment, governance mutation, or external request was performed."
    exit 0
}

$privatePlanRoot = [IO.Path]::GetFullPath((Join-Path $workspace '.forgeflow/acceptance'))
$planPath = if ([IO.Path]::IsPathRooted($Plan)) { [IO.Path]::GetFullPath($Plan) } else { [IO.Path]::GetFullPath((Join-Path $workspace $Plan)) }
Assert-Acceptance ($planPath.StartsWith($privatePlanRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) 'The populated acceptance plan must stay under .forgeflow/acceptance and must not be committed'
$planDocument = Read-JSON -Path $planPath
Assert-Acceptance (-not $planDocument.Raw.Contains('${')) 'The populated acceptance plan contains unresolved placeholders'
Assert-PlanShape -Value $planDocument.Value -AllowPlaceholders $false

$head = (& git -C $workspace rev-parse HEAD | Out-String).Trim().ToLowerInvariant()
Assert-Acceptance ($LASTEXITCODE -eq 0 -and $head -match '^[0-9a-f]{40}$') 'Unable to resolve the current ForgeFlow commit'
Assert-Acceptance ($head -eq $planDocument.Value.candidateGitCommit) 'Current ForgeFlow HEAD does not match the frozen candidate commit'
$trackedChanges = (& git -C $workspace status --porcelain --untracked-files=no | Out-String).Trim()
Assert-Acceptance ($LASTEXITCODE -eq 0 -and [string]::IsNullOrWhiteSpace($trackedChanges)) 'Tracked ForgeFlow files changed after the candidate was frozen'

Write-Host "Stage 9 populated acceptance plan preflight passed for release $($planDocument.Value.release) and commit $head."
Write-Host 'This command is read-only. Each paid, mutating, deployment, registry, promotion, tag, and release step still requires its dedicated human confirmation.'
