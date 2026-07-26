# Workspace backup helper for local maintenance snapshots.
# This is the safe pre-edit snapshot tool for this non-git workspace and should
# be used before larger refactors or risky cleanup passes.
#
# Ownership summary:
# 1) create timestamped workspace backups before non-trivial maintenance batches
# 2) keep backup exclusion rules centralized for this non-git workspace
# 3) provide a consistent recovery point before risky edits or packaging work
#
# Boundary rule:
# maintenance-only PowerShell helper; product runtime does not depend on this file.
#
# File map for maintainers:
# 1) input/source/destination parameter handling
# 2) backup exclusion directory policy
# 3) robocopy execution and exit-code enforcement

param(
  [string]$Label = "manual",
  [string]$SourceRoot = (Split-Path -Parent $PSScriptRoot)
)

$resolvedSourceRoot = (Resolve-Path -LiteralPath $SourceRoot).Path
$backupRoot = Join-Path $resolvedSourceRoot "backups"
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$destination = Join-Path $backupRoot "$Label-$timestamp"

New-Item -ItemType Directory -Force -Path $destination | Out-Null

$excludedDirectories = @(
  "backups",
  "node_modules",
  "dist",
  "build",
  ".git",
  "wails-shell\\build",
  "wails-shell\\release"
)

Write-Host "Creating workspace backup..."
Write-Host "Source      : $resolvedSourceRoot"
Write-Host "Destination : $destination"
Write-Host "Excluded    : $($excludedDirectories -join ', ')"

robocopy $resolvedSourceRoot $destination /E /XD $excludedDirectories /NFL /NDL /NJH /NJS /NP /R:1 /W:1 | Out-Null

if ($LASTEXITCODE -gt 7) {
  throw "Backup failed with robocopy exit code $LASTEXITCODE"
}

Write-Output $destination
