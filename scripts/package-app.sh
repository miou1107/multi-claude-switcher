#!/usr/bin/env bash
#
# Assemble "Multi-Claude Switcher.app" from the mcs-tray binary.
#
# Usage:
#   scripts/package-app.sh [VERSION] [TRAY_BINARY]
#
#   VERSION      version string baked into Info.plist (default: "dev").
#   TRAY_BINARY  prebuilt universal mcs-tray to wrap. If omitted, the script
#                builds a universal (arm64 + Intel) binary itself.
#
# Output: dist/Multi-Claude Switcher.app  and  dist/<zip> (a ditto archive).
#
# macOS only. Requires the Xcode command line tools (clang, lipo, sips,
# iconutil, ditto, codesign) and Go on PATH.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

VERSION="${1:-dev}"
TRAY_BIN="${2:-}"
APP_NAME="Multi-Claude Switcher"
DIST="dist"
APP_DIR="$DIST/$APP_NAME.app"
ICON_SRC="cmd/mcs-tray/assets/appicon-1024.png"
PLIST_TEMPLATE="packaging/Info.plist.template"

echo "==> Packaging $APP_NAME.app (version $VERSION)"

mkdir -p "$DIST"
rm -rf "$APP_DIR"

# 1. Build a universal binary unless one was supplied.
if [[ -z "$TRAY_BIN" ]]; then
	echo "==> Building universal mcs-tray"
	LDFLAGS="-X github.com/miou1107/multi-claude-switcher/core.Version=$VERSION"
	CGO_ENABLED=1 GOARCH=arm64 CC="clang -arch arm64" \
		go build -ldflags "$LDFLAGS" -o "$DIST/mcs-tray-arm64" ./cmd/mcs-tray
	CGO_ENABLED=1 GOARCH=amd64 CC="clang -arch x86_64" \
		go build -ldflags "$LDFLAGS" -o "$DIST/mcs-tray-amd64" ./cmd/mcs-tray
	lipo -create -output "$DIST/mcs-tray-universal" \
		"$DIST/mcs-tray-arm64" "$DIST/mcs-tray-amd64"
	rm -f "$DIST/mcs-tray-arm64" "$DIST/mcs-tray-amd64"
	TRAY_BIN="$DIST/mcs-tray-universal"
fi

# 2. Bundle skeleton.
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"
cp "$TRAY_BIN" "$APP_DIR/Contents/MacOS/mcs-tray"
chmod +x "$APP_DIR/Contents/MacOS/mcs-tray"

# 3. Info.plist with the version substituted.
sed "s/__VERSION__/$VERSION/g" "$PLIST_TEMPLATE" > "$APP_DIR/Contents/Info.plist"

# 4. Icon: build a .icns from the 1024 source.
ICONSET="$(mktemp -d)/icon.iconset"
mkdir -p "$ICONSET"
for pair in "16 16x16" "32 16x16@2x" "32 32x32" "64 32x32@2x" \
	"128 128x128" "256 128x128@2x" "256 256x256" "512 256x256@2x" \
	"512 512x512" "1024 512x512@2x"; do
	px="${pair%% *}"; label="${pair##* }"
	sips -z "$px" "$px" "$ICON_SRC" --out "$ICONSET/icon_${label}.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP_DIR/Contents/Resources/icon.icns"
rm -rf "$(dirname "$ICONSET")"

# 5. Ad-hoc sign the bundle. This needs no Apple Developer account and does NOT
#    notarize the app, so Gatekeeper still quarantines a browser-downloaded copy
#    (first-time users bypass it once — see the README). What it buys: after lipo
#    assembles the universal binary, the bundle gets one clean whole-bundle
#    signature with a stable identity, which keeps the self-updater's in-place
#    binary swap codesign-valid.
echo "==> Ad-hoc signing $APP_NAME.app"
codesign --force --sign - "$APP_DIR"
codesign --verify --strict "$APP_DIR"

# 6. Zip via ditto (preserves the bundle layout correctly).
ZIP="$DIST/Multi-Claude-Switcher_${VERSION}_macos.zip"
rm -f "$ZIP"
( cd "$DIST" && ditto -c -k --keepParent "$APP_NAME.app" "$(basename "$ZIP")" )

echo "==> Done:"
echo "    $APP_DIR"
echo "    $ZIP"
