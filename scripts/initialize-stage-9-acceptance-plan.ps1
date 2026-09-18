[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateNotNullOrEmpty()][string]$StagingBaseUri,
    [Parameter(Mandatory)][ValidateNotNullOrEmpty()][string]$SignerIdentity,
    [Parameter(Mandatory)][ValidateNotNullOrEmpty()][string]$OidcIssuer,
    [Parameter(Mandatory)][ValidateNotNullOrEmpty()][string]$OnCallRosterRecordId,
    [Parameter(Mandatory)][ValidateNotNullOrEmpty()][string]$SecurityReviewerRecordId,
    [Parameter(Mandatory)][ValidateNotNullOrEmpty()][string]$EvalDataScopeRecordId,
    [Parameter(Mandatory)][ValidateNotNullOrEmpty()][string]$EvalBudgetRecordId,
    [Parameter(Mandatory)][ValidateRange(0.000001, 1000000)][decimal]$MaxEvalCostUsd,
    [Parameter(Mandatory)][ValidateNotNullOrEmpty()][string]$ReleaseApprovalRecordId,
    [string]$FixtureCommit = '6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f',
    [string]$GraderCommit = '5942ec84d403e37385203b4c7851d1b92573548a',
    [string]$ModelProvider = 'DeepSeek',
    [string]$Model = 'deepseek-v4-flash',
    [string]$ReasoningEffort = 'low',
    [string]$Registry = 'ghcr.io/wxxsimply',
    [string]$Output = '.forgeflow/acceptance/1.0.0/plan.json'
)

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot
$templatePath = Join-Path $workspace 'deploy/release/stage-9-acceptance-plan.template.json'
$privateRoot = [IO.Path]::GetFullPath((Join-Path $workspace '.forgeflow/acceptance'))
$outputPath = if ([IO.Path]::IsPathRooted($Output)) { [IO.Path]::GetFullPath($Output) } else { [IO.Path]::GetFullPath((Join-Path $workspace $Output)) }
$temporaryPath = $null

function Assert-Initializer {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Normalize-RequiredValue {
    param([string]$Name, [string]$Value)
    $normalized = $Value.Trim()
    Assert-Initializer (-not [string]::IsNullOrWhiteSpace($normalized)) "$Name must not be empty"
    Assert-Initializer (-not $normalized.Contains('$' + '{')) "$Name must not contain an unresolved placeholder"
    Assert-Initializer ($normalized -notmatch '[\r\n]') "$Name must be a single line"
    return $normalized
}

Assert-Initializer (Test-Path -LiteralPath $templatePath -PathType Leaf) 'Stage 9 acceptance plan template is missing'
Assert-Initializer ($outputPath.StartsWith($privateRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) 'Output must stay under .forgeflow/acceptance'
Assert-Initializer (-not (Test-Path -LiteralPath $outputPath)) 'Output already exists; move or remove the existing private plan explicitly before creating another one'

$relativeOutput = [IO.Path]::GetRelativePath($workspace, $outputPath).Replace([IO.Path]::DirectorySeparatorChar, '/')
& git -C $workspace check-ignore -q -- $relativeOutput
Assert-Initializer ($LASTEXITCODE -eq 0) 'Output must be covered by the repository Git ignore rules'

$head = (& git -C $workspace rev-parse HEAD | Out-String).Trim().ToLowerInvariant()
Assert-Initializer ($LASTEXITCODE -eq 0 -and $head -match '^[0-9a-f]{40}$') 'Unable to resolve the current ForgeFlow commit'
$trackedChanges = (& git -C $workspace status --porcelain --untracked-files=no | Out-String).Trim()
Assert-Initializer ($LASTEXITCODE -eq 0 -and [string]::IsNullOrWhiteSpace($trackedChanges)) 'Tracked ForgeFlow files must be clean before freezing the candidate plan'

$plan = Get-Content -Raw -LiteralPath $templatePath | ConvertFrom-Json
$plan.candidateGitCommit = $head
$plan.freeze.fixtureCommit = (Normalize-RequiredValue 'FixtureCommit' $FixtureCommit)
$plan.freeze.graderCommit = (Normalize-RequiredValue 'GraderCommit' $GraderCommit)
$plan.freeze.modelProvider = (Normalize-RequiredValue 'ModelProvider' $ModelProvider)
$plan.freeze.model = (Normalize-RequiredValue 'Model' $Model)
$plan.freeze.reasoningEffort = (Normalize-RequiredValue 'ReasoningEffort' $ReasoningEffort)
$plan.authorization.evalDataScopeRecordId = (Normalize-RequiredValue 'EvalDataScopeRecordId' $EvalDataScopeRecordId)
$plan.authorization.evalBudgetRecordId = (Normalize-RequiredValue 'EvalBudgetRecordId' $EvalBudgetRecordId)
$plan.authorization.maxEvalCostUsd = $MaxEvalCostUsd.ToString([Globalization.CultureInfo]::InvariantCulture)
$plan.authorization.releaseApprovalRecordId = (Normalize-RequiredValue 'ReleaseApprovalRecordId' $ReleaseApprovalRecordId)
$plan.external.registry = (Normalize-RequiredValue 'Registry' $Registry)
$plan.external.stagingBaseUri = (Normalize-RequiredValue 'StagingBaseUri' $StagingBaseUri)
$plan.external.signerIdentity = (Normalize-RequiredValue 'SignerIdentity' $SignerIdentity)
$plan.external.oidcIssuer = (Normalize-RequiredValue 'OidcIssuer' $OidcIssuer)
$plan.external.onCallRosterRecordId = (Normalize-RequiredValue 'OnCallRosterRecordId' $OnCallRosterRecordId)
$plan.external.securityReviewerRecordId = (Normalize-RequiredValue 'SecurityReviewerRecordId' $SecurityReviewerRecordId)

$outputDirectory = Split-Path -Parent $outputPath
New-Item -ItemType Directory -Force -Path $outputDirectory | Out-Null
$temporaryPath = Join-Path $outputDirectory ('.plan-{0}.tmp.json' -f ([Guid]::NewGuid().ToString('N')))

try {
    (($plan | ConvertTo-Json -Depth 20) + [Environment]::NewLine) | Set-Content -LiteralPath $temporaryPath -Encoding utf8
    & (Join-Path $PSScriptRoot 'stage-9-acceptance-preflight.ps1') -Plan $temporaryPath
    if ($LASTEXITCODE -ne 0) { throw 'Stage 9 populated plan preflight failed' }
    Move-Item -LiteralPath $temporaryPath -Destination $outputPath
    $temporaryPath = $null
}
finally {
    if ($temporaryPath -and (Test-Path -LiteralPath $temporaryPath)) {
        Remove-Item -LiteralPath $temporaryPath -Force
    }
}

Write-Host "Stage 9 private acceptance plan created for candidate $head."
Write-Host "Private output: $outputPath"
Write-Host 'No paid Eval, external request, registry write, deployment, governance mutation, tag, or release was performed.'
