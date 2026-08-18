$ErrorActionPreference = "Stop"

$workspace = Split-Path -Parent $PSScriptRoot
$buildCache = Join-Path $workspace ".cache\go-build"
$testTemp = Join-Path $workspace ".cache\test-tmp"
$binaryDirectory = Join-Path $workspace "bin"
$previousTemp = $env:TEMP
$previousTmp = $env:TMP

Push-Location $workspace
try {
    New-Item -ItemType Directory -Force -Path $buildCache | Out-Null
    New-Item -ItemType Directory -Force -Path $testTemp | Out-Null
    New-Item -ItemType Directory -Force -Path $binaryDirectory | Out-Null
    $env:GOCACHE = $buildCache
    $env:TEMP = $testTemp
    $env:TMP = $testTemp

    $unformatted = @(gofmt -l ./cmd ./internal ./migrations)
    if ($LASTEXITCODE -ne 0) {
        throw "gofmt failed"
    }
    if ($unformatted.Count -gt 0) {
        throw "The following files need gofmt: $($unformatted -join ', ')"
    }

    go test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test failed" }

    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

    if (Get-Command staticcheck -ErrorAction SilentlyContinue) {
        staticcheck ./...
        if ($LASTEXITCODE -ne 0) { throw "staticcheck failed" }
    }

    go test ./migrations -run TestEmbeddedMigrations
    if ($LASTEXITCODE -ne 0) { throw "migration contract failed" }

    go build -trimpath -o (Join-Path $binaryDirectory "forgeflow.exe") ./cmd/forgeflow
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }

    go build -trimpath -o (Join-Path $binaryDirectory "forgeflow-worker.exe") ./cmd/forgeflow-worker
    if ($LASTEXITCODE -ne 0) { throw "worker build failed" }

    go build -trimpath -o (Join-Path $binaryDirectory "forgeflow-api.exe") ./cmd/forgeflow-api
    if ($LASTEXITCODE -ne 0) { throw "API build failed" }

    Push-Location (Join-Path $workspace "web")
    try {
        if (-not (Test-Path "node_modules")) { throw "web dependencies are missing; run npm ci in web/" }
        npm run check
        if ($LASTEXITCODE -ne 0) { throw "web verification failed" }
    }
    finally {
        Pop-Location
    }

    Write-Host "ForgeFlow verification passed."
}
finally {
    $env:TEMP = $previousTemp
    $env:TMP = $previousTmp
    Pop-Location
}
