# One-click publish for the cleaned JavFlow source directory.
# Authentication is delegated to Git Credential Manager / `gh auth login`.
param(
    [string]$Message = "",
    [string]$RemoteUrl = "https://github.com/Dyeink02/javflow.git",
    [string]$Branch = "main",
    [string]$BaseBranch = "main",
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -LiteralPath $repoRoot
. (Join-Path $repoRoot "github-auth.ps1")

function Invoke-Git {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Args)
    & git @Args
    if ($LASTEXITCODE -ne 0) {
        throw "git $($Args -join ' ') failed with exit code $LASTEXITCODE."
    }
}

function Test-GitRef {
    param([string]$Ref)
    & git rev-parse --verify --quiet $Ref *> $null
    return $LASTEXITCODE -eq 0
}

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    throw "Git for Windows was not found."
}

$branch = $Branch.Trim()
if ($branch -notmatch '^[A-Za-z0-9._/-]+$' -or $branch.StartsWith('/') -or $branch.EndsWith('/')) {
    throw "Invalid branch name: $Branch"
}
$baseBranch = $BaseBranch.Trim()
if ($baseBranch -notmatch '^[A-Za-z0-9._/-]+$' -or $baseBranch.StartsWith('/') -or $baseBranch.EndsWith('/')) {
    throw "Invalid base branch name: $BaseBranch"
}

if (-not (Test-Path -LiteralPath (Join-Path $repoRoot '.git'))) {
    Invoke-Git init -b $branch
}

$remoteNames = @(git remote)
if ($remoteNames -notcontains 'origin') {
    Invoke-Git remote add origin $RemoteUrl
} else {
    $originUrl = ((git config --get remote.origin.url) | Out-String).Trim()
    if ($originUrl -ne $RemoteUrl.Trim()) {
        Invoke-Git remote set-url origin $RemoteUrl
    }
}

$baseRemoteRef = "refs/remotes/origin/$baseBranch"
$targetRemoteRef = "refs/remotes/origin/$branch"
$baseRemoteExists = $false
& git ls-remote --exit-code --heads origin $baseBranch *> $null
if ($LASTEXITCODE -eq 0) {
    $baseRemoteExists = $true
    Invoke-Git fetch origin $baseBranch
}
$targetRemoteExists = $false
if ($branch -ne $baseBranch) {
    & git ls-remote --exit-code --heads origin $branch *> $null
    if ($LASTEXITCODE -eq 0) {
        $targetRemoteExists = $true
        Invoke-Git fetch origin $branch
    }
}

if (-not (Test-GitRef 'HEAD')) {
    if ($baseRemoteExists -and (Test-GitRef $baseRemoteRef)) {
        # Adopt the remote tree without overwriting the cleaned working files.
        Invoke-Git reset --mixed $baseRemoteRef
        Invoke-Git branch -M $baseBranch
    } else {
        Invoke-Git checkout -B $baseBranch
    }
}

$currentBranch = (& git branch --show-current).Trim()
if ($currentBranch -ne $branch) {
    if (Test-GitRef "refs/heads/$branch") {
        Invoke-Git checkout $branch
    } else {
        Invoke-Git checkout -b $branch
    }
}

if ($targetRemoteExists -and (Test-GitRef $targetRemoteRef)) {
    & git merge-base --is-ancestor HEAD $targetRemoteRef
    if ($LASTEXITCODE -ne 0) {
        & git merge-base --is-ancestor $targetRemoteRef HEAD
        if ($LASTEXITCODE -ne 0) {
            throw "Local branch and remote $branch have diverged. Resolve manually; this script never overwrites remote history."
        }
        Invoke-Git merge --ff-only $targetRemoteRef
    }
}

& git add -A
if ($LASTEXITCODE -ne 0) {
    throw "git add -A failed with exit code $LASTEXITCODE."
}
$staged = @(git diff --cached --name-only)
$sensitive = @($staged | Where-Object {
    $_ -match '(^|/)(\.env(\..*)?|\.github-token|credentials[^/]*|secrets[^/]*|[^/]+\.(pem|key|p12|pfx))$'
})
if ($sensitive.Count -gt 0) {
    throw "Credential-like files detected; commit stopped: $($sensitive -join ', ')"
}

if ($DryRun) {
    Write-Host "Dry run: no commit or push will be performed."
    git status --short
    git diff --cached --stat
    exit 0
}

$hasChanges = $false
& git diff --cached --quiet
if ($LASTEXITCODE -ne 0) {
    $hasChanges = $true
}
if ($hasChanges) {
    $commitMessage = if ($Message.Trim()) { $Message.Trim() } else { "publish: JavFlow source $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" }
    Invoke-Git commit -m $commitMessage
} else {
    Write-Host "No files to commit."
}

$isGitHubRemote = $RemoteUrl -match '(^|[/:])github\.com([/:]|$)'
if ($isGitHubRemote) {
    $null = Ensure-GitHubAuth
}
Invoke-Git push --set-upstream origin $branch
Write-Host "Pushed: $RemoteUrl ($branch)"
