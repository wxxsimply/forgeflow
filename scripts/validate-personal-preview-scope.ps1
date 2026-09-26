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
    '公开自助注册',
    '只能创建 `operator` 普通账号',
    '没有邮箱验证或自助密码找回',
    '运行时外部模型调用上限：0 次',
    '本轮总计 0 USD',
    'PERSONAL-008 完成前'
)) {
    Assert-PersonalPreviewScope ($scope.Contains($contract)) "Personal preview scope is missing contract: $contract"
}

$server = Get-Content -Raw -LiteralPath (Join-Path $workspace 'internal/httpapi/server.go')
$auth = Get-Content -Raw -LiteralPath (Join-Path $workspace 'internal/auth/service.go')
Assert-PersonalPreviewScope ($server.Contains('POST /api/v1/auth/register')) 'Public registration route is missing'
Assert-PersonalPreviewScope ($server.Contains('RegistrationLimiter')) 'Public registration rate limit is missing'
Assert-PersonalPreviewScope ($auth.Contains('Role: RoleOperator')) 'Public registration must create an ordinary operator account'

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

$workerMatch = [regex]::Match($compose, '(?ms)^  worker:\r?\n(?<body>.*?)(?=^  web:)')
Assert-PersonalPreviewScope ($workerMatch.Success) 'Personal preview Worker service block is missing'
Assert-PersonalPreviewScope ($workerMatch.Groups['body'].Value -match 'target: /repositories\s+read_only: true') 'Personal preview Worker repository mount must stay read-only'

$workflow = Get-Content -Raw -LiteralPath $workflowPath
Assert-PersonalPreviewScope ($workflow.Contains('validate-personal-preview-scope.ps1')) 'Deployment validation must run the personal preview scope contract'

Write-Host 'ForgeFlow personal preview scope contract passed.'
