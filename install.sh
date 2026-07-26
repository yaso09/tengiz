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
    linux)
        ASSET="tengiz-installer-linux"
        BINARY="tengiz-installer"
        ;;
    darwin)
        ASSET="tengiz-installer-darwin"
        BINARY="tengiz-installer"
        ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if command -v gh &>/dev/null; then
    echo "Looking for latest CI run via gh..."
    RUN_ID="$(gh run list --repo "$REPO" --limit 1 --json databaseId --jq '.[0].databaseId')"
    if [ -z "$RUN_ID" ]; then
        echo "No CI runs found."
        exit 1
    fi
    echo "Downloading $ASSET (run $RUN_ID)..."
    gh run download "$RUN_ID" --repo "$REPO" --name "$ASSET" --dir "$TMP"
    chmod +x "$TMP/$ASSET/$BINARY"
    exec "$TMP/$ASSET/$BINARY" "$@"
fi

echo "gh not found, falling back to API..."
TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
if [ -z "$TOKEN" ]; then
    echo "Set GH_TOKEN or GITHUB_TOKEN to download without gh."
    echo "Install gh from https://cli.github.com/"
    exit 1
fi

RESP="$(curl -s -H "Accept: application/vnd.github.v3+json" \
    -H "Authorization: token $TOKEN" \
    "https://api.github.com/repos/$REPO/actions/artifacts?per_page=50")"

ARTIFACT_ID="$(python3 -c "
import sys, json
data = json.loads(sys.stdin.read())
for a in data.get('artifacts', []):
    if a['name'] == '$ASSET' and not a['expired']:
        print(a['id'])
        break
" <<< "$RESP")"

if [ -z "$ARTIFACT_ID" ]; then
    echo "No matching artifact ($ASSET) found."
    exit 1
fi

ZIP="$TMP/artifact.zip"
echo "Downloading artifact $ARTIFACT_ID..."
curl -sL -H "Accept: application/vnd.github.v3+json" \
    -H "Authorization: token $TOKEN" \
    "https://api.github.com/repos/$REPO/actions/artifacts/$ARTIFACT_ID/zip" \
    -o "$ZIP"

unzip -o "$ZIP" -d "$TMP" >/dev/null 2>&1
chmod +x "$TMP/$BINARY"
exec "$TMP/$BINARY" "$@"
