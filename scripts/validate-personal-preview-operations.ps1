[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot

function Assert-PersonalPreviewOperations {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

$assets = [ordered]@{
    tool = 'scripts/personal_preview_operations.py'
    tests = 'scripts/test_personal_preview_operations.py'
    service = 'deploy/personal-preview/systemd/forgeflow-preview-observe.service'
    timer = 'deploy/personal-preview/systemd/forgeflow-preview-observe.timer'
    runbook = 'docs/personal-preview-operations.md'
    plan = 'FORGEFLOW_FORMAL_LAUNCH_PLAN.md'
    workflow = '.github/workflows/deployment.yml'
}
foreach ($entry in $assets.GetEnumerator()) {
    Assert-PersonalPreviewOperations (Test-Path -LiteralPath (Join-Path $workspace $entry.Value) -PathType Leaf) "Personal preview operations asset is missing: $($entry.Value)"
}

$tool = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.tool)
foreach ($contract in @(
    'forgeflow.personal-preview-observation/v1',
    'forgeflow.personal-preview-rollback-plan/v1',
    'containsRawLogs": False',
    'executedMutation": False',
    'externalModelCallsExpected": 0',
    'providerCostExpectedUSD": 0',
    'FORGEFLOW_PLANNER_MODE',
    'FORGEFLOW_DOCKER_ENABLED',
    'FORGEFLOW_HTTP_COOKIE_SECURE',
    'source-version',
    'personal preview env must not contain model credentials',
    'pg_database_size(current_database())',
    'worker-version',
    'forgeflow-preview-*.dump',
    'downMigration": False',
    'volumesRemoved": False',
    'org.opencontainers.image.revision',
    'rendered-image',
    'personal preview env must be a private regular file',
    'current and target env files must be private regular files, not symlinks',
    'os.replace',
    '0o600'
)) {
    Assert-PersonalPreviewOperations ($tool.Contains($contract)) "Personal preview operations tool is missing contract: $contract"
}
foreach ($forbidden in @('shell=True', 'docker compose down', 'down -v', 'migrate down')) {
    Assert-PersonalPreviewOperations (-not $tool.Contains($forbidden)) "Personal preview operations tool contains forbidden mutation: $forbidden"
}

$tests = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.tests)
foreach ($contract in @('test_observation_contains_only_sanitized_aggregates', 'test_model_key_or_real_planner_fails_boundary', 'test_observation_rejects_model_key_in_private_env', 'test_observation_rejects_non_private_env_file', 'test_observation_rejects_symlinked_env_file', 'test_rollback_plan_verifies_old_image_labels_without_execution', 'test_rollback_rejects_same_version_or_mount_change', 'test_rollback_rejects_symlinked_env_file', 'test_private_report_is_atomic_and_does_not_follow_directory_symlink')) {
    Assert-PersonalPreviewOperations ($tests.Contains($contract)) "Personal preview operations tests are missing case: $contract"
}

$service = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.service)
foreach ($contract in @('Type=oneshot', 'User=forgeflow', 'personal_preview_operations.py observe', 'UMask=0077', 'NoNewPrivileges=true', 'ProtectSystem=strict', 'ReadWritePaths=/srv/forgeflow/operations/observations')) {
    Assert-PersonalPreviewOperations ($service.Contains($contract)) "Personal preview observation service is missing contract: $contract"
}
$timer = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.timer)
foreach ($contract in @('OnCalendar=*:0/15', 'Persistent=true', 'RandomizedDelaySec=2m', 'forgeflow-preview-observe.service')) {
    Assert-PersonalPreviewOperations ($timer.Contains($contract)) "Personal preview observation timer is missing contract: $contract"
}

$runbook = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.runbook)
foreach ($contract in @('PERSONAL-005 实施资产已就绪', 'PERSONAL-005 仍未完成', '尚未执行', '不保存或打印原始日志', '不能读取第三方账单', '至少观察 24 小时', 'rollback-plan', '不得执行 `docker compose down`', '不证明服务器定时器或回滚演练已经发生')) {
    Assert-PersonalPreviewOperations ($runbook.Contains($contract)) "Personal preview operations runbook is missing evidence boundary: $contract"
}

$plan = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.plan)
Assert-PersonalPreviewOperations ($plan.Contains('| PERSONAL-005 | 仓库防护回归就绪，待服务器实测 |')) 'Formal launch plan must keep PERSONAL-005 pending real execution'

$workflow = Get-Content -Raw -LiteralPath (Join-Path $workspace $assets.workflow)
foreach ($contract in @('validate-personal-preview-operations.ps1', 'test_personal_preview_operations.py')) {
    Assert-PersonalPreviewOperations ($workflow.Contains($contract)) "Deployment workflow is missing personal preview operations check: $contract"
}

Write-Host 'ForgeFlow personal preview observability and rollback contract passed.'
