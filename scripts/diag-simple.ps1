# Ownership summary:
#   Quick PowerShell diagnostic: launch the packaged EXE and capture window diagnostics.
#
# File map for maintainers:
#   1) EXE launch and PID capture.
#   2) Visible-window enumeration and rectangle capture.
#   3) Cleanup via process termination.
#
$ErrorActionPreference = 'Stop'
$exe = 'e:\JAVAV\源码\javflow-source-20260725-182718\javflow\wails-shell\release\javflow.exe'
$proc = Start-Process -FilePath $exe -PassThru
Write-Host "Started PID: $($proc.Id)"
Start-Sleep -Seconds 5
Get-Process | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object Id, ProcessName, MainWindowTitle, MainWindowHandle, @{N='WindowRect';E={
  Add-Type -TypeDefinition @"
using System; using System.Runtime.InteropServices;
public class RectHelper {
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT lpRect);
  public struct RECT { public int Left, Top, Right, Bottom; }
  public static string Get(IntPtr hWnd) {
    RECT r; if (!GetWindowRect(hWnd, out r)) return "";
    return $"{r.Left},{r.Top}-{r.Right-r.Left}x{r.Bottom-r.Top}";
  }
}
"@ -Language CSharp -ErrorAction SilentlyContinue
  [RectHelper]::Get($_.MainWindowHandle)
}} | Format-Table -AutoSize | Out-String | Tee-Object -FilePath 'e:\JAVAV\源码\JAV-auto-integrated-source-github\diag-simple.txt'
Stop-Process -Id $proc.Id -Force
