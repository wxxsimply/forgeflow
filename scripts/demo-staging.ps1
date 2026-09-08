[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)] [uri]$BaseUri,
    [Parameter(Mandatory = $true)] [string]$Email,
    [Parameter(Mandatory = $true)] [Security.SecureString]$Password,
    [string]$RepositoryPath = "/repositories/demo",
    [string]$Task = "Inspect the fixture repository and produce an approval-protected delivery report without external side effects.",
    [string]$ExpectedRelease,
    [string]$ExpectedGitCommit,
    [Parameter(Mandatory = $true)] [switch]$ConfirmApprovals
)

$ErrorActionPreference = "Stop"
if (-not $ConfirmApprovals) { throw "The demo requires -ConfirmApprovals after the operator reviews its bounded fixture and task" }
if ($BaseUri.Scheme -ne 'https' -or $BaseUri.UserInfo -or $BaseUri.Query -or $BaseUri.AbsolutePath -notin @('', '/')) {
    throw "BaseUri must be an HTTPS origin without credentials, query, or path"
}
if ($RepositoryPath -notmatch '^/repositories(?:/[A-Za-z0-9._-]+)+$' -or $RepositoryPath -match '(?:^|/)\.\.?(/|$)') {
    throw "RepositoryPath must be a bounded child of /repositories"
}
if ($Task.Length -lt 10 -or $Task.Length -gt 1000 -or $Task -match '(?i)(password|api[_ -]?key|secret|cookie|credential|private[_ -]?key)\s*[:=]') {
    throw "Task is empty, too long, or appears to contain a credential"
}
if ($ExpectedRelease -and $ExpectedRelease -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$') { throw "ExpectedRelease is invalid" }
if ($ExpectedGitCommit -and $ExpectedGitCommit -notmatch '^[a-f0-9]{40}$') { throw "ExpectedGitCommit is invalid" }

$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$credential = New-Object System.Management.Automation.PSCredential('unused', $Password)
$plainPassword = $credential.GetNetworkCredential().Password
$origin = $BaseUri.GetLeftPart([System.UriPartial]::Authority).TrimEnd('/')
$csrf = $null

function Invoke-ForgeFlow {
    param([string]$Method, [string]$Path, [object]$Body, [hashtable]$Headers = @{})
    $parameters = @{ Method=$Method; Uri="$origin$Path"; WebSession=$session; Headers=$Headers; UseBasicParsing=$true; TimeoutSec=30 }
    if ($null -ne $Body) { $parameters.ContentType='application/json'; $parameters.Body=($Body | ConvertTo-Json -Depth 12) }
    Invoke-WebRequest @parameters
}

function Assert-Approval {
    param([object]$Request)
    if ($Request.actionType -notin @('plan', 'plan_approval', 'tool', 'judge_security')) {
        throw "Demo stopped at an unapproved action type: $($Request.actionType)"
    }
    if ($Request.actionType -eq 'tool' -and $Request.toolName -ne 'apply_patch') {
        throw "Demo only permits apply_patch tool approvals"
    }
    foreach ($item in @($Request.scope)) {
        if ([string]::IsNullOrWhiteSpace($item) -or $item -match '(^|[\\/])\.\.([\\/]|$)|(^[A-Za-z]:)|^[/\\]|(?i)(\.env|credential|secret|private[_-]?key)') {
            throw "Approval scope is outside the bounded demo policy"
        }
    }
    if ($Request.inputSha256 -and $Request.inputSha256 -notmatch '^[a-f0-9]{64}$') { throw "Approval input digest is invalid" }
}

try {
    $started = Get-Date
    $healthResponse = Invoke-ForgeFlow GET '/healthz' $null
    if ($healthResponse.StatusCode -ne 200) { throw "Staging health check failed" }
    $health = $healthResponse.Content | ConvertFrom-Json
    if ($ExpectedRelease -and $health.serviceVersion -ne $ExpectedRelease) { throw "Staging release does not match ExpectedRelease" }
    if ($ExpectedGitCommit -and $health.gitCommit -ne $ExpectedGitCommit) { throw "Staging Git SHA does not match ExpectedGitCommit" }

    $loginResponse = Invoke-ForgeFlow POST '/api/v1/auth/login' @{email=$Email; password=$plainPassword; remember=$false}
    $login = $loginResponse.Content | ConvertFrom-Json
    $csrf = $login.csrfToken
    if (-not $csrf) { throw "Login did not return a CSRF token" }
    $mutationHeaders = @{'X-CSRF-Token'=$csrf}

    $repositoryResponse = Invoke-ForgeFlow POST '/api/v1/repositories' @{name="staging-demo-$([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())"; localPath=$RepositoryPath; defaultBranch='HEAD'} $mutationHeaders
    $repository = $repositoryResponse.Content | ConvertFrom-Json
    $runHeaders = @{'X-CSRF-Token'=$csrf; 'Idempotency-Key'=[guid]::NewGuid().ToString()}
    $runResponse = Invoke-ForgeFlow POST '/api/v1/runs' @{repositoryId=$repository.id; task=$Task; baseRevision='HEAD'; maxIterations=2} $runHeaders
    $run = $runResponse.Content | ConvertFrom-Json
    Write-Host "Created Run $($run.runId); monitoring bounded approval checkpoints..."

    $approvalEvidence = @()
    $approvedIDs = @{}
    $terminal = $null
    for ($attempt=0; $attempt -lt 120; $attempt++) {
        $current = (Invoke-ForgeFlow GET "/api/v1/runs/$($run.runId)" $null).Content | ConvertFrom-Json
        if ($current.status -in @('completed','failed','cancelled')) { $terminal=$current; break }
        if ($current.status -in @('waiting_for_plan_approval', 'waiting_for_action_approval')) {
            $list = (Invoke-ForgeFlow GET '/api/v1/approvals?status=pending' $null).Content | ConvertFrom-Json
            $pending = @($list.items | Where-Object { $_.request.runId -eq $run.runId -and -not $approvedIDs.ContainsKey($_.request.approvalId) }) | Select-Object -First 1
            if ($pending) {
                $approvalResponse = Invoke-ForgeFlow GET "/api/v1/approvals/$($pending.request.approvalId)" $null
                $approval = $approvalResponse.Content | ConvertFrom-Json
                Assert-Approval $approval.request
                if ($approvalEvidence.Count -ge 4) { throw "Demo exceeded the four-approval safety bound" }
                $approvalEvidence += [ordered]@{
                    approvalId = $approval.request.approvalId
                    actionType = $approval.request.actionType
                    toolName = $approval.request.toolName
                    risk = $approval.request.risk
                    scope = @($approval.request.scope)
                    inputSha256 = $approval.request.inputSha256
                    policyVersion = $approval.request.policyVersion
                }
                $decisionHeaders = @{'X-CSRF-Token'=$csrf; 'If-Match'=$approvalResponse.Headers.ETag}
                Invoke-ForgeFlow POST "/api/v1/approvals/$($approval.request.approvalId)/decision" @{decision='approve'; comment='Bounded staging fixture approval recorded by demo operator.'} $decisionHeaders | Out-Null
                $approvedIDs[$approval.request.approvalId] = $true
                continue
            }
        }
        Start-Sleep -Seconds 2
    }
    if (-not $terminal) { throw "Run did not reach a terminal state within four minutes" }
    $report = (Invoke-ForgeFlow GET "/api/v1/runs/$($run.runId)/report" $null).Content | ConvertFrom-Json
    $elapsed = [math]::Round(((Get-Date)-$started).TotalSeconds, 1)
    $repositoryHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($RepositoryPath))).ToLowerInvariant()
    $evidence = [ordered]@{
        schemaVersion = 'forgeflow.demo-evidence/v1'
        runId = $run.runId
        status = $terminal.status
        reportStatus = $report.status
        release = $health.serviceVersion
        gitCommit = $health.gitCommit
        durationSeconds = $elapsed
        repositoryPathSha256 = $repositoryHash
        approvals = $approvalEvidence
        completedAt = (Get-Date).ToUniversalTime().ToString('o')
    }
    $evidenceDirectory = Join-Path (Split-Path -Parent $PSScriptRoot) '.forgeflow/staging/demo'
    New-Item -ItemType Directory -Force -Path $evidenceDirectory | Out-Null
    $evidencePath = Join-Path $evidenceDirectory "$($run.runId).json"
    $evidence | ConvertTo-Json -Depth 12 | Set-Content -Encoding utf8 -LiteralPath $evidencePath
    $evidence | ConvertTo-Json -Depth 12
    Write-Host "Sanitized demo evidence: $evidencePath"
    if ($terminal.status -ne 'completed') { throw "Demo Run ended with $($terminal.status)" }
}
finally {
    $plainPassword = $null
    if ($csrf -and $session.Cookies.Count -gt 0) {
        try { Invoke-ForgeFlow POST '/api/v1/auth/logout' @{} @{'X-CSRF-Token'=$csrf} | Out-Null } catch { }
    }
}
