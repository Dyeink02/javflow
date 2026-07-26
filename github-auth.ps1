# Shared GitHub CLI authentication helpers for the Windows publishing flow.
# The helper never stores, prints, or accepts account passwords.

$ErrorActionPreference = "Stop"

function Resolve-GhCli {
    $command = Get-Command gh -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -ne $command -and $command.Source) {
        return $command.Source
    }

    $candidates = @(
        (Join-Path ${env:ProgramFiles} "GitHub CLI\gh.exe"),
        (Join-Path ${env:LOCALAPPDATA} "Programs\GitHub CLI\gh.exe")
    )
    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path -LiteralPath $candidate)) {
            return $candidate
        }
    }

    throw "GitHub CLI (gh.exe) was not found. Install GitHub CLI, then run login-github.bat."
}

function Get-GitHubLogin {
    param([Parameter(Mandatory = $true)][string]$GhPath)

    $login = (& $GhPath api user --jq .login 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($login)) {
        return ""
    }
    return $login
}

function Ensure-GitHubAuth {
    param([switch]$Force)

    $ghPath = Resolve-GhCli
    if (-not $Force) {
        & $ghPath auth status --hostname github.com *> $null
        if ($LASTEXITCODE -eq 0) {
            $existingLogin = Get-GitHubLogin -GhPath $ghPath
            if (-not [string]::IsNullOrWhiteSpace($existingLogin)) {
                & $ghPath auth setup-git
                if ($LASTEXITCODE -ne 0) {
                    throw "GitHub CLI is logged in as $existingLogin, but Git credential setup failed."
                }
                Write-Host "GitHub login OK: $existingLogin"
                return $existingLogin
            }
        }
    }

    Write-Host "GitHub login is required."
    Write-Host "A one-time device code will be printed in this window."
    Write-Host "If no browser opens, visit https://github.com/login/device manually."
    & $ghPath auth login --hostname github.com --git-protocol https --web
    if ($LASTEXITCODE -ne 0) {
        throw "GitHub device login failed or was cancelled."
    }

    $login = Get-GitHubLogin -GhPath $ghPath
    if ([string]::IsNullOrWhiteSpace($login)) {
        throw "GitHub login returned without a verified account. Run login-github.bat again."
    }

    & $ghPath auth setup-git
    if ($LASTEXITCODE -ne 0) {
        throw "GitHub login succeeded for $login, but Git credential setup failed."
    }

    Write-Host "GitHub login successful: $login"
    return $login
}
