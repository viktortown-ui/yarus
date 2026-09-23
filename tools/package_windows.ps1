$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$dist = Join-Path $root 'dist'
$folder = Join-Path $dist 'windows\YARUS-1.2.0-Windows-x64'
$zip = Join-Path $dist 'windows\YARUS-1.2.0-Windows-x64.zip'
$output = Join-Path $dist 'windows'

New-Item -ItemType Directory -Force -Path $output | Out-Null
Get-ChildItem -LiteralPath $output -File | Where-Object {
    $_.Name -match '^YARUS-.+-Windows-x64\.zip$' -and $_.FullName -ne $zip
} | ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force }
Get-ChildItem -LiteralPath $output -Directory | Where-Object {
    $_.Name -match '^YARUS-.+-Windows-x64$' -and $_.FullName -ne $folder
} | ForEach-Object {
    $used = @(
        (Join-Path $_.FullName 'data\yarus.journal'),
        (Join-Path $_.FullName 'data\yarus.key'),
        (Join-Path $_.FullName 'YARUS.exe.WebView2')
    ) | Where-Object { Test-Path -LiteralPath $_ }
    if ($used.Count -eq 0) { Remove-Item -LiteralPath $_.FullName -Recurse -Force }
    else { Write-Warning "Старая папка $($_.Name) использовалась и сохранена вместе с данными." }
}

& (Join-Path $PSScriptRoot 'build_windows.ps1')
if ($LASTEXITCODE -ne 0) { throw 'Windows build failed.' }

if (Test-Path -LiteralPath $folder) { Remove-Item -LiteralPath $folder -Recurse -Force }
New-Item -ItemType Directory -Force -Path (Join-Path $folder 'data'),(Join-Path $folder 'licenses') | Out-Null

Copy-Item -LiteralPath (Join-Path $dist 'YARUS.exe') -Destination $folder
Copy-Item -LiteralPath (Join-Path $dist 'YARUS-server.exe') -Destination $folder
Copy-Item -LiteralPath (Join-Path $dist 'WebView2Loader.dll') -Destination $folder
Copy-Item -LiteralPath (Join-Path $root 'docs\ИНСТРУКЦИЯ-1.2.0.html') -Destination (Join-Path $folder 'ИНСТРУКЦИЯ.html')
Copy-Item -LiteralPath (Join-Path $root 'docs\YARUS-1.2.0-stock-report-example.pdf') -Destination (Join-Path $folder 'ПРИМЕР-ОТЧЕТА-ОСТАТКОВ.pdf')
Copy-Item -LiteralPath (Join-Path $root 'THIRD_PARTY_NOTICES.txt') -Destination $folder
Copy-Item -LiteralPath (Join-Path $root 'docs\licenses\Microsoft.WebView2.LICENSE.txt') -Destination (Join-Path $folder 'licenses')
Copy-Item -LiteralPath (Join-Path $root 'docs\licenses\Microsoft.WebView2.NOTICE.txt') -Destination (Join-Path $folder 'licenses')
Copy-Item -LiteralPath (Join-Path $root 'docs\DATA-README.txt') -Destination (Join-Path $folder 'data\README-НЕ-УДАЛЯТЬ.txt')

[IO.File]::WriteAllText((Join-Path $folder 'YARUS-PORTABLE.flag'), "ЯРУС хранит данные внутри папки data.`r`n", [Text.UTF8Encoding]::new($false))

$hashes = Get-FileHash -Algorithm SHA256 (Join-Path $folder 'YARUS.exe'),(Join-Path $folder 'YARUS-server.exe'),(Join-Path $folder 'WebView2Loader.dll')
$hashes | ForEach-Object { "$($_.Hash.ToLowerInvariant())  $([IO.Path]::GetFileName($_.Path))" } |
    Set-Content -LiteralPath (Join-Path $folder 'SHA256SUMS.txt') -Encoding ascii

if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip -Force }
Compress-Archive -LiteralPath $folder -DestinationPath $zip -CompressionLevel Optimal
Write-Host "Packaged: $zip"
