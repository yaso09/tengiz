@echo off
setlocal enabledelayedexpansion

set REPO=yaso09/tengiz
set ASSET=tengiz-installer-windows.exe

echo :: Looking for latest CI run...
for /f "usebackq delims=" %%i in (`gh run list --repo %REPO% --limit 1 --json databaseId --jq ".[0].databaseId"`) do set RUN_ID=%%i
if "%RUN_ID%"=="" (
    echo :: No CI runs found.
    exit /b 1
)

set TMP=%TEMP%\tengiz-installer-%RUN_ID%
mkdir "%TMP%" 2>nul

echo :: Downloading %ASSET% (run %RUN_ID%)...
gh run download %RUN_ID% --repo %REPO% --name %ASSET% --dir "%TMP%"
if errorlevel 1 (
    echo :: Download failed. Try "gh auth login" first.
    exit /b 1
)

"%TMP%\%ASSET%" %*
