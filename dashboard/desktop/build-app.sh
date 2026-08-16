#!/bin/bash
set -euo pipefail

# Builds the native Career Dashboard GUI (Wails v2) and installs it to
# /Applications. Re-run after changing the Go backend, frontend, or moving the
# repo (the repo path is baked into the binary via -ldflags).

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"          # dashboard/desktop
REPO="$(cd "$SCRIPT_DIR/../.." && pwd)"              # repo root
export PATH="$(go env GOPATH)/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"

if ! command -v wails >/dev/null 2>&1; then
  echo "Installing Wails CLI..."
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.14.0
fi

echo "Building Career Dashboard (this links WebKit via cgo, ~10s)..."
cd "$SCRIPT_DIR"
wails build -ldflags "-X main.defaultRepoPath=$REPO"

SRC="$SCRIPT_DIR/build/bin/career-dashboard.app"
DEST="/Applications/Career Dashboard.app"

if [ ! -d "$SRC" ]; then
  echo "Build did not produce $SRC" >&2
  exit 1
fi

echo "Installing to $DEST ..."
rm -rf "$DEST"
cp -R "$SRC" "$DEST"

echo ""
echo "Done. Launch 'Career Dashboard' from Launchpad, Spotlight, or /Applications."
echo "First launch: macOS may ask permission for it to control Terminal (used by Scan/Apply) -- click OK."
