param(
    [Parameter(Mandatory = $true)] [string] $Dsn,
    [string] $OutputDirectory = ".forgeflow\backups",
    [ValidateRange(1, 3650)] [int] $RetentionDays = 14
)

$ErrorActionPreference = "Stop"
$resolvedOutput = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $resolvedOutput | Out-Null
$timestamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
$temporary = Join-Path $resolvedOutput "forgeflow-$timestamp.dump.tmp"
$target = Join-Path $resolvedOutput "forgeflow-$timestamp.dump"

try {
    & pg_dump --format=custom --no-owner --no-privileges --file=$temporary --dbname=$Dsn
    if ($LASTEXITCODE -ne 0) { throw "pg_dump failed" }
    Move-Item -LiteralPath $temporary -Destination $target
    $digest = (Get-FileHash -Algorithm SHA256 -LiteralPath $target).Hash.ToLowerInvariant()
    Set-Content -Encoding ascii -LiteralPath "$target.sha256" -Value "$digest  $([System.IO.Path]::GetFileName($target))"
    $manifest = [ordered]@{ schemaVersion = "forgeflow.backup/v1"; file = [System.IO.Path]::GetFileName($target); sha256 = $digest; createdAt = (Get-Date).ToUniversalTime().ToString("o"); format = "postgres-custom" }
    $manifest | ConvertTo-Json | Set-Content -Encoding utf8 -LiteralPath "$target.json"
    Get-ChildItem -LiteralPath $resolvedOutput -File | Where-Object { $_.Name -match '^forgeflow-\d{8}T\d{6}Z\.dump(\.sha256|\.json)?$' -and $_.LastWriteTimeUtc -lt (Get-Date).ToUniversalTime().AddDays(-$RetentionDays) } | Remove-Item -Force
    Write-Output $target
}
finally {
    if (Test-Path -LiteralPath $temporary) { Remove-Item -LiteralPath $temporary }
}
