# Ownership summary:
#   Alternative Windows UI diagnostic: launch EXE, wait for main window, and enumerate visible windows.
#
# File map for maintainers:
#   1) EXE launch and main-window wait loop.
#   2) Win32 enumeration P/Invoke declarations.
#   3) Window list output and cleanup.
#
$ErrorActionPreference = 'Stop'
$exe = Resolve-Path 'wails-shell/release/javflow.exe'
$p = Start-Process -FilePath $exe -PassThru -WindowStyle Normal
Write-Host "Started PID $($p.Id)"
$maxWait = 30
$waited = 0
while ($p.MainWindowHandle -eq 0 -and $waited -lt $maxWait) {
  Start-Sleep -Milliseconds 500
  $p.Refresh()
  $waited++
}
Write-Host "MainWindowHandle: $($p.MainWindowHandle) after $($waited*0.5)s"
Start-Sleep -Seconds 2

Add-Type -TypeDefinition @"
using System; using System.Runtime.InteropServices; using System.Text;
public class WinEnum {
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc enumProc, IntPtr lParam);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint pid);
  [DllImport("user32.dll", CharSet=CharSet.Auto)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder sb, int max);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT r);
  public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);
  public struct RECT { public int Left, Top, Right, Bottom; }
}
"@ -Language CSharp

$outPath = 'e:\JAVAV\源码\JAV-auto-integrated-source-github\diag-windows2.txt'
$sb = New-Object System.Text.StringBuilder 512
$windows = @()
$cb = {
  param($hWnd, $lParam)
  if (-not [WinEnum]::IsWindowVisible($hWnd)) { return $true }
  [void][WinEnum]::GetWindowText($hWnd, $sb, 512)
  $title = $sb.ToString()
  $winPid = 0
  [void][WinEnum]::GetWindowThreadProcessId($hWnd, [ref]$winPid)
  $r = New-Object WinEnum+RECT
  [void][WinEnum]::GetWindowRect($hWnd, [ref]$r)
  $windows += [pscustomobject]@{
    Hwnd = [int]$hWnd
    Pid = [int]$winPid
    Title = $title
    Left = $r.Left
    Top = $r.Top
    Width = ($r.Right - $r.Left)
    Height = ($r.Bottom - $r.Top)
    IsTarget = ($winPid -eq $p.Id)
  }
  return $true
}
[WinEnum]::EnumWindows([WinEnum+EnumWindowsProc]$cb, [IntPtr]::Zero) | Out-Null
$windows | Sort-Object IsTarget -Descending | Format-Table -AutoSize | Out-String | Tee-Object -FilePath $outPath
Stop-Process -Id $p.Id -Force
