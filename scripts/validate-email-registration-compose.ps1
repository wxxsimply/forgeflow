[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot
$fixture = @{
    FORGEFLOW_SMTP_HOST = 'smtp.example.invalid'
    FORGEFLOW_SMTP_PORT = '465'
    FORGEFLOW_SMTP_FROM = 'registration@example.invalid'
    FORGEFLOW_SMTP_USER = 'registration@example.invalid'
    FORGEFLOW_SMTP_PASSWORD_PATH = '/non-sensitive/ci-smtp-password'
    FORGEFLOW_REGISTRATION_CODE_KEY_PATH = '/non-sensitive/ci-code-key'
    FORGEFLOW_PUBLIC_IP = '39.102.136.31'
    FORGEFLOW_ACME_EMAIL = 'ci@example.invalid'
}
$previous = @{}

function Assert-EmailCompose {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

Push-Location $workspace
try {
    foreach ($name in $fixture.Keys) {
        $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
        [Environment]::SetEnvironmentVariable($name, $fixture[$name], 'Process')
    }
    foreach ($publicIP in @($false, $true)) {
        $arguments = @('compose', '--env-file', 'deploy/personal-preview/preview.env.example', '-f', 'deploy/personal-preview/compose.yaml')
        if ($publicIP) { $arguments += @('-f', 'deploy/personal-preview/compose.public-ip.yaml') }
        $arguments += @('-f', 'deploy/personal-preview/compose.email.yaml', 'config', '--format', 'json')
        $json = & docker @arguments
        if ($LASTEXITCODE -ne 0) { throw 'Email Compose configuration failed' }
        $configuration = ($json -join "`n") | ConvertFrom-Json -AsHashtable
        Assert-EmailCompose ($configuration.networks.app.internal -eq $true) 'app must remain internal'
        Assert-EmailCompose ($configuration.networks.data.internal -eq $true) 'data must remain internal'
        Assert-EmailCompose ($configuration.networks.ContainsKey('registration-egress')) 'SMTP egress network missing'
        Assert-EmailCompose (-not $configuration.networks['registration-egress'].internal) 'SMTP egress must permit outbound connectivity'
        Assert-EmailCompose ($configuration.networks['registration-egress'].driver -eq 'bridge') 'SMTP egress must use a bridge'
        foreach ($name in $configuration.services.Keys) {
            $attached = $configuration.services[$name].networks.ContainsKey('registration-egress')
            Assert-EmailCompose ($attached -eq ($name -eq 'api')) 'Only API may attach the SMTP egress network'
            if ($name -ne 'api') {
                foreach ($secret in @($configuration.services[$name].secrets)) {
                    Assert-EmailCompose ($secret.source -notin @('registration_smtp_password', 'registration_code_key')) 'Registration secrets must stay API-only'
                }
            }
        }
        $api = $configuration.services.api
        Assert-EmailCompose ($api.networks.ContainsKey('app') -and $api.networks.ContainsKey('data')) 'API must retain internal service/database networks'
        Assert-EmailCompose (@($api.ports).Where({ $null -ne $_ }).Count -eq 0) 'API must not publish ports'
        Assert-EmailCompose ($api.environment.FORGEFLOW_SMTP_PASSWORD_FILE -eq '/run/secrets/registration_smtp_password') 'SMTP password must use the private file'
        Assert-EmailCompose ($api.environment.FORGEFLOW_REGISTRATION_CODE_KEY_FILE -eq '/run/secrets/registration_code_key') 'Code key must use the private file'
    }
}
finally {
    foreach ($name in $previous.Keys) { [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process') }
    Pop-Location
}
Write-Host 'Email registration Compose contract passed for base and public-IP HTTPS overlays. No email was sent.'
