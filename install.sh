#!/usr/bin/env bash
set -euo pipefail

REPO="yaso09/tengiz"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
esac

case "$OS" in
    linux)  ASSET="tengiz-installer-linux"  ;;
    darwin) ASSET="tengiz-installer-darwin" ;;
    *)      echo "Unsupported OS: $OS"; exit 1 ;;
esac

if ! command -v gh &>/dev/null; then
    echo "gh CLI not found. Install it from https://cli.github.com/"
    exit 1
fi

echo "Looking for latest CI run..."
RUN_ID="$(gh run list --repo "$REPO" --limit 1 --json databaseId --jq '.[0].databaseId')"
if [ -z "$RUN_ID" ]; then
    echo "No CI runs found."
    exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading $ASSET (run $RUN_ID)..."
gh run download "$RUN_ID" --repo "$REPO" --name "$ASSET" --dir "$TMP"

chmod +x "$TMP/$ASSET"
exec "$TMP/$ASSET" "$@"
