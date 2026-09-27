param([string]$JavaHome = '')

$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
if (-not $JavaHome) { $JavaHome = $env:JAVA_HOME }
if (-not $JavaHome) { $JavaHome = 'D:\ПРОГРАММЫ\Android studio\jbr' }
$javac = Join-Path $JavaHome 'bin\javac.exe'
$java = Join-Path $JavaHome 'bin\java.exe'
if (-not (Test-Path -LiteralPath $javac) -or -not (Test-Path -LiteralPath $java)) {
    throw 'Не найдена Java 17 для проверки Android-сканера.'
}

$classes = Join-Path $root 'android\build\policy-self-test'
New-Item -ItemType Directory -Force -Path $classes | Out-Null
$policy = Join-Path $root 'android\app\src\main\java\ru\viktortown\yarus\BarcodeScanPolicy.java'
$selfTest = Join-Path $root 'android\policy-test\ru\viktortown\yarus\BarcodeScanPolicySelfTest.java'
& $javac -encoding UTF-8 -d $classes $policy $selfTest
if ($LASTEXITCODE -ne 0) { throw 'Не удалось скомпилировать правила Android-сканера.' }
& $java -ea -cp $classes ru.viktortown.yarus.BarcodeScanPolicySelfTest
if ($LASTEXITCODE -ne 0) { throw 'Проверка правил Android-сканера не пройдена.' }
