#!/usr/bin/env bash
set -euo pipefail

REPO="yaso09/tengiz"
RAW="https://raw.githubusercontent.com/$REPO/main"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
esac

case "$OS" in
    linux)  ASSET="tengiz-installer-linux";  BINARY="tengiz-installer"  ;;
    darwin) ASSET="tengiz-installer-darwin"; BINARY="tengiz-installer"  ;;
    *)      echo "Unsupported OS: $OS"; exit 1 ;;
esac

if command -v gh &>/dev/null; then
    echo "Looking for latest CI run via gh..."
    RUN_ID="$(gh run list --repo "$REPO" --limit 1 --json databaseId --jq '.[0].databaseId')"
    if [ -n "$RUN_ID" ]; then
        TMP="$(mktemp -d)"
        trap 'rm -rf "$TMP"' EXIT
        echo "Downloading $ASSET (run $RUN_ID)..."
        gh run download "$RUN_ID" --repo "$REPO" --name "$ASSET" --dir "$TMP"
        chmod +x "$TMP/$ASSET/$BINARY"
        exec "$TMP/$ASSET/$BINARY" "$@"
    fi
    echo "No CI runs found, falling back to source..."
fi

if [ -f "installer/install.py" ]; then
    echo "Running from local source..."
    exec python3 installer/install.py "$@"
fi

if command -v python3 &>/dev/null; then
    PY="python3"
elif command -v python &>/dev/null; then
    PY="python"
else
    echo "Python not found. Install it or gh CLI (https://cli.github.com/)."
    exit 1
fi

echo "Downloading installer source from GitHub..."
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$TMP/installer/installer"
curl -sL "$RAW/installer/install.py" -o "$TMP/installer/install.py"
for f in __init__.py __main__.py cli.py core.py github.py platform.py; do
    curl -sL "$RAW/installer/installer/$f" -o "$TMP/installer/installer/$f"
done

exec "$PY" "$TMP/installer/install.py" "$@"
