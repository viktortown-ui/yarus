$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$go = Join-Path $root '.tooling\go\bin\go.exe'
if (-not (Test-Path -LiteralPath $go)) { $go = (Get-Command go -ErrorAction Stop).Source }
$output = Join-Path $root 'dist\YARUS-LAUNCHER.exe'
New-Item -ItemType Directory -Force (Split-Path $output -Parent) | Out-Null
$env:CGO_ENABLED = '0'; $env:GOOS = 'windows'; $env:GOARCH = 'amd64'
& $go build -trimpath -ldflags='-s -w -H=windowsgui' -o $output (Join-Path $PSScriptRoot 'launcher\main_windows.go')
if ($LASTEXITCODE -ne 0) { throw 'Launcher build failed.' }
python (Join-Path $PSScriptRoot 'pe_icon.py') $output (Join-Path $root 'branding\YARUS.ico')
if ($LASTEXITCODE -ne 0) { throw 'Launcher icon embedding failed.' }
Write-Host "Built: $output"
