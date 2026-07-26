# Compatibility entry point for the one-click publisher.
param(
    [string]$Message = "",
    [string]$RemoteUrl = "https://github.com/Dyeink02/javflow.git",
    [string]$Branch = "main",
    [switch]$DryRun
)

$publisher = Join-Path (Split-Path -Parent $MyInvocation.MyCommand.Path) 'publish-to-github.ps1'
& powershell -NoProfile -ExecutionPolicy Bypass -File $publisher -Message $Message -RemoteUrl $RemoteUrl -Branch $Branch -DryRun:$DryRun
exit $LASTEXITCODE
