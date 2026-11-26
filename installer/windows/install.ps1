# Per-user StreamGrab companion installer. No administrator prompt.
# Unsigned. streamgrab-host.exe must sit next to this script.
# yt-dlp, FFmpeg, and Deno stay separate installs. yt-dlp can update itself later.
param(
  [string]$ExtensionId = "empedidcmdedncnndnonehnalohohacm",
  [string]$FirefoxId = "streamgrab@local",
  [string]$SourceExe = ""
)

$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $SourceExe) { $SourceExe = Join-Path $here "streamgrab-host.exe" }
if (-not (Test-Path -LiteralPath $SourceExe)) {
  throw "streamgrab-host.exe was not found next to install.ps1."
}

$installDir = Join-Path $env:LOCALAPPDATA "StreamGrab"
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$dest = Join-Path $installDir "streamgrab-host.exe"
$sourceFull = (Resolve-Path -LiteralPath $SourceExe).Path
if ($sourceFull -ne $dest) {
  try {
    Copy-Item -LiteralPath $sourceFull -Destination $dest -Force
  } catch {
    $previous = "$dest.previous"
    if (Test-Path -LiteralPath $previous) {
      Remove-Item -LiteralPath $previous -Force -ErrorAction SilentlyContinue
    }
    Rename-Item -LiteralPath $dest -NewName "streamgrab-host.exe.previous"
    Copy-Item -LiteralPath $sourceFull -Destination $dest -Force
    Write-Output "A host was still running. Quit the browser completely so the next connection uses this build."
  }
}

function Write-Utf8([string]$Path, [string]$Text) {
  $utf8 = New-Object System.Text.UTF8Encoding $false
  [System.IO.File]::WriteAllText($Path, $Text.Trim() + "`n", $utf8)
}

$chromeManifest = Join-Path $installDir "com.streamgrab.host.chrome.json"
$firefoxManifest = Join-Path $installDir "com.streamgrab.host.firefox.json"
Write-Utf8 $chromeManifest ((@{
  name = "com.streamgrab.host"
  description = "StreamGrab companion host"
  path = $dest
  type = "stdio"
  allowed_origins = @("chrome-extension://$ExtensionId/")
} | ConvertTo-Json))
Write-Utf8 $firefoxManifest ((@{
  name = "com.streamgrab.host"
  description = "StreamGrab companion host"
  path = $dest
  type = "stdio"
  allowed_extensions = @($FirefoxId)
} | ConvertTo-Json))

New-Item -Path "HKCU:\Software\Google\Chrome\NativeMessagingHosts\com.streamgrab.host" -Force | Out-Null
Set-ItemProperty -Path "HKCU:\Software\Google\Chrome\NativeMessagingHosts\com.streamgrab.host" -Name "(default)" -Value $chromeManifest
New-Item -Path "HKCU:\Software\Mozilla\NativeMessagingHosts\com.streamgrab.host" -Force | Out-Null
Set-ItemProperty -Path "HKCU:\Software\Mozilla\NativeMessagingHosts\com.streamgrab.host" -Name "(default)" -Value $firefoxManifest

function Find-Tool([string]$Name) {
  $dirs = @(
    [Environment]::GetEnvironmentVariable("Path", "Process"),
    [Environment]::GetEnvironmentVariable("Path", "User"),
    [Environment]::GetEnvironmentVariable("Path", "Machine")
  ) | Where-Object { $_ }
  $seen = @{}
  foreach ($entry in (($dirs -join ";") -split ";")) {
    $dir = $entry.Trim()
    if (-not $dir -or $seen.ContainsKey($dir.ToLowerInvariant())) { continue }
    $seen[$dir.ToLowerInvariant()] = $true
    $candidate = Join-Path $dir ($Name + ".exe")
    if (Test-Path -LiteralPath $candidate) { return $candidate }
  }
  return $null
}

function Tool-Line([string]$Name) {
  $path = Find-Tool $Name
  if (-not $path) { return "missing" }
  $previous = $ErrorActionPreference
  $ErrorActionPreference = "Continue"
  $line = & $path -version 2>&1 | Select-Object -First 1
  if ($line -is [System.Management.Automation.ErrorRecord] -or -not "$line") {
    $line = & $path --version 2>&1 | Select-Object -First 1
  }
  $ErrorActionPreference = $previous
  $text = "$line".Trim()
  if (-not $text) { return $path }
  return "$path ($text)"
}

$missing = @()
$yt = Tool-Line "yt-dlp"
$ff = Tool-Line "ffmpeg"
$denoPath = Find-Tool "deno"
$denoLine = "missing"
if (-not $denoPath) {
  $missing += "deno"
} else {
  $denoRaw = & $denoPath --version 2>&1 | Select-Object -First 1
  $denoLine = "$denoPath ($denoRaw)"
  if ($denoRaw -notmatch "(\d+)\.(\d+)") { $missing += "deno" }
  else {
    $major = [int]$Matches[1]
    $minor = [int]$Matches[2]
    if ($major -lt 2 -or ($major -eq 2 -and $minor -lt 3)) { $missing += "deno" }
  }
}
if ($yt -eq "missing") { $missing += "yt-dlp" }
if ($ff -eq "missing") { $missing += "ffmpeg" }

Write-Output "Installed: $dest"
Write-Output "This build is unsigned."
Write-Output "Chrome extension id: $ExtensionId"
Write-Output "Firefox id: $FirefoxId"
Write-Output "yt-dlp: $yt"
Write-Output "ffmpeg: $ff"
Write-Output "deno: $denoLine"
Write-Output "1. Keep yt-dlp, FFmpeg, and Deno 2.3+ installed. The host does not bundle them."
Write-Output "2. Load the extension from chrome://extensions or Firefox about:debugging."
Write-Output "3. Quit the browser completely, then open a YouTube video."
if ($missing.Count -gt 0) {
  Write-Output ("Missing or too old: " + ($missing -join ", ") + ". The host is registered anyway.")
  exit 2
}
exit 0
