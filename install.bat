@echo off
setlocal enabledelayedexpansion

set REPO=yaso09/tengiz
set RAW=https://raw.githubusercontent.com/%REPO%/main

where gh >nul 2>nul || goto check_source

echo :: Looking for latest CI run via gh...
set RUN_ID=
for /f "delims=" %%i in ('gh run list --repo %REPO% --limit 1 --json databaseId --jq .[0].databaseId') do set RUN_ID=%%i
if defined RUN_ID (
    set TMP_DIR=%TEMP%\tengiz-installer-!RUN_ID!
    if exist "!TMP_DIR!" rmdir /s /q "!TMP_DIR!"
    echo :: Downloading tengiz-installer-windows ^(run !RUN_ID!^)...
    mkdir "!TMP_DIR!"
    gh run download !RUN_ID! --repo %REPO% --name tengiz-installer-windows --dir "!TMP_DIR!" >nul 2>&1
    if not errorlevel 1 if exist "!TMP_DIR!\tengiz-installer.exe" (
        "!TMP_DIR!\tengiz-installer.exe" %*
        exit /b !errorlevel!
    )
)

:check_source
if exist "installer\install.py" (
    python installer\install.py %*
    exit /b %errorlevel%
)

where python >nul 2>nul || goto download_source

echo :: No gh or Python found. Install Python or gh CLI ^(https://cli.github.com/^)
exit /b 1

:download_source
echo :: Downloading installer source from GitHub...
set TMP=%TEMP%\tengiz-installer-%RANDOM%
mkdir "%TMP%\installer\installer" 2>nul

curl -sL "%RAW%/installer/install.py" -o "%TMP%\installer\install.py"
curl -sL "%RAW%/installer/installer/__init__.py" -o "%TMP%\installer\installer\__init__.py"
curl -sL "%RAW%/installer/installer/__main__.py" -o "%TMP%\installer\installer\__main__.py"
curl -sL "%RAW%/installer/installer/cli.py" -o "%TMP%\installer\installer\cli.py"
curl -sL "%RAW%/installer/installer/core.py" -o "%TMP%\installer\installer\core.py"
curl -sL "%RAW%/installer/installer/github.py" -o "%TMP%\installer\installer\github.py"
curl -sL "%RAW%/installer/installer/platform.py" -o "%TMP%\installer\installer\platform.py"

python "%TMP%\installer\install.py" %*