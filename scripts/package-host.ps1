# Builds streamgrab-host.exe and stages the per-user Windows installer in dist/windows.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
  $candidate = Join-Path $env:LOCALAPPDATA "streamgrab-go\go\bin\go.exe"
  if (Test-Path -LiteralPath $candidate) { $go = $candidate } else { throw "Go was not found. Install Go or extract it to $candidate." }
} else {
  $go = $go.Source
}

$version = "0.0.0"
$protocol = Join-Path $root "host\internal\protocol\protocol.go"
$match = Select-String -Path $protocol -Pattern 'HostVersion\s+=\s+"([^"]+)"' | Select-Object -First 1
if ($match) { $version = $match.Matches[0].Groups[1].Value }

$stage = Join-Path $root "dist\windows"
if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
New-Item -ItemType Directory -Force -Path $stage | Out-Null
$built = Join-Path $stage "streamgrab-host.exe"
Push-Location (Join-Path $root "host")
& $go build -o $built ./cmd/streamgrab-host
$code = $LASTEXITCODE
Pop-Location
if ($code -ne 0) { throw "host build failed" }

Copy-Item -LiteralPath (Join-Path $root "installer\windows\install.ps1") -Destination $stage
Copy-Item -LiteralPath (Join-Path $root "installer\windows\uninstall.ps1") -Destination $stage
Copy-Item -LiteralPath (Join-Path $root "installer\windows\README.txt") -Destination $stage

$zip = Join-Path $root "dist\StreamGrab-host-$version-windows-x64.zip"
if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip -Force }
Compress-Archive -Path (Join-Path $stage "*") -DestinationPath $zip
Write-Output "Staged: $stage"
Write-Output "Zip: $zip"
exit 0
