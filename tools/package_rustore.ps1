$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing.Common -ErrorAction Stop
$root = Split-Path $PSScriptRoot -Parent
$dist = Join-Path $root 'dist'
$target = Join-Path $dist 'RuStore'
$binaries = Join-Path $target 'binaries'
$icon = Join-Path $target 'icon'
$metadata = Join-Path $target 'metadata'
$signature = Join-Path $target 'signature'
$zip = Join-Path $dist 'YARUS-1.1.2-RuStore-submission.zip'

New-Item -ItemType Directory -Force -Path $target | Out-Null
foreach ($required in @(
    (Join-Path $dist 'android\YARUS-1.1.2-RuStore.apk'),
    (Join-Path $dist 'android\YARUS-1.1.2-RuStore.aab'),
    (Join-Path $dist 'android\YARUS-upload-certificate.pem'),
    (Join-Path $target 'screenshots\phone\01-overview-1080x1920.png'),
    (Join-Path $target 'screenshots\tablet\01-overview-1600x2560.png')
)) { if (-not (Test-Path -LiteralPath $required)) { throw "Missing release artifact: $required" } }

# Keep freshly generated screenshots, but never mix binaries or metadata from an older release.
foreach ($folder in $binaries,$icon,$metadata,$signature) {
    if (Test-Path -LiteralPath $folder) { Remove-Item -LiteralPath $folder -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $folder | Out-Null
}
foreach ($file in 'ИНСТРУКЦИЯ.html','privacy-policy.html','SHA256SUMS-ALL.txt') {
    $stale = Join-Path $target $file
    if (Test-Path -LiteralPath $stale) { Remove-Item -LiteralPath $stale -Force }
}

Copy-Item -LiteralPath (Join-Path $dist 'android\YARUS-1.1.2-RuStore.apk'),(Join-Path $dist 'android\YARUS-1.1.2-RuStore.aab'),(Join-Path $dist 'android\SHA256SUMS.txt') -Destination $binaries -Force
Copy-Item -LiteralPath (Join-Path $dist 'android\YARUS-upload-certificate.pem') -Destination $signature -Force
$storeIcon = Join-Path $root 'branding\YARUS-RuStore-512.png'
Copy-Item -LiteralPath $storeIcon -Destination (Join-Path $icon 'YARUS-icon-512x512.png') -Force
$iconBitmap = [Drawing.Bitmap]::new((Join-Path $icon 'YARUS-icon-512x512.png'))
try {
    if ($iconBitmap.Width -ne 512 -or $iconBitmap.Height -ne 512) { throw 'RuStore icon must be 512x512.' }
    foreach ($point in @(@(0,0),@(511,0),@(0,511),@(511,511))) {
        if ($iconBitmap.GetPixel($point[0], $point[1]).A -ne 255) { throw 'RuStore icon edges must have an opaque background.' }
    }
} finally {
    $iconBitmap.Dispose()
}
Copy-Item -Path (Join-Path $root 'rustore\*.txt') -Destination $metadata -Force
Copy-Item -LiteralPath (Join-Path $root 'docs\privacy-policy.html') -Destination (Join-Path $target 'privacy-policy.html') -Force
Copy-Item -LiteralPath (Join-Path $root 'docs\ИНСТРУКЦИЯ-1.1.2.html') -Destination (Join-Path $target 'ИНСТРУКЦИЯ.html') -Force

$phoneCount = @(Get-ChildItem -LiteralPath (Join-Path $target 'screenshots\phone') -Filter '*.png').Count
$tabletCount = @(Get-ChildItem -LiteralPath (Join-Path $target 'screenshots\tablet') -Filter '*.png').Count
if ($phoneCount -lt 3 -or $tabletCount -lt 3) { throw 'RuStore needs at least three screenshots for each selected device type.' }
if (Get-ChildItem -LiteralPath $target -Recurse -File | Where-Object { $_.Name -match 'jks|signing\.properties|PRIVATE' }) {
    throw 'Private signing material must never enter the RuStore package.'
}
$certificateText = Get-Content -LiteralPath (Join-Path $signature 'YARUS-upload-certificate.pem') -Raw -Encoding ascii
if ($certificateText -notmatch 'BEGIN CERTIFICATE' -or $certificateText -match 'PRIVATE KEY') {
    throw 'RuStore package must contain only the public PEM certificate.'
}

$hashFiles = Get-ChildItem -LiteralPath $target -Recurse -File | Where-Object { $_.Name -ne 'SHA256SUMS-ALL.txt' }
$hashFiles | Get-FileHash -Algorithm SHA256 | Sort-Object Path |
    ForEach-Object { "$($_.Hash.ToLowerInvariant())  $($_.Path.Substring($target.Length + 1).Replace('\','/'))" } |
    Set-Content -LiteralPath (Join-Path $target 'SHA256SUMS-ALL.txt') -Encoding utf8

if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip -Force }
Compress-Archive -LiteralPath $target -DestinationPath $zip -CompressionLevel Optimal
Write-Host "Prepared: $target ($phoneCount phone + $tabletCount tablet screenshots)"
Write-Host "Prepared: $zip"
