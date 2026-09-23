[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$Workspace = Split-Path -Parent $PSScriptRoot

function Assert-Contract {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

$Required = @(
    'deploy/personal-preview/candidate-smoke-plan.example.json',
    'docs/personal-preview-candidate-smoke.md',
    'scripts/personal-preview-candidate-smoke.ps1',
    'scripts/personal_candidate_smoke.py',
    'scripts/test_personal_candidate_smoke.py',
    'scripts/stage-4-developer-prompt-eval.ps1'
)
foreach ($RelativePath in $Required) {
    Assert-Contract (Test-Path -LiteralPath (Join-Path $Workspace $RelativePath) -PathType Leaf) "Missing personal candidate smoke asset: $RelativePath"
}

$ExamplePath = Join-Path $Workspace 'deploy/personal-preview/candidate-smoke-plan.example.json'
try {
    $Example = Get-Content -Raw -LiteralPath $ExamplePath | ConvertFrom-Json
}
catch {
    throw 'Personal candidate smoke example is not valid JSON'
}
Assert-Contract ($Example.schemaVersion -eq 'forgeflow.personal-candidate-smoke-plan/v1') 'Unexpected personal candidate smoke plan schema'
Assert-Contract ($Example.candidateGitCommit -eq ('0' * 40)) 'Example candidate SHA must remain a non-runnable placeholder'
Assert-Contract ($Example.baselinePromptVersion -eq 'developer/v1') 'Personal candidate smoke baseline must remain developer/v1'
Assert-Contract ($Example.candidatePromptVersion -eq 'developer/v4') 'Personal candidate smoke candidate must remain developer/v4'
Assert-Contract ($Example.caseId -eq 'feature-01') 'Personal candidate smoke must remain pinned to feature-01'
Assert-Contract ($Example.mode -eq 'planner_developer') 'Personal candidate smoke must remain pinned to planner_developer mode'
Assert-Contract ($Example.provider.name -eq 'deepseek') 'Personal candidate smoke example must use the reviewed provider'
Assert-Contract ($Example.provider.model -eq 'deepseek-v4-flash') 'Personal candidate smoke example must use the reviewed model'
Assert-Contract ($Example.provider.reasoningEffort -eq 'low') 'Personal candidate smoke example must use the reviewed reasoning effort'
Assert-Contract ([decimal]$Example.maxCampaignUsd -le [decimal]'0.10') 'Personal candidate smoke hard budget must not exceed USD 0.10'
Assert-Contract ([string]$Example.pricing.inputUsdPerMillion -match '^REPLACE_WITH_') 'Example must not freeze stale provider pricing'

$Runner = Get-Content -Raw -LiteralPath (Join-Path $Workspace 'scripts/personal-preview-candidate-smoke.ps1')
foreach ($Contract in @(
    'ConfirmApprovedDataScope',
    'ConfirmPaidSmoke',
    'Tracked ForgeFlow files must be clean',
    'ExpectedGitCommit',
    'SmokeCaseLimit           = 1',
    'personal_candidate_smoke.py',
    'Promotion evidence'
)) {
    Assert-Contract ($Runner.Contains($Contract)) "Personal candidate smoke runner is missing contract: $Contract"
}
Assert-Contract (-not $Runner.Contains('Invoke-Expression')) 'Personal candidate smoke runner must not evaluate command text'
Assert-Contract (-not $Runner.Contains('OPENAI_API_KEY=')) 'Personal candidate smoke runner must not embed a provider credential'

$Reviewer = Get-Content -Raw -LiteralPath (Join-Path $Workspace 'scripts/personal_candidate_smoke.py')
foreach ($Contract in @(
    'forgeflow.personal-candidate-smoke-decision/v1',
    'developer/v1',
    'developer/v4',
    'feature-01',
    'planner_developer',
    'promotionEligible',
    'NO-GO',
    'ALLOWED_FAILURES',
    'os.link',
    'decision_output_exists',
    'decision_write_failed'
)) {
    Assert-Contract ($Reviewer.Contains($Contract)) "Personal candidate smoke reviewer is missing contract: $Contract"
}

$Tests = Get-Content -Raw -LiteralPath (Join-Path $Workspace 'scripts/test_personal_candidate_smoke.py')
Assert-Contract ($Tests.Contains('test_private_writer_does_not_overwrite_concurrent_target')) 'Candidate smoke tests are missing the concurrent output collision regression'

$Runbook = Get-Content -Raw -LiteralPath (Join-Path $Workspace 'docs/personal-preview-candidate-smoke.md')
foreach ($Contract in @(
    'PERSONAL-006',
    '实施就绪，待付费实测',
    '-ConfirmApprovedDataScope',
    '-ConfirmPaidSmoke',
    'NO-GO',
    '.forgeflow/',
    '不得提交 Git',
    '不是 Promotion',
    '原子且不覆盖'
)) {
    Assert-Contract ($Runbook.Contains($Contract)) "Personal candidate smoke runbook is missing contract: $Contract"
}

$Plan = Get-Content -Raw -LiteralPath (Join-Path $Workspace 'FORGEFLOW_FORMAL_LAUNCH_PLAN.md')
Assert-Contract ($Plan.Contains('PERSONAL-006 | 离线决策写入回归就绪，待付费实测')) 'Formal launch plan must keep PERSONAL-006 pending the authorized paid smoke'

Write-Host 'Personal preview candidate smoke contract validation passed. No provider request was sent.'
