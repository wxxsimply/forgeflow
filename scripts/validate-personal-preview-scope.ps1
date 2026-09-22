[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot

function Assert-PersonalPreviewScope {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

$scopePath = Join-Path $workspace 'docs/personal-preview-scope.md'
$composePath = Join-Path $workspace 'deploy/personal-preview/compose.yaml'
$workflowPath = Join-Path $workspace '.github/workflows/deployment.yml'

Assert-PersonalPreviewScope (Test-Path -LiteralPath $scopePath -PathType Leaf) 'Personal preview scope document is missing'
Assert-PersonalPreviewScope (Test-Path -LiteralPath $composePath -PathType Leaf) 'Personal preview Compose file is missing'

$scope = Get-Content -Raw -LiteralPath $scopePath
foreach ($contract in @(
    'PERSONAL-001 已完成',
    '同时最多 5 个活跃测试账号',
    '运行时外部模型调用上限：0 次',
    '本轮总计 0 USD',
    'PERSONAL-008 完成前'
)) {
    Assert-PersonalPreviewScope ($scope.Contains($contract)) "Personal preview scope is missing contract: $contract"
}

$compose = Get-Content -Raw -LiteralPath $composePath
foreach ($contract in @(
    'FORGEFLOW_WORKFLOW_MODE: planning',
    'FORGEFLOW_PLANNER_MODE: mock',
    'FORGEFLOW_DOCKER_ENABLED: "false"',
    'FORGEFLOW_REPOSITORY_ROOTS: /repositories'
)) {
    Assert-PersonalPreviewScope ($compose.Contains($contract)) "Personal preview Compose is missing boundary: $contract"
}
foreach ($forbidden in @('OPENAI_API_KEY', 'DEEPSEEK_API_KEY')) {
    Assert-PersonalPreviewScope (-not $compose.Contains($forbidden)) "Personal preview Compose must not declare $forbidden"
}

$apiMatch = [regex]::Match($compose, '(?ms)^  api:\r?\n(?<body>.*?)(?=^  worker:)')
Assert-PersonalPreviewScope ($apiMatch.Success) 'Personal preview API service block is missing'
Assert-PersonalPreviewScope ($apiMatch.Groups['body'].Value -match 'target: /repositories\s+read_only: true') 'Personal preview API repository mount must stay read-only'

$workflow = Get-Content -Raw -LiteralPath $workflowPath
Assert-PersonalPreviewScope ($workflow.Contains('validate-personal-preview-scope.ps1')) 'Deployment validation must run the personal preview scope contract'

Write-Host 'ForgeFlow personal preview scope contract passed.'
