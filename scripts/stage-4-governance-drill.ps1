param(
    [Parameter(Mandatory = $true)] [uri]$BaseUri,
    [Parameter(Mandatory = $true)] [string]$Email,
    [Parameter(Mandatory = $true)] [Security.SecureString]$Password,
    [Parameter(Mandatory = $true)] [ValidatePattern('^[0-9a-fA-F]{40}$')] [string]$ExpectedAPIGitCommit,
    [ValidateSet('Inspect', 'ImportEval', 'Promote', 'Rollback')] [string]$Action = 'Inspect',
    [ValidateSet('planner', 'developer', 'reviewer', 'security')] [string]$Agent = 'developer',
    [string]$PromptVersion,
    [string]$EvalRunId,
    [string]$TargetReleaseId,
    [string]$Comment,
    [string]$EvidencePath,
    [uri]$WorkerReadinessUri,
    [string]$ExpectedWorkerGitCommit,
    [ValidateSet(200, 503)] [int]$ExpectedReadinessStatus = 200,
    [switch]$AllowInsecureLocalhost,
    [switch]$ConfirmEvalImport,
    [switch]$ConfirmPromotion,
    [switch]$ConfirmRollback
)

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot
$origin = $BaseUri.GetLeftPart([System.UriPartial]::Authority).TrimEnd('/')
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$csrf = $null
$plainPassword = $null

function Assert-UUID {
    param([string]$Name, [string]$Value)
    $parsed = [guid]::Empty
    if (-not [guid]::TryParse($Value, [ref]$parsed)) { throw "$Name must be a UUID" }
}

function Invoke-ForgeFlow {
    param(
        [string]$Method,
        [string]$Path,
        [object]$Body,
        [string]$RawJson,
        [hashtable]$Headers = @{}
    )
    $parameters = @{
        Method = $Method
        Uri = "$origin$Path"
        WebSession = $session
        Headers = $Headers
        UseBasicParsing = $true
        TimeoutSec = 60
    }
    if ($PSBoundParameters.ContainsKey('RawJson')) {
        $parameters.ContentType = 'application/json'
        $parameters.Body = $RawJson
    }
    elseif ($PSBoundParameters.ContainsKey('Body') -and $null -ne $Body) {
        $parameters.ContentType = 'application/json'
        $parameters.Body = ($Body | ConvertTo-Json -Depth 20 -Compress)
    }
    Invoke-WebRequest @parameters
}

function Get-ReadinessResponse {
    param([uri]$Uri)
    Invoke-WebRequest -Uri $Uri -UseBasicParsing -TimeoutSec 10 -SkipHttpErrorCheck
}

function Get-PromptCatalog {
    ((Invoke-ForgeFlow -Method GET -Path '/api/v1/prompts').Content | ConvertFrom-Json)
}

function Get-ActiveReleaseSummary {
    param([object]$Catalog)
    @($Catalog.releases | Where-Object { $_.active } | ForEach-Object {
        [ordered]@{
            id = $_.id
            agent = $_.agent
            version = $_.version
            promptSha256 = $_.promptSha256
            model = $_.model
            evalRunId = $_.evalRunId
            rollbackOf = $_.rollbackOf
            createdAt = $_.createdAt
        }
    })
}

if ($BaseUri.Scheme -ne 'https') {
    $localHosts = @('127.0.0.1', 'localhost', '::1')
    if (-not $AllowInsecureLocalhost -or $BaseUri.Scheme -ne 'http' -or $BaseUri.Host -notin $localHosts) {
        throw 'BaseUri must use HTTPS. HTTP is allowed only for localhost with -AllowInsecureLocalhost.'
    }
}

$ExpectedAPIGitCommit = $ExpectedAPIGitCommit.ToLowerInvariant()
if ($null -ne $WorkerReadinessUri) {
    if ($ExpectedWorkerGitCommit -notmatch '^[0-9a-fA-F]{40}$') { throw 'WorkerReadinessUri requires a 40-character -ExpectedWorkerGitCommit.' }
    $ExpectedWorkerGitCommit = $ExpectedWorkerGitCommit.ToLowerInvariant()
}
elseif (-not [string]::IsNullOrWhiteSpace($ExpectedWorkerGitCommit)) {
    throw 'ExpectedWorkerGitCommit may only be used with -WorkerReadinessUri.'
}

if (($ConfirmEvalImport -and $Action -ne 'ImportEval') -or
    ($ConfirmPromotion -and $Action -ne 'Promote') -or
    ($ConfirmRollback -and $Action -ne 'Rollback')) {
    throw 'A mutation confirmation switch may only be used with its matching action.'
}

switch ($Action) {
    'ImportEval' {
        if (-not $ConfirmEvalImport) { throw 'ImportEval requires -ConfirmEvalImport.' }
        if ([string]::IsNullOrWhiteSpace($EvidencePath)) { throw 'ImportEval requires -EvidencePath.' }
    }
    'Promote' {
        if (-not $ConfirmPromotion) { throw 'Promote requires -ConfirmPromotion.' }
        Assert-UUID 'EvalRunId' $EvalRunId
        if ([string]::IsNullOrWhiteSpace($PromptVersion)) { throw 'Promote requires -PromptVersion.' }
        if ([string]::IsNullOrWhiteSpace($Comment) -or $Comment.Length -gt 2000) { throw 'Promote requires a comment of 1 to 2000 characters.' }
    }
    'Rollback' {
        if (-not $ConfirmRollback) { throw 'Rollback requires -ConfirmRollback.' }
        Assert-UUID 'TargetReleaseId' $TargetReleaseId
        if ([string]::IsNullOrWhiteSpace($Comment) -or $Comment.Length -gt 2000) { throw 'Rollback requires a comment of 1 to 2000 characters.' }
    }
}

$result = [ordered]@{
    schemaVersion = 'forgeflow.governance-drill/v2'
    action = $Action
    recordedAt = (Get-Date).ToUniversalTime().ToString('o')
    baseUri = $origin
    apiBuild = $null
    operatorId = $null
    workerReadiness = $null
    before = $null
    mutation = $null
    after = $null
}

try {
    $apiHealth = (Invoke-ForgeFlow -Method GET -Path '/healthz').Content | ConvertFrom-Json
    if ($apiHealth.status -ne 'ok') { throw "API health status is $($apiHealth.status), expected ok." }
    if ($apiHealth.gitCommit -ne $ExpectedAPIGitCommit) { throw "API Git commit $($apiHealth.gitCommit) does not match expected $ExpectedAPIGitCommit." }
    $result.apiBuild = [ordered]@{ serviceVersion = $apiHealth.serviceVersion; gitCommit = $apiHealth.gitCommit }

    $credential = New-Object System.Management.Automation.PSCredential('unused', $Password)
    $plainPassword = $credential.GetNetworkCredential().Password
    $loginResponse = Invoke-ForgeFlow -Method POST -Path '/api/v1/auth/login' -Body @{ email = $Email; password = $plainPassword; remember = $false }
    $login = $loginResponse.Content | ConvertFrom-Json
    $csrf = $login.csrfToken
    if ([string]::IsNullOrWhiteSpace($csrf)) { throw 'Login did not return a CSRF token.' }

    $me = (Invoke-ForgeFlow -Method GET -Path '/api/v1/auth/me').Content | ConvertFrom-Json
    if ($me.role -ne 'admin') { throw 'Stage 4 governance actions require an authenticated admin.' }
    $result.operatorId = $me.id
    $mutationHeaders = @{ 'X-CSRF-Token' = $csrf }

    $before = Get-PromptCatalog
    $result.before = [ordered]@{
        releaseCount = @($before.releases).Count
        activeReleases = @(Get-ActiveReleaseSummary $before)
    }

    if ($null -ne $WorkerReadinessUri) {
        if ($WorkerReadinessUri.Scheme -ne 'https' -and $WorkerReadinessUri.Host -notin @('127.0.0.1', 'localhost', '::1')) {
            throw 'WorkerReadinessUri must use HTTPS unless it targets localhost.'
        }
        $readinessResponse = Get-ReadinessResponse $WorkerReadinessUri
        $readiness = $readinessResponse.Content | ConvertFrom-Json
        $status = [int]$readinessResponse.StatusCode
        $result.workerReadiness = [ordered]@{ uri = $WorkerReadinessUri.AbsoluteUri; expectedStatus = $ExpectedReadinessStatus; actualStatus = $status; serviceVersion = $readiness.serviceVersion; gitCommit = $readiness.gitCommit }
        if ($readiness.gitCommit -ne $ExpectedWorkerGitCommit) { throw "Worker Git commit $($readiness.gitCommit) does not match expected $ExpectedWorkerGitCommit." }
        if ($status -ne $ExpectedReadinessStatus) { throw "Worker readiness returned $status; expected $ExpectedReadinessStatus." }
        $expectedBodyStatus = if ($ExpectedReadinessStatus -eq 200) { 'ready' } else { 'not_ready' }
        if ($readiness.status -ne $expectedBodyStatus) { throw "Worker readiness body status is $($readiness.status); expected $expectedBodyStatus." }
    }

    switch ($Action) {
        'ImportEval' {
            $resolvedEvidence = (Resolve-Path -LiteralPath $EvidencePath).Path
            $rawEvidence = [System.IO.File]::ReadAllText($resolvedEvidence)
            $evidence = $rawEvidence | ConvertFrom-Json
            if (@($evidence.runs).Count -ne 3) { throw 'Evidence must contain exactly three mode runs.' }
            $response = Invoke-ForgeFlow -Method POST -Path '/api/v1/evals/runs' -RawJson $rawEvidence -Headers $mutationHeaders
            $evalRun = $response.Content | ConvertFrom-Json
            $result.mutation = [ordered]@{
                evalRunId = $evalRun.id
                dataset = $evalRun.dataset
                datasetVersion = $evalRun.datasetVersion
                status = $evalRun.status
                createdAt = $evalRun.createdAt
            }
            $rawEvidence = $null
            $evidence = $null
        }
        'Promote' {
            $fullVersion = if ($PromptVersion.Contains('/')) { $PromptVersion } else { "$Agent/$PromptVersion" }
            if (-not $fullVersion.StartsWith("$Agent/", [System.StringComparison]::Ordinal)) { throw 'PromptVersion agent does not match -Agent.' }
            $versionSegment = $fullVersion.Substring($Agent.Length + 1)
            if ([string]::IsNullOrWhiteSpace($versionSegment) -or $versionSegment.Contains('/')) { throw 'PromptVersion must contain one version segment.' }
            $embedded = @($before.items | Where-Object { $_.agent -eq $Agent -and $_.version -eq $fullVersion })
            if ($embedded.Count -ne 1) { throw "Prompt $fullVersion is not embedded exactly once in the running API image." }
            $encodedAgent = [uri]::EscapeDataString($Agent)
            $encodedVersion = [uri]::EscapeDataString($versionSegment)
            $response = Invoke-ForgeFlow -Method POST -Path "/api/v1/prompts/$encodedAgent/$encodedVersion/promote" -Body @{ evalRunId = $EvalRunId; comment = $Comment.Trim() } -Headers $mutationHeaders
            $release = $response.Content | ConvertFrom-Json
            $result.mutation = [ordered]@{
                releaseId = $release.id
                agent = $release.agent
                version = $release.version
                promptSha256 = $release.promptSha256
                model = $release.model
                evalRunId = $release.evalRunId
                createdAt = $release.createdAt
            }
        }
        'Rollback' {
            $target = @($before.releases | Where-Object { $_.id -eq $TargetReleaseId -and $_.agent -eq $Agent })
            if ($target.Count -ne 1) { throw "Rollback target $TargetReleaseId was not found exactly once for $Agent." }
            $priorActive = @($before.releases | Where-Object { $_.active -and $_.agent -eq $Agent })
            if ($priorActive.Count -ne 1) { throw "Rollback requires exactly one active $Agent release before the mutation." }
            if ($priorActive[0].id -eq $TargetReleaseId) { throw 'Rollback target is already active.' }
            $encodedAgent = [uri]::EscapeDataString($Agent)
            $response = Invoke-ForgeFlow -Method POST -Path "/api/v1/prompts/$encodedAgent/rollback" -Body @{ releaseId = $TargetReleaseId; comment = $Comment.Trim() } -Headers $mutationHeaders
            $release = $response.Content | ConvertFrom-Json
            if ($release.rollbackOf -ne $priorActive[0].id) { throw 'Rollback response does not reference the previously active release.' }
            $result.mutation = [ordered]@{
                releaseId = $release.id
                agent = $release.agent
                version = $release.version
                promptSha256 = $release.promptSha256
                model = $release.model
                evalRunId = $release.evalRunId
                rollbackOf = $release.rollbackOf
                createdAt = $release.createdAt
            }
        }
    }

    $after = Get-PromptCatalog
    $result.after = [ordered]@{
        releaseCount = @($after.releases).Count
        activeReleases = @(Get-ActiveReleaseSummary $after)
    }

    if ($Action -in @('Promote', 'Rollback')) {
        if ($result.after.releaseCount -ne ($result.before.releaseCount + 1)) { throw 'Mutation did not append exactly one immutable release.' }
        $newActive = @($after.releases | Where-Object { $_.id -eq $result.mutation.releaseId -and $_.active })
        if ($newActive.Count -ne 1) { throw 'The new release is not the unique active release returned by the mutation.' }
        $agentActive = @($after.releases | Where-Object { $_.agent -eq $Agent -and $_.active })
        if ($agentActive.Count -ne 1) { throw "Expected exactly one active $Agent release after the mutation." }
    }

    $resolvedOutput = [System.IO.Path]::GetFullPath((Join-Path $workspace '.forgeflow/governance-drills'))
    New-Item -ItemType Directory -Force -Path $resolvedOutput | Out-Null
    $fileName = '{0}-{1}-{2}.json' -f ([DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')), $Action.ToLowerInvariant(), ([guid]::NewGuid().ToString('N'))
    $outputPath = Join-Path $resolvedOutput $fileName
    $result | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $outputPath -Encoding utf8
    Write-Host "Stage 4 governance $Action completed. Private metadata record: $outputPath"
    if ($null -ne $result.mutation) { $result.mutation | ConvertTo-Json -Depth 10 }
}
finally {
    $plainPassword = $null
    $rawEvidence = $null
    $evidence = $null
    if ($session.Cookies.Count -gt 0 -and -not [string]::IsNullOrWhiteSpace($csrf)) {
        try { Invoke-ForgeFlow -Method POST -Path '/api/v1/auth/logout' -Body @{} -Headers @{ 'X-CSRF-Token' = $csrf } | Out-Null } catch { }
    }
}
