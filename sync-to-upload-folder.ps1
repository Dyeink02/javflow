# Sync javflow source to the clean upload folder.
# Usage: run in PowerShell5 as ./sync-to-upload-folder.ps1, or double-click sync-to-upload-folder.bat.
$ErrorActionPreference = "Stop"

$sourceDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $sourceDir
$uploadDir = Join-Path $repoRoot "javflow-github-upload-clean"

if (-not (Test-Path (Join-Path $sourceDir ".gitignore"))) {
    throw "Source directory must be $sourceDir with a .gitignore file."
}

# Ensure upload folder exists.
if (-not (Test-Path $uploadDir)) {
    New-Item -ItemType Directory -Path $uploadDir | Out-Null
}

# Gather files that should be uploaded: tracked working-tree files plus
# untracked files that are not ignored. The working tree is intentional here:
# README edits do not need to be staged before syncing.
Set-Location $sourceDir
$tracked = git -c core.quotePath=false ls-files 2>$null | ForEach-Object { $_.Trim('"') }
$untracked = git -c core.quotePath=false ls-files --others --exclude-standard 2>$null | ForEach-Object { $_.Trim('"') }
$allSourceFiles = @($tracked) + @($untracked) |
    Where-Object { $_ } |
    Sort-Object -Unique

$sourceFiles = $allSourceFiles |
    Where-Object { $_ -eq 'README.md' -or $_ -notlike 'README-*.md' }

if (-not $sourceFiles) {
    throw "No source files found to sync."
}

# Copy files to the upload folder, preserving relative structure.
$uploadedPaths = New-Object System.Collections.Generic.HashSet[string]
foreach ($relPath in $sourceFiles) {
    $src = Join-Path $sourceDir $relPath
    $dst = Join-Path $uploadDir $relPath
    if (-not (Test-Path -LiteralPath $src -PathType Leaf)) {
        continue
    }

    $dstParent = Split-Path -Parent $dst
    if (-not (Test-Path -LiteralPath $dstParent)) {
        New-Item -ItemType Directory -Path $dstParent -Force | Out-Null
    }

    Copy-Item -LiteralPath $src -Destination $dst -Force
    [void]$uploadedPaths.Add($relPath)
}

Write-Host "Synced $($uploadedPaths.Count) files to $uploadDir"
Write-Host "README source: README.md"
