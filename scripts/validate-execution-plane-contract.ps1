[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot

function Assert-ExecutionContract {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Read-ExecutionAsset {
    param([string]$RelativePath)
    $path = Join-Path $workspace $RelativePath
    Assert-ExecutionContract (Test-Path -LiteralPath $path -PathType Leaf) "Missing execution-plane asset: $RelativePath"
    return Get-Content -Raw -LiteralPath $path
}

$template = Read-ExecutionAsset 'deploy/production/execution-plane/execution-plane.template.yaml'
$readme = Read-ExecutionAsset 'deploy/production/execution-plane/README.md'
$networkTests = Read-ExecutionAsset 'deploy/production/execution-plane/network-test-contract.md'
$dockerRunner = Read-ExecutionAsset 'internal/sandbox/docker.go'
$dockerTests = Read-ExecutionAsset 'internal/sandbox/docker_test.go'

foreach ($contract in @(
    'non-deployable Production contract template',
    'forgeflow-control',
    'forgeflow-execution',
    'name: control-api',
    'name: execution-worker',
    'name: sandbox-daemon',
    'serviceAccountName: control-api',
    'serviceAccountName: execution-worker',
    'serviceAccountName: sandbox-daemon',
    'secrets-store.csi.k8s.io',
    'PRIVATE_APPROVED_API_IMAGE_AT_SHA256_DIGEST',
    'PRIVATE_APPROVED_WORKER_IMAGE_AT_SHA256_DIGEST',
    'PRIVATE_APPROVED_SANDBOX_DAEMON_IMAGE_AT_SHA256_DIGEST',
    'DOCKER_TLS_VERIFY',
    'tcp://sandbox-daemon.forgeflow-execution.svc.cluster.local:2376',
    '--tlsverify',
    'execution-default-deny',
    'control-dns-egress',
    'execution-dns-egress',
    'worker-egress-only',
    'sandbox-daemon-ingress-and-egress',
    'api-ingress-from-gateway',
    'api-control-plane-only',
    'approved-egress-gateway',
    'port: 2376',
    'port: 5432',
    'port: 8443',
    'port: 8080',
    'port: 53',
    'hostNetwork: false',
    'hostPID: false',
    'hostIPC: false',
    'PRIVATE_EXECUTION_RWX_STORAGE_CLASS'
)) {
    Assert-ExecutionContract ($template.Contains($contract)) "Execution-plane template is missing contract: $contract"
}

Assert-ExecutionContract ($template -match '(?m)^\s*automountServiceAccountToken:\s*false\s*$') 'Service-account tokens must not be auto-mounted'
Assert-ExecutionContract ($template -match '(?m)^\s*readOnly:\s*true\s*$') 'Secret and CSI mounts must be read-only'
Assert-ExecutionContract ($template -match '(?m)^\s*runAsNonRoot:\s*true\s*$') 'API and Worker must run as non-root'
Assert-ExecutionContract ($template -match '(?m)^\s*readOnlyRootFilesystem:\s*true\s*$') 'API and Worker must use a read-only root filesystem'
Assert-ExecutionContract ($template -match '(?m)^\s*allowPrivilegeEscalation:\s*false\s*$') 'API and Worker must forbid privilege escalation'
Assert-ExecutionContract ($template -match '(?m)^\s*-\s*ALL\s*$') 'API and Worker must drop all Linux capabilities'
Assert-ExecutionContract (($template | Select-String -Pattern '(?m)^\s*privileged:\s*true\s*$' -AllMatches).Matches.Count -eq 1) 'Only the documented Sandbox daemon may be privileged'
Assert-ExecutionContract ($template -notmatch '(?i)(docker\.sock|hostPath\s*:|secretKeyRef\s*:|kind:\s*Secret\b)') 'Execution-plane template must not mount host Docker, host paths, or Kubernetes Secret values'
Assert-ExecutionContract ($template -notmatch '(?im)^[ \t]*value:[ \t]*(?:sk-[A-Za-z0-9]|AKIA|-----BEGIN|[A-Za-z0-9+/]{40,}={0,2})') 'Execution-plane template must not place secret values in environment variables'
Assert-ExecutionContract ($dockerRunner.Contains('"--network", "none"')) 'Sandbox implementation must enforce Docker network isolation'
Assert-ExecutionContract ($dockerRunner.Contains('"--read-only"')) 'Sandbox implementation must enforce a read-only root filesystem'
Assert-ExecutionContract ($dockerRunner.Contains('"--cap-drop", "ALL"')) 'Sandbox implementation must drop all Linux capabilities'
foreach ($contract in @(
    'TestDockerRunnerRejectsWorkspaceSymlinkEscape',
    'TestDockerRunnerRejectsWorkingDirectorySymlinkEscape'
)) {
    Assert-ExecutionContract ($dockerTests.Contains($contract)) "Sandbox path-escape regression is missing: $contract"
}

foreach ($contract in @(
    'P8-002',
    'Critical / Open',
    '不可直接部署',
    'PRIVATE_',
    '@sha256:',
    'NetworkPolicy',
    'SecretProviderClass',
    '独立的工作负载身份锚点',
    '控制面 API',
    '执行 Worker',
    'Sandbox daemon',
    'host Docker socket',
    'ReadWriteMany',
    'CEL/Kyverno',
    '不表示身份、Secret、网络、节点或镜像已经部署'
)) {
    Assert-ExecutionContract ($readme.Contains($contract)) "Execution-plane runbook is missing contract: $contract"
}

foreach ($contract in @(
    'P8-002-P01',
    'P8-002-P05',
    'P8-002-N01',
    'P8-002-N04',
    'P8-002-N07',
    'NetworkMode=none',
    'mTLS',
    'P8-002=Open'
)) {
    Assert-ExecutionContract ($networkTests.Contains($contract)) "Execution-plane network test contract is missing: $contract"
}

Write-Host 'ForgeFlow execution-plane deployment contract passed: separate identities, Secret Manager CSI, default-deny network policy, and sandbox boundary checks are present.'
