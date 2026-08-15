# Ownership summary:
#   Windows UI diagnostic script: enumerate windows for a running EXE and capture screenshots.
#
# File map for maintainers:
#   1) Parameter block and Win32 P/Invoke declarations.
#   2) Window enumeration helpers.
#   3) Screenshot capture and output.
#
param(
  [string]$ExePath = ""
)

$ErrorActionPreference = "SilentlyContinue"
$repoRoot = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($ExePath)) {
  $ExePath = Join-Path $repoRoot 'wails-shell\release\javflow.exe'
}

Add-Type @"
using System;
using System.Runtime.InteropServices;
using System.Text;
public class Win32Diag {
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc enumProc, IntPtr lParam);
  [DllImport("user32.dll", SetLastError = true)] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint lpdwProcessId);
  [DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder lpString, int nMaxCount);
  [DllImport("user32.dll", SetLastError = true)] public static extern bool IsWindowVisible(IntPtr hWnd);
  [DllImport("user32.dll", SetLastError = true)] public static extern bool GetWindowRect(IntPtr hWnd, out RECT lpRect);
  [DllImport("user32.dll")] public static extern IntPtr GetWindowDC(IntPtr hWnd);
  [DllImport("gdi32.dll")] public static extern IntPtr CreateCompatibleDC(IntPtr hdc);
  [DllImport("gdi32.dll")] public static extern IntPtr CreateCompatibleBitmap(IntPtr hdc, int nWidth, int nHeight);
  [DllImport("gdi32.dll")] public static extern IntPtr SelectObject(IntPtr hdc, IntPtr hgdiobj);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr hWnd, IntPtr hdcBlt, uint nFlags);
  [DllImport("gdi32.dll")] public static extern bool DeleteObject(IntPtr hObject);
  [DllImport("gdi32.dll")] public static extern bool DeleteDC(IntPtr hdc);
  [DllImport("user32.dll")] public static extern int ReleaseDC(IntPtr hWnd, IntPtr hDC);
  public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);
  public struct RECT { public int Left; public int Top; public int Right; public int Bottom; }
}
"@

function CaptureWindow($hWnd, $path) {
  $rect = New-Object Win32Diag+RECT
  if (-not [Win32Diag]::GetWindowRect($hWnd, [ref]$rect)) { return }
  $w = $rect.Right - $rect.Left
  $h = $rect.Bottom - $rect.Top
  if ($w -le 0 -or $h -le 0) { return }
  Add-Type -AssemblyName System.Drawing
  $hwndDC = [Win32Diag]::GetWindowDC($hWnd)
  $hdc = [Win32Diag]::CreateCompatibleDC($hwndDC)
  $bitmap = [Win32Diag]::CreateCompatibleBitmap($hwndDC, $w, $h)
  $old = [Win32Diag]::SelectObject($hdc, $bitmap)
  [Win32Diag]::PrintWindow($hWnd, $hdc, 2) | Out-Null
  $bmp = [System.Drawing.Image]::FromHbitmap($bitmap)
  $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
  $bmp.Dispose()
  [Win32Diag]::SelectObject($hdc, $old) | Out-Null
  [Win32Diag]::DeleteObject($bitmap) | Out-Null
  [Win32Diag]::DeleteDC($hdc) | Out-Null
  [Win32Diag]::ReleaseDC($hWnd, $hwndDC) | Out-Null
}

$proc = Start-Process -FilePath $ExePath -PassThru
Start-Sleep -Seconds 6

Write-Host "Process ID: $($proc.Id)"

$windows = @()
$cb = {
  param($hWnd, $lParam)
  if (-not [Win32Diag]::IsWindowVisible($hWnd)) { return $true }
  $pidOut = 0
  [void][Win32Diag]::GetWindowThreadProcessId($hWnd, [ref]$pidOut)
  $sb = New-Object System.Text.StringBuilder 512
  [void][Win32Diag]::GetWindowText($hWnd, $sb, 512)
  $title = $sb.ToString()
  $rect = New-Object Win32Diag+RECT
  [void][Win32Diag]::GetWindowRect($hWnd, [ref]$rect)
  $windows += [pscustomobject]@{
    Hwnd = $hWnd
    Pid = $pidOut
    Title = $title
    Left = $rect.Left
    Top = $rect.Top
    Width = ($rect.Right - $rect.Left)
    Height = ($rect.Bottom - $rect.Top)
    IsTarget = ($pidOut -eq $proc.Id)
  }
  return $true
}

[Win32Diag]::EnumWindows([Win32Diag+EnumWindowsProc]$cb, [IntPtr]::Zero) | Out-Null

$outRoot = Join-Path $repoRoot 'log'
New-Item -ItemType Directory -Path $outRoot -Force | Out-Null
$windows | Format-Table -AutoSize | Out-String | Tee-Object -FilePath "$outRoot\diag-windows.txt"

$i = 0
foreach ($w in $windows) {
  $i++
  $path = "$outRoot\diag-window-$($i.ToString('00'))-pid$($w.Pid).png"
  CaptureWindow $w.Hwnd $path
}

Stop-Process -Id $proc.Id -Force
