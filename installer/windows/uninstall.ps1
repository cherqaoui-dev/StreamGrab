# Removes the per-user StreamGrab host registration.
# Leaves downloads, jobs, settings, and a managed yt-dlp.exe in place.
$ErrorActionPreference = "Stop"
$installDir = Join-Path $env:LOCALAPPDATA "StreamGrab"
foreach ($name in @(
  "streamgrab-host.exe",
  "streamgrab-host.exe.previous",
  "com.streamgrab.host.chrome.json",
  "com.streamgrab.host.firefox.json"
)) {
  $path = Join-Path $installDir $name
  if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Force }
}
foreach ($key in @(
  "HKCU:\Software\Google\Chrome\NativeMessagingHosts\com.streamgrab.host",
  "HKCU:\Software\Mozilla\NativeMessagingHosts\com.streamgrab.host"
)) {
  if (Test-Path $key) { Remove-Item -Path $key -Force }
}
Write-Output "Uninstalled the StreamGrab host for this user."
