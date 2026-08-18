param(
    [Parameter(Mandatory = $true)] [string] $Dsn,
    [Parameter(Mandatory = $true)] [string] $Backup,
    [Parameter(Mandatory = $true)] [switch] $ConfirmRestore
)

$ErrorActionPreference = "Stop"
$resolvedBackup = [System.IO.Path]::GetFullPath($Backup)
if (-not (Test-Path -LiteralPath $resolvedBackup -PathType Leaf)) { throw "Backup file does not exist" }
$checksumPath = "$resolvedBackup.sha256"
if (-not (Test-Path -LiteralPath $checksumPath -PathType Leaf)) { throw "Backup checksum file does not exist" }
$expectedChecksum = ((Get-Content -Raw -LiteralPath $checksumPath).Trim() -split '\s+')[0].ToLowerInvariant()
if ($expectedChecksum -notmatch '^[a-f0-9]{64}$') { throw "Backup checksum file is invalid" }
$actualChecksum = (Get-FileHash -Algorithm SHA256 -LiteralPath $resolvedBackup).Hash.ToLowerInvariant()
if ($actualChecksum -ne $expectedChecksum) { throw "Backup SHA-256 verification failed" }
& pg_restore --list $resolvedBackup | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Backup archive cannot be read by pg_restore" }
$databaseName = (& psql --tuples-only --no-align --dbname=$Dsn --command="SELECT current_database()")
if ($LASTEXITCODE -ne 0) { throw "Could not identify restore database" }
$databaseName = $databaseName.Trim()
if ($databaseName -notmatch "(?i)(test|staging)") {
    throw "Restore is restricted to databases whose name contains test or staging"
}

& pg_restore --clean --if-exists --no-owner --no-privileges --exit-on-error --dbname=$Dsn $resolvedBackup
if ($LASTEXITCODE -ne 0) { throw "pg_restore failed" }
& psql --dbname=$Dsn --set=ON_ERROR_STOP=1 --command="SELECT count(*) FROM schema_migrations" | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Restored database failed schema verification" }
Write-Output "Restored $resolvedBackup into $databaseName"
