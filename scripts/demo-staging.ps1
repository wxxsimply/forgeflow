param(
    [Parameter(Mandatory = $true)] [uri]$BaseUri,
    [Parameter(Mandatory = $true)] [string]$Email,
    [Parameter(Mandatory = $true)] [Security.SecureString]$Password,
    [string]$RepositoryPath = "/repositories/demo",
    [string]$Task = "Inspect the fixture repository and produce an approval-protected delivery report without external side effects."
)

$ErrorActionPreference = "Stop"
if ($BaseUri.Scheme -ne 'https') { throw "The staging demo requires an HTTPS BaseUri" }
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$credential = New-Object System.Management.Automation.PSCredential('unused', $Password)
$plainPassword = $credential.GetNetworkCredential().Password
$origin = $BaseUri.GetLeftPart([System.UriPartial]::Authority).TrimEnd('/')

function Invoke-ForgeFlow {
    param([string]$Method, [string]$Path, [object]$Body, [hashtable]$Headers = @{})
    $parameters = @{ Method=$Method; Uri="$origin$Path"; WebSession=$session; Headers=$Headers; UseBasicParsing=$true; TimeoutSec=30 }
    if ($null -ne $Body) { $parameters.ContentType='application/json'; $parameters.Body=($Body | ConvertTo-Json -Depth 12) }
    Invoke-WebRequest @parameters
}

try {
    $started = Get-Date
    $health = Invoke-ForgeFlow GET '/healthz' $null
    if ($health.StatusCode -ne 200) { throw "Staging health check failed" }

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
    Write-Host "Created Run $($run.runId); waiting for plan approval..."

    $approval = $null
    for ($attempt=0; $attempt -lt 30 -and -not $approval; $attempt++) {
        $list = (Invoke-ForgeFlow GET '/api/v1/approvals?status=pending' $null).Content | ConvertFrom-Json
        $approval = @($list.items | Where-Object { $_.request.runId -eq $run.runId }) | Select-Object -First 1
        if (-not $approval) { Start-Sleep -Seconds 2 }
    }
    if (-not $approval) { throw "Run did not reach a pending approval" }

    $approvalID = $approval.request.approvalId
    $approvalResponse = Invoke-ForgeFlow GET "/api/v1/approvals/$approvalID" $null
    $decisionHeaders = @{'X-CSRF-Token'=$csrf; 'If-Match'=$approvalResponse.Headers.ETag}
    Invoke-ForgeFlow POST "/api/v1/approvals/$approvalID/decision" @{decision='approve'; comment='Staging demo approval after reviewing bounded plan.'} $decisionHeaders | Out-Null

    $terminal = $null
    for ($attempt=0; $attempt -lt 90; $attempt++) {
        $current = (Invoke-ForgeFlow GET "/api/v1/runs/$($run.runId)" $null).Content | ConvertFrom-Json
        if ($current.status -in @('completed','failed','cancelled')) { $terminal=$current; break }
        Start-Sleep -Seconds 2
    }
    if (-not $terminal) { throw "Run did not reach a terminal state" }
    $report = (Invoke-ForgeFlow GET "/api/v1/runs/$($run.runId)/report" $null).Content | ConvertFrom-Json
    $elapsed = [math]::Round(((Get-Date)-$started).TotalSeconds, 1)
    [ordered]@{runId=$run.runId; status=$terminal.status; reportStatus=$report.status; durationSeconds=$elapsed; originalRepositoryPath=$RepositoryPath} | ConvertTo-Json
    if ($terminal.status -ne 'completed') { throw "Demo Run ended with $($terminal.status)" }
}
finally {
    $plainPassword = $null
    if ($session.Cookies.Count -gt 0) {
        try { Invoke-ForgeFlow POST '/api/v1/auth/logout' @{} @{'X-CSRF-Token'=$csrf} | Out-Null } catch { }
    }
}
