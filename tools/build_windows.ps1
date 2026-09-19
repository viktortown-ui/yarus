$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
New-Item -ItemType Directory -Force dist | Out-Null
$go = Join-Path (Get-Location) '.tooling\go\bin\go.exe'
if (-not (Test-Path -LiteralPath $go)) { $go = (Get-Command go -ErrorAction Stop).Source }
$loader = Join-Path (Get-Location) '.tooling\webview2\WebView2Loader.dll'
if (-not (Test-Path -LiteralPath $loader)) { throw 'Missing .tooling\webview2\WebView2Loader.dll' }
python tools/pack_web.py
if ($LASTEXITCODE -ne 0) { throw 'Web packaging failed' }
$env:CGO_ENABLED='0'; $env:GOOS='windows'; $env:GOARCH='amd64'
& $go test ./...
if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
& $go build -trimpath -ldflags='-s -w -H=windowsgui' -o dist/YARUS.exe .
if ($LASTEXITCODE -ne 0) { throw 'GUI build failed' }
python tools/pe_icon.py dist/YARUS.exe branding/YARUS.ico
if ($LASTEXITCODE -ne 0) { throw 'Icon embedding failed' }
& $go build -trimpath -ldflags='-s -w' -o dist/YARUS-server.exe .
if ($LASTEXITCODE -ne 0) { throw 'Server build failed' }
Copy-Item -LiteralPath $loader -Destination 'dist\WebView2Loader.dll' -Force
Write-Host 'Built: dist\YARUS.exe, dist\YARUS-server.exe and official WebView2Loader.dll (not Authenticode-signed).'
