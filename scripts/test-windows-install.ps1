# Installs a Windows installer from scripts/package-windows.sh on this PC,
# checks that Matinee runs from the tray app, and uninstalls it again. CI
# runs it on a fresh Windows runner; don't run it on a PC you use.
# Usage: scripts/test-windows-install.ps1 -Installer dist\Matinee-Setup-x64.exe [-Shots dir]
param(
  [Parameter(Mandatory)] [string] $Installer,
  [string] $Shots = ""
)
$ErrorActionPreference = "Stop"
$dir = Join-Path $env:ProgramFiles "Matinee"
$data = Join-Path $env:LOCALAPPDATA "Matinee"
$base = "http://localhost:7171/api/v1"

function Check($ok, $what) {
  if (-not $ok) { throw "FAILED: $what" }
  Write-Host "ok: $what"
}

Add-Type -AssemblyName System.Windows.Forms, System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class Win {
  [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern IntPtr FindWindow(string cls, string title);
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint msg, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
}
"@

# Saves the screen, or the part of it around a point, as a PNG.
function Shot($name, $x = 0, $y = 0, $w = 0, $h = 0) {
  if (-not $Shots) { return }
  New-Item -ItemType Directory -Force $Shots | Out-Null
  $screen = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
  if ($w -eq 0) { $x, $y, $w, $h = 0, 0, $screen.Width, $screen.Height }
  $bmp = New-Object System.Drawing.Bitmap $w, $h
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.CopyFromScreen($x, $y, 0, 0, $bmp.Size)
  $bmp.Save((Join-Path $Shots "$name.png"), [System.Drawing.Imaging.ImageFormat]::Png)
  $g.Dispose(); $bmp.Dispose()
}

# Install the way the tray app updates: silently.
$p = Start-Process -FilePath $Installer -ArgumentList "/S" -Wait -PassThru
Check ($p.ExitCode -eq 0) "installer exits 0 (got $($p.ExitCode))"
foreach ($f in "matinee.exe", "matinee-tray.exe", "uninstall.exe", "LICENSE.txt", "ffmpeg\ffmpeg.exe", "ffmpeg\ffprobe.exe", "ffmpeg\LICENSE.txt") {
  Check (Test-Path (Join-Path $dir $f)) "installed $f"
}
& (Join-Path $dir "ffmpeg\ffmpeg.exe") -hide_banner -version | Select-Object -First 1
Check ($LASTEXITCODE -eq 0) "bundled ffmpeg runs"
Check (Test-Path "$env:ProgramData\Microsoft\Windows\Start Menu\Programs\Matinee.lnk") "Start menu shortcut"
$rule = Get-NetFirewallRule -DisplayName "Matinee" -ErrorAction SilentlyContinue
Check ($null -ne $rule) "firewall rule"
Check (($rule | Get-NetFirewallApplicationFilter).Program -eq (Join-Path $dir "matinee.exe")) "firewall rule is for matinee.exe"
$uninst = Get-ItemProperty "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Matinee"
Check ($uninst.DisplayName -eq "Matinee") "listed in Installed apps as $($uninst.DisplayName) $($uninst.DisplayVersion)"

# The first start: the tray app starts the server, shows its welcome and
# turns on starting at sign-in.
Start-Process -FilePath (Join-Path $dir "matinee-tray.exe")
$health = $null
foreach ($i in 1..60) {
  try { $health = Invoke-RestMethod "$base/health" -TimeoutSec 2; break } catch { Start-Sleep 1 }
}
Check ($health.status -eq "ok") "server answers ($($health.version))"
Check ((Get-Process matinee -ErrorAction SilentlyContinue).Count -eq 1) "one server running"
Check (Test-Path (Join-Path $data "matinee.db")) "database in $data"
Check (Test-Path (Join-Path $data "Logs\matinee.log")) "log in $data\Logs"
Check (Test-Path (Join-Path $data "matinee.env")) "settings file"
$run = (Get-ItemProperty "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" -ErrorAction SilentlyContinue).Matinee
Check ($run -like "*matinee-tray.exe*--background*") "starts at sign-in ($run)"

Start-Sleep 2
$welcome = [Win]::FindWindow("#32770", "Welcome to Matinee")
Check ($welcome -ne [IntPtr]::Zero) "welcome message shows"
[Win]::SetForegroundWindow($welcome) | Out-Null
Start-Sleep 1
Shot "welcome"
[System.Windows.Forms.SendKeys]::SendWait("{ENTER}")

# A second start opens the running one rather than starting another server.
Start-Process -FilePath (Join-Path $dir "matinee-tray.exe") -ArgumentList "--background" -Wait
Check ((Get-Process matinee-tray).Count -eq 1) "a second start exits"

# The tray menu, opened the way a click opens it.
$tray = [Win]::FindWindow("SystrayClass", $null)
Check ($tray -ne [IntPtr]::Zero) "tray icon window"
$screen = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$x, $y = ($screen.Width - 40), ($screen.Height - 60)
[Win]::SetCursorPos($x, $y) | Out-Null
[Win]::PostMessage($tray, 0x0401, [IntPtr]::Zero, [IntPtr]0x0205) | Out-Null # WM_USER+1, WM_RBUTTONUP
Start-Sleep 2
Shot "tray-menu" ($screen.Width - 420) ($screen.Height - 480) 420 480
Shot "tray-desktop"
[System.Windows.Forms.SendKeys]::SendWait("{ESC}")

# The folder picker lists this PC's drives and full Windows paths.
$login = Invoke-RestMethod -Method Post "$base/setup" -ContentType "application/json" -Body '{"username":"admin","password":"windows-test-1"}'
$auth = @{ Authorization = "Bearer $($login.token)" }
$drives = Invoke-RestMethod "$base/fs?path=/" -Headers $auth
Check ($drives.drives -and ($drives.dirs -contains "C:\")) "folder picker lists drives: $($drives.dirs -join ', ')"
$c = Invoke-RestMethod "$base/fs?path=C:\" -Headers $auth
Check ($c.parent -eq "/" -and $c.paths[0] -like "C:\*") "C:\ lists $($c.paths[0]) and goes up to the drives"

# Ending the tray app stops the server too: it sees its stdin close.
Stop-Process -Name matinee-tray -Force
$stopped = $false
foreach ($i in 1..20) {
  if (-not (Get-Process matinee -ErrorAction SilentlyContinue)) { $stopped = $true; break }
  Start-Sleep 1
}
Check $stopped "server stops with the tray app"
Check ((Get-Content (Join-Path $data "Logs\matinee.log") -Raw) -match "shutting down") "server shut down cleanly"

# Uninstall. _?= keeps the uninstaller from copying itself away, so -Wait waits.
$p = Start-Process -FilePath (Join-Path $dir "uninstall.exe") -ArgumentList "/S", "_?=$dir" -Wait -PassThru
Check ($p.ExitCode -eq 0) "uninstaller exits 0"
Check (-not (Test-Path (Join-Path $dir "matinee.exe"))) "program removed"
Check ($null -eq (Get-NetFirewallRule -DisplayName "Matinee" -ErrorAction SilentlyContinue)) "firewall rule removed"
Check (-not (Test-Path "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Matinee")) "uninstall entry removed"
Check (-not (Test-Path "$env:ProgramData\Microsoft\Windows\Start Menu\Programs\Matinee.lnk")) "shortcut removed"
Check (Test-Path (Join-Path $data "matinee.db")) "a quiet uninstall keeps the library"
Write-Host "Windows install test passed"
