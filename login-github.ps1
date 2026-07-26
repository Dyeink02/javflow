# Interactive GitHub device login with an explicit success check.
param([switch]$Force)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $repoRoot "github-auth.ps1")

Set-Location -LiteralPath $repoRoot
$null = Ensure-GitHubAuth -Force:$Force
Write-Host "GitHub authentication is ready for git push."
