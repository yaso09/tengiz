@echo off
setlocal enabledelayedexpansion

set REPO=yaso09/tengiz
set ASSET=tengiz-installer-windows
set BINARY=tengiz-installer.exe

where gh >nul 2>nul
if %errorlevel% equ 0 goto use_gh

goto use_api

:use_gh
echo :: Looking for latest CI run via gh...
for /f "usebackq delims=" %%i in (`gh run list --repo %REPO% --limit 1 --json databaseId --jq ".[0].databaseId"`) do set RUN_ID=%%i
if "%RUN_ID%"=="" (
    echo :: No CI runs found.
    exit /b 1
)
echo :: Downloading %BINARY% (run %RUN_ID%)...
gh run download %RUN_ID% --repo %REPO% --name %ASSET% --dir "%TEMP%\tengiz-installer-%RUN_ID%"
if errorlevel 1 (
    echo :: gh download failed.
    exit /b 1
)
"%TEMP%\tengiz-installer-%RUN_ID%\%BINARY%" %*
exit /b %errorlevel%

:use_api
echo :: gh not found, falling back to API...
set TOKEN=%GH_TOKEN%
if "%TOKEN%"=="" set TOKEN=%GITHUB_TOKEN%
if "%TOKEN%"=="" (
    echo :: Set GH_TOKEN or GITHUB_TOKEN to download without gh.
    exit /b 1
)

set TMP=%TEMP%\tengiz-installer-%RANDOM%
mkdir "%TMP%" 2>nul

echo :: Looking for artifact...
powershell -NoProfile -Command ^
    "$r = Invoke-RestMethod -Uri 'https://api.github.com/repos/%REPO%/actions/artifacts?per_page=50' -Headers @{Accept='application/vnd.github.v3+json'; Authorization='token %TOKEN%'};" ^
    "$m = $r.artifacts | Where-Object { $_.name -eq '%ASSET%' -and -not $_.expired } | Select-Object -First 1;" ^
    "if ($m) { Write-Output $m.id } else { Write-Output '' }" > "%TMP%\artifact_id.txt" 2>nul
set /p ARTIFACT_ID=<"%TMP%\artifact_id.txt"
if "%ARTIFACT_ID%"=="" (
    echo :: No matching artifact (%ASSET%) found.
    exit /b 1
)

echo :: Downloading artifact %ARTIFACT_ID%...
powershell -NoProfile -Command ^
    "Invoke-WebRequest -Uri 'https://api.github.com/repos/%REPO%/actions/artifacts/%ARTIFACT_ID%/zip' -Headers @{Accept='application/vnd.github.v3+json'; Authorization='token %TOKEN%'} -OutFile '%TMP%\artifact.zip'" ^
    >nul 2>&1
if errorlevel 1 (
    echo :: API download failed.
    exit /b 1
)

powershell -NoProfile -Command ^
    "Expand-Archive -Path '%TMP%\artifact.zip' -DestinationPath '%TMP%' -Force" ^
    >nul 2>&1

"%TMP%\%BINARY%" %*
