param(
    [string]$SigningDirectory = (Join-Path (Split-Path $PSScriptRoot -Parent) 'yarus-private-signing')
)

$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$android = Join-Path $root 'android'
$secretsPath = Join-Path $SigningDirectory 'signing.properties'
$keystorePath = Join-Path $SigningDirectory 'YARUS-release.jks'
$jbr = 'D:\ПРОГРАММЫ\Android studio\jbr'
$sdk = Join-Path $env:LOCALAPPDATA 'Android\Sdk'

if (-not (Test-Path -LiteralPath $secretsPath)) { throw "Нет файла подписи: $secretsPath" }
if (-not (Test-Path -LiteralPath $keystorePath)) { throw "Нет ключа подписи: $keystorePath" }
if (-not (Test-Path -LiteralPath (Join-Path $jbr 'bin\java.exe'))) { throw 'Не найдена Java из Android Studio.' }

$secrets = @{}
foreach ($line in Get-Content -LiteralPath $secretsPath -Encoding UTF8) {
    if ($line -and -not $line.TrimStart().StartsWith('#')) {
        $pair = $line.Split('=', 2)
        if ($pair.Count -eq 2) { $secrets[$pair[0].Trim()] = $pair[1].Trim() }
    }
}
foreach ($name in 'storePassword','keyAlias','keyPassword') {
    if (-not $secrets[$name]) { throw "В signing.properties отсутствует $name" }
}

& python (Join-Path $root 'tools\pack_web.py')
if ($LASTEXITCODE -ne 0) { throw 'Не удалось упаковать web-интерфейс.' }

$env:JAVA_HOME = $jbr
$env:YARUS_KEYSTORE_FILE = $keystorePath
$env:YARUS_KEYSTORE_PASSWORD = $secrets.storePassword
$env:YARUS_KEY_ALIAS = $secrets.keyAlias
$env:YARUS_KEY_PASSWORD = $secrets.keyPassword

try {
    Push-Location $android
    & .\gradlew.bat --offline --no-daemon clean assembleRelease bundleRelease
    if ($LASTEXITCODE -ne 0) { throw 'Android release build failed.' }
} finally {
    Pop-Location
    Remove-Item Env:YARUS_KEYSTORE_FILE -ErrorAction SilentlyContinue
    Remove-Item Env:YARUS_KEYSTORE_PASSWORD -ErrorAction SilentlyContinue
    Remove-Item Env:YARUS_KEY_ALIAS -ErrorAction SilentlyContinue
    Remove-Item Env:YARUS_KEY_PASSWORD -ErrorAction SilentlyContinue
}

$output = Join-Path $root 'dist\android'
New-Item -ItemType Directory -Force -Path $output | Out-Null
$apk = Join-Path $output 'YARUS-1.0.0-RuStore.apk'
$aab = Join-Path $output 'YARUS-1.0.0-RuStore.aab'
$certificate = Join-Path $output 'YARUS-upload-certificate.pem'
Copy-Item -LiteralPath (Join-Path $android 'app\build\outputs\apk\release\app-release.apk') -Destination $apk -Force
Copy-Item -LiteralPath (Join-Path $android 'app\build\outputs\bundle\release\app-release.aab') -Destination $aab -Force

$apksigner = Join-Path $sdk 'build-tools\36.1.0\apksigner.bat'
& $apksigner verify --verbose --print-certs $apk
if ($LASTEXITCODE -ne 0) { throw 'APK signature verification failed.' }
& (Join-Path $jbr 'bin\jarsigner.exe') -verify $aab
if ($LASTEXITCODE -ne 0) { throw 'AAB signature verification failed.' }
& (Join-Path $jbr 'bin\keytool.exe') -exportcert -rfc -alias $secrets.keyAlias -keystore $keystorePath -storepass $secrets.storePassword -file $certificate
if ($LASTEXITCODE -ne 0) { throw 'Public upload certificate export failed.' }
$certificateText = Get-Content -LiteralPath $certificate -Raw -Encoding ascii
if ($certificateText -notmatch 'BEGIN CERTIFICATE' -or $certificateText -match 'PRIVATE KEY') {
    throw 'Exported certificate is not a safe public PEM certificate.'
}

Get-FileHash -Algorithm SHA256 $apk,$aab |
    ForEach-Object { "$($_.Hash.ToLowerInvariant())  $([IO.Path]::GetFileName($_.Path))" } |
    Set-Content -LiteralPath (Join-Path $output 'SHA256SUMS.txt') -Encoding ascii

Write-Host "Built and verified: $apk"
Write-Host "Built and verified: $aab"
Write-Host "Exported public certificate: $certificate"
