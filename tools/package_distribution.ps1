param(
    [string]$OutputRoot = 'D:\Yrus_sklad'
)

$ErrorActionPreference = 'Stop'
$version = '1.2.0'
$root = Split-Path $PSScriptRoot -Parent
$dist = Join-Path $root 'dist'
$outputBase = [IO.Path]::GetFullPath($OutputRoot).TrimEnd('\')
$ready = [IO.Path]::GetFullPath((Join-Path $outputBase "YARUS-$version-READY"))

if (-not $ready.StartsWith($outputBase + '\', [StringComparison]::OrdinalIgnoreCase) -or
    [IO.Path]::GetFileName($ready) -ne "YARUS-$version-READY") {
    throw 'Unsafe output path.'
}

$required = @(
    (Join-Path $dist "windows\YARUS-$version-Windows-x64"),
    (Join-Path $dist "windows\YARUS-$version-Windows-x64.zip"),
    (Join-Path $dist "android\YARUS-$version-RuStore.apk"),
    (Join-Path $dist "android\YARUS-$version-RuStore.aab"),
    (Join-Path $dist "YARUS-$version-RuStore-submission.zip"),
    (Join-Path $dist 'RuStore'),
    (Join-Path $dist 'YARUS-LAUNCHER.exe'),
    (Join-Path $root "docs\ИНСТРУКЦИЯ-$version.html"),
    (Join-Path $root "docs\YARUS-$version-stock-report-example.pdf")
)
foreach ($path in $required) { if (-not (Test-Path -LiteralPath $path)) { throw "Missing release artifact: $path" } }

if (Test-Path -LiteralPath $ready) {
    $existingWindows = Join-Path $ready "01-Windows\YARUS-$version-Windows-x64"
    $usedMarkers = @(
        (Join-Path $existingWindows 'data\yarus.journal'),
        (Join-Path $existingWindows 'data\yarus.key'),
        (Join-Path $existingWindows 'data\server.lock'),
        (Join-Path $existingWindows 'YARUS.exe.WebView2')
    ) | Where-Object { Test-Path -LiteralPath $_ }
    if ($usedMarkers.Count -gt 0) {
        throw "Refusing to replace $ready because it has already been used. Its warehouse data was not changed. Package to another output folder."
    }
    Remove-Item -LiteralPath $ready -Recurse -Force
}
$windows = Join-Path $ready '01-Windows'
$android = Join-Path $ready '02-Android'
$store = Join-Path $ready '03-RuStore'
$documents = Join-Path $ready '04-Документы'
$sources = Join-Path $ready '05-Исходники'
$publish = Join-Path $ready '06-Публикация'
$github = Join-Path $publish 'GitHub-Release'
$yandexStage = Join-Path $publish "YARUS-$version-для-Яндекс-Диска"
New-Item -ItemType Directory -Force -Path $windows,$android,$store,$documents,$sources,$github,$yandexStage | Out-Null

Copy-Item -LiteralPath (Join-Path $dist "windows\YARUS-$version-Windows-x64") -Destination $windows -Recurse
Copy-Item -LiteralPath (Join-Path $dist "windows\YARUS-$version-Windows-x64.zip") -Destination $windows
Copy-Item -LiteralPath (Join-Path $dist "android\YARUS-$version-RuStore.apk"),(Join-Path $dist "android\YARUS-$version-RuStore.aab"),(Join-Path $dist 'android\SHA256SUMS.txt') -Destination $android
Copy-Item -LiteralPath (Join-Path $dist 'RuStore') -Destination $store -Recurse
Copy-Item -LiteralPath (Join-Path $dist "YARUS-$version-RuStore-submission.zip") -Destination $store
Copy-Item -LiteralPath (Join-Path $dist 'YARUS-LAUNCHER.exe') -Destination (Join-Path $ready "ЗАПУСТИТЬ ЯРУС $version.exe")
Copy-Item -LiteralPath (Join-Path $dist "android\YARUS-$version-RuStore.apk") -Destination (Join-Path $ready "УСТАНОВИТЬ НА ТЕЛЕФОН ЯРУС $version.apk")
Copy-Item -LiteralPath (Join-Path $root 'docs\ОТКРОЙТЕ-СНАЧАЛА.txt') -Destination $ready

# Convenience copies beside all READY folders prevent an old Android APK or launcher
# from being selected accidentally. Used READY folders are intentionally not deleted.
Get-ChildItem -LiteralPath $outputBase -File | Where-Object {
    ($_.Name -match '^УСТАНОВИТЬ-НА-ТЕЛЕФОН-ЯРУС-.+\.apk$' -and $_.Name -ne "УСТАНОВИТЬ-НА-ТЕЛЕФОН-ЯРУС-$version.apk") -or
    ($_.Name -match '^ЗАПУСТИТЬ ЯРУС .+\.exe$' -and $_.Name -ne "ЗАПУСТИТЬ ЯРУС $version.exe") -or
    ($_.Name -match '^YARUS-.+-для-Яндекс-Диска\.zip$' -and $_.Name -ne "YARUS-$version-для-Яндекс-Диска.zip")
} | ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force }
Copy-Item -LiteralPath (Join-Path $dist "android\YARUS-$version-RuStore.apk") -Destination (Join-Path $outputBase "УСТАНОВИТЬ-НА-ТЕЛЕФОН-ЯРУС-$version.apk") -Force
Copy-Item -LiteralPath (Join-Path $dist 'YARUS-LAUNCHER.exe') -Destination (Join-Path $outputBase "ЗАПУСТИТЬ ЯРУС $version.exe") -Force

foreach ($name in @('АРХИТЕКТУРА.md','ВОССТАНОВЛЕНИЕ.md','ПРОВЕРКИ.md','release-1.2.0-tests.txt','privacy-policy.html','downloads.html',"ИНСТРУКЦИЯ-$version.html","YARUS-$version-stock-report-example.pdf")) {
    Copy-Item -LiteralPath (Join-Path $root "docs\$name") -Destination $documents
}

# Package the exact working source without ignored build caches, generated output or private keys.
$sourceFolder = Join-Path $sources "YARUS-$version-source"
New-Item -ItemType Directory -Force -Path $sourceFolder | Out-Null
$listed = & git -C $root -c core.quotepath=false ls-files --cached --others --exclude-standard
if ($LASTEXITCODE -ne 0) { throw 'Could not enumerate source files.' }
foreach ($relative in $listed) {
    if ($relative -match '(^|/)(dist|tmp|\.tooling|\.gradle|build|legacy)(/|$)' -or
        $relative -match '(?i)(signing\.properties|\.jks$|private.*key|host-state\.yarus|yarus\.journal|yarus\.key)') { continue }
    $source = Join-Path $root ($relative -replace '/', '\')
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { continue }
    $destination = Join-Path $sourceFolder ($relative -replace '/', '\')
    New-Item -ItemType Directory -Force -Path (Split-Path $destination -Parent) | Out-Null
    Copy-Item -LiteralPath $source -Destination $destination
}
if (Get-ChildItem -LiteralPath $sourceFolder -Recurse -File | Where-Object { $_.Name -match '(?i)(signing\.properties|\.jks$|private.*key)' }) {
    throw 'Private signing material was found in the source package.'
}
$sourceZip = Join-Path $sources "YARUS-$version-source.zip"
Compress-Archive -LiteralPath $sourceFolder -DestinationPath $sourceZip -CompressionLevel Optimal

# Public release files: ready for a future GitHub Release, but this script does not upload anything.
Copy-Item -LiteralPath (Join-Path $dist "windows\YARUS-$version-Windows-x64.zip") -Destination $github
Copy-Item -LiteralPath (Join-Path $dist "android\YARUS-$version-RuStore.apk") -Destination (Join-Path $github "YARUS-$version-Android.apk")
Copy-Item -LiteralPath (Join-Path $root 'rustore\03-release-notes.txt') -Destination (Join-Path $github 'RELEASE-NOTES.txt')
Copy-Item -LiteralPath (Join-Path $root 'docs\ОТКРОЙТЕ-СНАЧАЛА.txt') -Destination (Join-Path $github 'START-HERE-RU.txt')
$publicFiles = Get-ChildItem -LiteralPath $github -File | Where-Object Name -ne 'SHA256SUMS.txt'
$publicFiles | Get-FileHash -Algorithm SHA256 | Sort-Object Path | ForEach-Object {
    "$($_.Hash.ToLowerInvariant())  $([IO.Path]::GetFileName($_.Path))"
} | Set-Content -LiteralPath (Join-Path $github 'SHA256SUMS.txt') -Encoding utf8

Copy-Item -LiteralPath (Join-Path $github "YARUS-$version-Windows-x64.zip"),(Join-Path $github "YARUS-$version-Android.apk"),(Join-Path $github 'RELEASE-NOTES.txt'),(Join-Path $github 'START-HERE-RU.txt'),(Join-Path $github 'SHA256SUMS.txt') -Destination $yandexStage
$yandexZip = Join-Path $publish "YARUS-$version-для-Яндекс-Диска.zip"
Compress-Archive -LiteralPath $yandexStage -DestinationPath $yandexZip -CompressionLevel Optimal

$allHash = Join-Path $ready 'SHA256SUMS-ALL.txt'
Get-ChildItem -LiteralPath $ready -Recurse -File | Where-Object FullName -ne $allHash | Get-FileHash -Algorithm SHA256 | Sort-Object Path | ForEach-Object {
    "$($_.Hash.ToLowerInvariant())  $($_.Path.Substring($ready.Length + 1).Replace('\','/'))"
} | Set-Content -LiteralPath $allHash -Encoding utf8

Write-Host "Prepared release folder: $ready"
Write-Host "Prepared GitHub files: $github"
Write-Host "Prepared Yandex Disk ZIP: $yandexZip"
