# One-click sync current project source to GitHub.
# Usage: run in PowerShell5 as ./sync-github.ps1, or double-click sync-github.bat.
param(
    [string]$Message = "",
    [string]$RemoteUrl = "https://github.com/Dyeink02/javflow.git",
    [string]$Branch = "main",
    [string]$TokenFile = ""
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $repoRoot

# Read GitHub Personal Access Token from a local file so it is never committed.
$tokenPath = if ($TokenFile) { $TokenFile } else { Join-Path $repoRoot ".github-token" }
$pat = ""
if (Test-Path $tokenPath) {
    $pat = (Get-Content $tokenPath -Raw -ErrorAction SilentlyContinue).Trim()
}
if (-not $pat) {
    Write-Host "GitHub token not found. Create a Personal Access Token and save it to:" -ForegroundColor Yellow
    Write-Host "  $tokenPath" -ForegroundColor Yellow
    Write-Host "See https://github.com/settings/tokens (scopes: repo)" -ForegroundColor Yellow
    exit 1
}

# Build authenticated remote URL only in memory; the real URL in .git/config stays clean.
$remoteUrlWithToken = "https://${pat}@" + $RemoteUrl.Substring(8)

# Common git options: pin GitHub URLs to themselves so local mirror configs cannot redirect them.
$gitBaseArgs = @("-c", "url.https://github.com/.insteadOf=https://github.com/")

function Invoke-ReqGit {
    param([Parameter(ValueFromRemainingArguments=$true)][string[]]$PassArgs)
    & git @gitBaseArgs @PassArgs
    if ($LASTEXITCODE -ne 0) {
        throw "git $PassArgs failed with exit code $LASTEXITCODE"
    }
}

function Invoke-OptGit {
    param([Parameter(ValueFromRemainingArguments=$true)][string[]]$PassArgs)
    & git @gitBaseArgs @PassArgs
}

# Initialize repo and add remote if missing.
if (-not (Test-Path .git)) {
    Invoke-ReqGit init
}
$remotes = Invoke-OptGit remote -v 2>$null
if (-not $remotes) {
    Invoke-ReqGit remote add origin $RemoteUrl
}

# Fetch remote branch. If it exists, base local branch on it to keep history.
$remoteExists = $false
try {
    Invoke-ReqGit fetch $remoteUrlWithToken $Branch 2>$null | Out-Null
    $remoteExists = $true
} catch {
    $remoteExists = $false
}

$remoteBranches = Invoke-OptGit branch -r 2>$null
if ($remoteExists -and ($remoteBranches | Select-String "origin/$Branch")) {
    $localBranches = Invoke-OptGit branch --list $Branch 2>$null
    if (-not $localBranches) {
        Invoke-ReqGit checkout -b $Branch origin/$Branch
    } else {
        Invoke-ReqGit checkout $Branch
        Invoke-ReqGit pull $remoteUrlWithToken $Branch
    }
} else {
    Invoke-ReqGit checkout -B $Branch
}

# Stage all changes. .gitignore controls what is excluded (node_modules, build, release, etc.).
Invoke-ReqGit add .

$commitMsg = if ($Message) { $Message } else { "update: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" }
$hasChanges = Invoke-OptGit status --short 2>$null
if ($hasChanges) {
    Invoke-ReqGit commit -m "$commitMsg"
} else {
    Write-Host "No changes to commit."
}

# Push to GitHub.
Invoke-ReqGit push $remoteUrlWithToken $Branch
Write-Host "Sync complete: $RemoteUrl ($Branch)"
