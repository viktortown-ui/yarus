$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$dist = Join-Path $root 'dist'
$target = Join-Path $dist 'RuStore'
$binaries = Join-Path $target 'binaries'
$icon = Join-Path $target 'icon'
$metadata = Join-Path $target 'metadata'
$zip = Join-Path $dist 'YARUS-1.0.0-RuStore-submission.zip'

foreach ($folder in $target,$binaries,$icon,$metadata) { New-Item -ItemType Directory -Force -Path $folder | Out-Null }
foreach ($required in @(
    (Join-Path $dist 'android\YARUS-1.0.0-RuStore.apk'),
    (Join-Path $dist 'android\YARUS-1.0.0-RuStore.aab'),
    (Join-Path $target 'screenshots\phone\01-overview-1080x1920.png'),
    (Join-Path $target 'screenshots\tablet\01-overview-1600x2560.png')
)) { if (-not (Test-Path -LiteralPath $required)) { throw "Missing release artifact: $required" } }

Copy-Item -LiteralPath (Join-Path $dist 'android\YARUS-1.0.0-RuStore.apk'),(Join-Path $dist 'android\YARUS-1.0.0-RuStore.aab'),(Join-Path $dist 'android\SHA256SUMS.txt') -Destination $binaries -Force
Copy-Item -LiteralPath (Join-Path $root 'branding\YARUS-512.png') -Destination (Join-Path $icon 'YARUS-icon-512x512.png') -Force
Copy-Item -Path (Join-Path $root 'rustore\*.txt') -Destination $metadata -Force
Copy-Item -LiteralPath (Join-Path $root 'docs\privacy-policy.html') -Destination (Join-Path $target 'privacy-policy.html') -Force
Copy-Item -LiteralPath (Join-Path $root 'docs\ИНСТРУКЦИЯ-1.0.0.html') -Destination (Join-Path $target 'ИНСТРУКЦИЯ.html') -Force

$phoneCount = @(Get-ChildItem -LiteralPath (Join-Path $target 'screenshots\phone') -Filter '*.png').Count
$tabletCount = @(Get-ChildItem -LiteralPath (Join-Path $target 'screenshots\tablet') -Filter '*.png').Count
if ($phoneCount -lt 3 -or $tabletCount -lt 3) { throw 'RuStore needs at least three screenshots for each selected device type.' }
if (Get-ChildItem -LiteralPath $target -Recurse -File | Where-Object { $_.Name -match 'jks|signing\.properties|PRIVATE' }) {
    throw 'Private signing material must never enter the RuStore package.'
}

$hashFiles = Get-ChildItem -LiteralPath $target -Recurse -File | Where-Object { $_.Name -ne 'SHA256SUMS-ALL.txt' }
$hashFiles | Get-FileHash -Algorithm SHA256 | Sort-Object Path |
    ForEach-Object { "$($_.Hash.ToLowerInvariant())  $($_.Path.Substring($target.Length + 1).Replace('\','/'))" } |
    Set-Content -LiteralPath (Join-Path $target 'SHA256SUMS-ALL.txt') -Encoding ascii

if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip -Force }
Compress-Archive -LiteralPath $target -DestinationPath $zip -CompressionLevel Optimal
Write-Host "Prepared: $target ($phoneCount phone + $tabletCount tablet screenshots)"
Write-Host "Prepared: $zip"
