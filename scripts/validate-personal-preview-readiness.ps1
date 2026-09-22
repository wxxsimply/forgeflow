[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot

function Assert-PersonalPreviewReadiness {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

$composePath = Join-Path $workspace 'deploy/personal-preview/compose.yaml'
$bootstrapPath = Join-Path $workspace 'deploy/personal-preview/compose.bootstrap.yaml'
$publicPath = Join-Path $workspace 'deploy/personal-preview/compose.public-ip.yaml'
$caddyPath = Join-Path $workspace 'deploy/personal-preview/Caddyfile.public-ip'
$ignorePath = Join-Path $workspace 'deploy/personal-preview/secrets/.gitignore'
$secretGuidePath = Join-Path $workspace 'deploy/personal-preview/secrets/README.md'
$recordPath = Join-Path $workspace 'docs/personal-preview-readiness.md'
$workflowPath = Join-Path $workspace '.github/workflows/deployment.yml'

foreach ($path in @($composePath, $bootstrapPath, $publicPath, $caddyPath, $ignorePath, $secretGuidePath, $recordPath, $workflowPath)) {
    Assert-PersonalPreviewReadiness (Test-Path -LiteralPath $path -PathType Leaf) "Personal preview readiness asset is missing: $path"
}

$compose = Get-Content -Raw -LiteralPath $composePath
foreach ($contract in @(
    'FORGEFLOW_ENV: development',
    'FORGEFLOW_POSTGRES_DSN_FILE: /run/secrets/postgres_dsn',
    'FORGEFLOW_WORKFLOW_MODE: planning',
    'FORGEFLOW_PLANNER_MODE: mock',
    'FORGEFLOW_DOCKER_ENABLED: "false"',
    'file: ./secrets/postgres_password',
    'file: ./secrets/postgres_dsn'
)) {
    Assert-PersonalPreviewReadiness ($compose.Contains($contract)) "Personal preview Compose is missing readiness contract: $contract"
}
Assert-PersonalPreviewReadiness ($compose -notmatch '(?m)^\s+FORGEFLOW_POSTGRES_DSN:\s') 'Personal preview must not inject the PostgreSQL DSN directly'
foreach ($forbidden in @('OPENAI_API_KEY', 'DEEPSEEK_API_KEY')) {
    Assert-PersonalPreviewReadiness (-not $compose.Contains($forbidden)) "Personal preview Compose must not declare $forbidden"
}

$bootstrap = Get-Content -Raw -LiteralPath $bootstrapPath
Assert-PersonalPreviewReadiness ($bootstrap.Contains('FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD_FILE: /run/secrets/bootstrap_admin_password')) 'Bootstrap password must use a Secret file'
Assert-PersonalPreviewReadiness ($bootstrap.Contains('file: ./secrets/bootstrap_admin_password')) 'Bootstrap Secret file mapping is missing'
Assert-PersonalPreviewReadiness ($bootstrap -notmatch '(?m)^\s+FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD:\s') 'Bootstrap password must not be injected directly'

$public = Get-Content -Raw -LiteralPath $publicPath
foreach ($contract in @(
    'FORGEFLOW_HTTP_COOKIE_SECURE: "true"',
    'FORGEFLOW_HTTP_ALLOWED_ORIGINS: https://',
    '"127.0.0.1:8080:8080"',
    '"443:443"',
    'caddy-public-data:/data',
    'caddy-public-config:/config'
)) {
    Assert-PersonalPreviewReadiness ($public.Contains($contract)) "Personal preview HTTPS overlay is missing contract: $contract"
}

$caddy = Get-Content -Raw -LiteralPath $caddyPath
foreach ($contract in @('default_sni', 'https://', 'profile shortlived', 'X-Frame-Options "DENY"', '-Server')) {
    Assert-PersonalPreviewReadiness ($caddy.Contains($contract)) "Personal preview Caddy config is missing contract: $contract"
}

$ignore = Get-Content -Raw -LiteralPath $ignorePath
foreach ($contract in @('*', '!.gitignore', '!README.md')) {
    Assert-PersonalPreviewReadiness ($ignore -match "(?m)^$([regex]::Escape($contract))$") "Personal preview Secret ignore rule is missing: $contract"
}

$secretGuide = Get-Content -Raw -LiteralPath $secretGuidePath
foreach ($contract in @('chmod 600', '10001:10001', 'rm deploy/personal-preview/secrets/bootstrap_admin_password', '不要在个人预览服务器上创建 DeepSeek/OpenAI Key')) {
    Assert-PersonalPreviewReadiness ($secretGuide.Contains($contract)) "Personal preview Secret guide is missing contract: $contract"
}

$record = Get-Content -Raw -LiteralPath $recordPath
foreach ($contract in @('PERSONAL-002、PERSONAL-003 已完成', 'https://39.102.136.31', 'FORGEFLOW_ENV=development', '本次 PR 重新连接、部署或修改了服务器')) {
    Assert-PersonalPreviewReadiness ($record.Contains($contract)) "Personal preview readiness record is missing evidence boundary: $contract"
}

$workflow = Get-Content -Raw -LiteralPath $workflowPath
Assert-PersonalPreviewReadiness ($workflow.Contains('validate-personal-preview-readiness.ps1')) 'Deployment validation must run the personal preview readiness contract'

Write-Host 'ForgeFlow personal preview HTTPS and Secret readiness contract passed.'
