[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot

function Assert-Production {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Read-Required {
    param([string]$RelativePath)
    $path = Join-Path $workspace $RelativePath
    Assert-Production (Test-Path -LiteralPath $path -PathType Leaf) "Missing Production readiness asset: $RelativePath"
    return Get-Content -Raw -LiteralPath $path
}

$architecture = Read-Required 'docs/production-architecture.md'
foreach ($contract in @('控制面', '执行面', '数据面', 'API、Worker、数据库、Sandbox Engine 不得共享', 'Managed PostgreSQL', '对象存储', 'Secret Manager/Vault', 'artifact.Store', '阶段 9')) {
    Assert-Production ($architecture.Contains($contract)) "Production architecture is missing contract: $contract"
}
Assert-Production ($architecture -match '不表示授权上线|不代表.*已.*部署') 'Architecture approval must not imply Production deployment approval'

$security = Read-Required 'docs/production-security-readiness.md'
foreach ($id in 1..7) {
    $riskID = 'P8-{0:D3}' -f $id
    Assert-Production ($security.Contains($riskID)) "Production risk register is missing $riskID"
}
foreach ($contract in @('Independent reviewer', 'Critical', 'High', 'MFA', 'append-only', 'Risk Accepted', '默认 No-Go')) {
    Assert-Production ($security.Contains($contract)) "Production security review is missing contract: $contract"
}

$slo = Read-Required 'docs/production-slo-capacity.md'
foreach ($contract in @('99.90%', 'P95 ≤ 500 ms', '错误预算', '0.60', '30%', 'RPO', 'RTO', '待实测', 'Model calls')) {
    Assert-Production ($slo.Contains($contract)) "Production SLO/capacity plan is missing contract: $contract"
}
Assert-Production ($slo -match 'PostgreSQL \| ≤ 15 分钟 \| ≤ 2 小时') 'Production database RPO/RTO targets are missing'

$data = Read-Required 'docs/production-data-governance.md'
foreach ($contract in @('Restricted', 'Confidential', '保留计划', '用户导出流程', '用户删除流程', '模型 Provider', 'Private Grader', '备份恢复重放清单', '自助导出端点', '用户级联删除')) {
    Assert-Production ($data.Contains($contract)) "Production data governance is missing contract: $contract"
}

$operations = Read-Required 'docs/production-operations-governance.md'
foreach ($contract in @('Primary', 'Secondary', 'P0', 'P1', 'High risk', 'Go/No-Go', 'Release Approver', 'Down Migration', '私有记录 ID')) {
    Assert-Production ($operations.Contains($contract)) "Production operations governance is missing contract: $contract"
}

$privacy = Read-Required 'docs/legal/privacy-policy-draft.md'
$terms = Read-Required 'docs/legal/terms-of-service-draft.md'
Assert-Production ($privacy -match '不是已生效法律文本') 'Privacy draft must not claim to be an effective legal policy'
Assert-Production ($terms -match '不是已生效合同') 'Terms draft must not claim to be an effective contract'
Assert-Production ($privacy -match '子处理者.*Region|Region.*子处理者') 'Privacy draft must require Region and subprocessor disclosure'
Assert-Production ($terms -match '人工审查') 'Terms draft must retain human review responsibility'

$goNoGo = Read-Required 'release-reports/v1.0.0-go-no-go-template.md'
$releaseNotes = Read-Required 'release-reports/v1.0.0-release-notes-draft.md'
foreach ($contract in @('Candidate Git SHA', 'Release manifest', 'Open Critical', 'Open High', 'GO', 'NO-GO', 'Release Approver')) {
    Assert-Production ($goNoGo.Contains($contract)) "Go/No-Go template is missing contract: $contract"
}
Assert-Production ($releaseNotes -match '在最终 GO 前不得发布') 'Release notes draft must remain blocked before final GO'
Assert-Production ($releaseNotes -match 'Apache-2.0') 'Release notes must state the project license'

$license = Read-Required 'LICENSE'
$thirdParty = Read-Required 'docs/third-party-dependency-review.md'
$securityPolicy = Read-Required 'SECURITY.md'
Assert-Production ($license -match 'Apache License' -and $license -match 'Version 2\.0') 'Root LICENSE must remain Apache License 2.0'
Assert-Production ($thirdParty -match '第三方|Third.party') 'Third-party dependency review is missing'
Assert-Production ($securityPolicy -match 'report|报告|vulnerabilit') 'Security policy must retain a vulnerability reporting channel'

$threatModel = Read-Required 'docs/threat-model.md'
foreach ($id in 1..7) {
    Assert-Production ($threatModel.Contains(('P8-{0:D3}' -f $id))) "Threat model is missing a stage 8 risk"
}

$diagramRoot = Join-Path $workspace 'diagrams/2026-09-08T172047'
$svgPath = Join-Path $diagramRoot 'diagram.svg'
$pngPath = Join-Path $diagramRoot 'diagram.png'
$jsonPath = Join-Path $diagramRoot 'diagram.json'
foreach ($path in @($svgPath, $pngPath, $jsonPath)) {
    Assert-Production (Test-Path -LiteralPath $path -PathType Leaf) "Production architecture diagram asset is missing: $path"
}
$svg = Get-Content -Raw -LiteralPath $svgPath
Assert-Production ($svg -match '<text') 'Production architecture SVG must keep text editable'
Assert-Production ($svg -notmatch '<(?:radialGradient|filter|pattern|clipPath|mask)') 'Production architecture SVG uses an unsupported visual feature'
$diagram = Get-Content -Raw -LiteralPath $jsonPath | ConvertFrom-Json
Assert-Production (@($diagram.nodes).Count -ge 30) 'Production architecture diagram is unexpectedly incomplete'
$png = [IO.File]::ReadAllBytes($pngPath)
$signature = [byte[]](137, 80, 78, 71, 13, 10, 26, 10)
Assert-Production ($png.Length -gt 1024) 'Production architecture PNG is unexpectedly small'
for ($index = 0; $index -lt $signature.Length; $index++) {
    Assert-Production ($png[$index] -eq $signature[$index]) 'Production architecture output is not a valid PNG'
}

Write-Host 'ForgeFlow stage 8 Production readiness contract passed: architecture, security, SLO/capacity, data policy, operations, release templates, and diagram assets.'
