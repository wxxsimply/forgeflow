[CmdletBinding()]
param(
    [string]$Manifest
)

$ErrorActionPreference = "Stop"
$workspace = Split-Path -Parent $PSScriptRoot
$templatePath = Join-Path $workspace "deploy/release/release-manifest.template.json"
$bakePath = Join-Path $workspace "docker-bake.hcl"
$expected = @(
    [pscustomobject]@{ Name = "forgeflow-api"; Target = "api"; Dockerfile = "Dockerfile"; DigestVariable = "FORGEFLOW_API_DIGEST" },
    [pscustomobject]@{ Name = "forgeflow-worker"; Target = "worker"; Dockerfile = "Dockerfile"; DigestVariable = "FORGEFLOW_WORKER_DIGEST" },
    [pscustomobject]@{ Name = "forgeflow-web"; Target = "web"; Dockerfile = "web/Dockerfile"; DigestVariable = "FORGEFLOW_WEB_DIGEST" },
    [pscustomobject]@{ Name = "forgeflow-caddy"; Target = "caddy"; Dockerfile = "deploy/staging/caddy/Dockerfile"; DigestVariable = "FORGEFLOW_CADDY_DIGEST" },
    [pscustomobject]@{ Name = "forgeflow-sandbox"; Target = "sandbox"; Dockerfile = "deploy/sandbox/Dockerfile"; DigestVariable = "FORGEFLOW_SANDBOX_DIGEST" }
)

function Assert-Contract {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

Assert-Contract (Test-Path -LiteralPath $bakePath -PathType Leaf) "docker-bake.hcl is missing"
Assert-Contract (Test-Path -LiteralPath $templatePath -PathType Leaf) "release manifest template is missing"
$bake = Get-Content -Raw -LiteralPath $bakePath
$template = Get-Content -Raw -LiteralPath $templatePath | ConvertFrom-Json
$expectedNames = @($expected.Name)
$templateNames = @($template.images | ForEach-Object { $_.name })

Assert-Contract ($template.schemaVersion -eq "forgeflow.release/v2") "unexpected release manifest schema"
Assert-Contract (($templateNames -join ",") -eq ($expectedNames -join ",")) "manifest template must contain the five release images in canonical order"
Assert-Contract ($bake -match 'type=sbom') "build plan must enable an SBOM attestation"
Assert-Contract ($bake -match 'type=provenance,mode=max') "build plan must enable max provenance"

foreach ($image in $expected) {
    $dockerfilePath = Join-Path $workspace $image.Dockerfile
    Assert-Contract (Test-Path -LiteralPath $dockerfilePath -PathType Leaf) "missing Dockerfile for $($image.Name)"
    $dockerfile = Get-Content -Raw -LiteralPath $dockerfilePath
    Assert-Contract ($dockerfile -match 'ARG FORGEFLOW_VERSION=development') "$($image.Name) Dockerfile must declare FORGEFLOW_VERSION"
    Assert-Contract ($dockerfile -match 'ARG FORGEFLOW_GIT_COMMIT=unknown') "$($image.Name) Dockerfile must declare FORGEFLOW_GIT_COMMIT"
    Assert-Contract ($dockerfile -match 'org\.opencontainers\.image\.version') "$($image.Name) Dockerfile must label its version"
    Assert-Contract ($dockerfile -match 'org\.opencontainers\.image\.revision') "$($image.Name) Dockerfile must label its Git revision"
    Assert-Contract ($bake -match ('target\s+"' + [regex]::Escape($image.Target) + '"')) "build target $($image.Target) is missing"
    Assert-Contract ($bake -match ([regex]::Escape($image.Name) + ':sha-\$\{GIT_COMMIT\}')) "$($image.Name) must have a Git SHA tag"

    $entry = @($template.images | Where-Object { $_.name -eq $image.Name })
    Assert-Contract ($entry.Count -eq 1) "manifest template must contain exactly one $($image.Name) entry"
    $digestPlaceholder = '$' + '{' + $image.DigestVariable + '}'
    Assert-Contract ($entry[0].digest -eq $digestPlaceholder) "$($image.Name) digest placeholder is invalid"
    $expectedReference = '$' + '{REGISTRY}/' + $image.Name + '@' + $digestPlaceholder
    Assert-Contract ($entry[0].reference -eq $expectedReference) "$($image.Name) reference must use its digest placeholder"
    foreach ($evidence in @($entry[0].sbom.path, $entry[0].provenance.path, $entry[0].signature.path, $entry[0].vulnerabilityScan.path)) {
        Assert-Contract (-not [string]::IsNullOrWhiteSpace($evidence)) "$($image.Name) is missing an evidence path"
    }
}

if ($Manifest) {
    $manifestPath = [System.IO.Path]::GetFullPath((Join-Path $workspace $Manifest))
    Assert-Contract (Test-Path -LiteralPath $manifestPath -PathType Leaf) "release manifest does not exist: $manifestPath"
    $actual = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
    Assert-Contract ($actual.schemaVersion -eq "forgeflow.release/v2") "release manifest schema must be forgeflow.release/v2"
    Assert-Contract ($actual.release -match '^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$') "release must be a semantic version"
    Assert-Contract ($actual.gitCommit -match '^[0-9a-f]{40}$') "gitCommit must be a lowercase 40-character SHA"
    Assert-Contract ($actual.platform -eq "linux/amd64") "release platform must be linux/amd64"
    $createdAt = [DateTimeOffset]::MinValue
    Assert-Contract ([DateTimeOffset]::TryParse($actual.createdAt, [ref]$createdAt) -and $createdAt.Offset -eq [TimeSpan]::Zero) "createdAt must be an RFC3339 UTC timestamp"
    Assert-Contract (($actual | ConvertTo-Json -Depth 20) -notmatch '\$\{') "release manifest contains unresolved placeholders"
    $actualNames = @($actual.images | ForEach-Object { $_.name })
    Assert-Contract (($actualNames -join ",") -eq ($expectedNames -join ",")) "release manifest must contain exactly the five canonical images"
    $releaseRoot = [System.IO.Path]::GetFullPath((Join-Path $workspace ".forgeflow/release/$($actual.release)"))

    foreach ($entry in $actual.images) {
        Assert-Contract ($entry.digest -match '^sha256:[a-f0-9]{64}$') "$($entry.name) has an invalid digest"
        Assert-Contract ($entry.reference.EndsWith("@$($entry.digest)")) "$($entry.name) reference must end with its immutable digest"
        $repository = $entry.reference.Substring(0, $entry.reference.LastIndexOf('@'))
        Assert-Contract ($entry.tags -contains "$($repository):$($actual.release)") "$($entry.name) is missing its release tag"
        Assert-Contract ($entry.tags -contains "$($repository):sha-$($actual.gitCommit)") "$($entry.name) is missing its Git SHA tag"
        Assert-Contract ($entry.sbom.status -eq "generated") "$($entry.name) SBOM is not generated"
        Assert-Contract ($entry.provenance.status -eq "verified") "$($entry.name) provenance is not verified"
        Assert-Contract ($entry.signature.status -eq "verified") "$($entry.name) signature is not verified"
        Assert-Contract ($entry.vulnerabilityScan.critical -is [int] -or $entry.vulnerabilityScan.critical -is [long]) "$($entry.name) critical count must be an integer"
        Assert-Contract ($entry.vulnerabilityScan.high -is [int] -or $entry.vulnerabilityScan.high -is [long]) "$($entry.name) high count must be an integer"
        Assert-Contract ($entry.vulnerabilityScan.critical -ge 0 -and $entry.vulnerabilityScan.high -ge 0) "$($entry.name) vulnerability counts cannot be negative"
        Assert-Contract ($entry.vulnerabilityScan.status -in @("passed", "risk-accepted")) "$($entry.name) vulnerability status must be passed or risk-accepted"
        if (($entry.vulnerabilityScan.critical + $entry.vulnerabilityScan.high) -gt 0) {
            Assert-Contract ($entry.vulnerabilityScan.status -eq "risk-accepted") "$($entry.name) has blocking findings without risk acceptance"
            $acceptance = @($actual.riskAcceptances | Where-Object { $_.image -eq $entry.name -and $_.record })
            Assert-Contract ($acceptance.Count -gt 0) "$($entry.name) requires a recorded risk acceptance"
            $acceptancePath = [System.IO.Path]::GetFullPath((Join-Path $workspace $acceptance[0].record))
            Assert-Contract ($acceptancePath.StartsWith($releaseRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) "$($entry.name) risk acceptance must stay inside its release evidence directory"
            Assert-Contract (Test-Path -LiteralPath $acceptancePath -PathType Leaf) "missing risk acceptance: $acceptancePath"
        }
        foreach ($evidence in @($entry.sbom.path, $entry.provenance.path, $entry.signature.path, $entry.vulnerabilityScan.path)) {
            $evidencePath = [System.IO.Path]::GetFullPath((Join-Path $workspace $evidence))
            Assert-Contract ($evidencePath.StartsWith($releaseRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) "release evidence must stay inside $releaseRoot"
            Assert-Contract (Test-Path -LiteralPath $evidencePath -PathType Leaf) "missing release evidence: $evidencePath"
        }
    }
}

Write-Host "ForgeFlow release asset contract passed for five immutable images."
