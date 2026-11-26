# Builds the host from source and runs the per-user installer.
# install.ps1 is what a machine without Go runs. This script is the developer path.
$ErrorActionPreference = "Stop"
& (Join-Path $PSScriptRoot "package-host.ps1")
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$root = Split-Path -Parent $PSScriptRoot
& (Join-Path $root "dist\windows\install.ps1")
exit $LASTEXITCODE
