$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED='0'; $env:GOOS='windows'; $env:GOARCH='amd64'
go test ./...
if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
go build -trimpath -ldflags='-s -w -H=windowsgui' -o dist/YARUS.exe .
if ($LASTEXITCODE -ne 0) { throw 'GUI build failed' }
python tools/pe_icon.py dist/YARUS.exe branding/YARUS.ico
if ($LASTEXITCODE -ne 0) { throw 'Icon embedding failed' }
go build -trimpath -ldflags='-s -w' -o dist/YARUS-server.exe .
if ($LASTEXITCODE -ne 0) { throw 'Server build failed' }
Write-Host 'Built: dist\YARUS.exe and dist\YARUS-server.exe (not Authenticode-signed).'
