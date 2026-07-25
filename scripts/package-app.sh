#!/usr/bin/env bash
#
# Assemble "Multi-Claude Switcher.app" from the mcs-menubar binary (the macOS
# menu-bar panel app; the Windows build uses cmd/mcs-tray instead).
#
# Usage:
#   scripts/package-app.sh [VERSION] [MENUBAR_BINARY]
#
#   VERSION          version string baked into Info.plist (default: "dev").
#   MENUBAR_BINARY   prebuilt universal mcs-menubar to wrap. If omitted, the
#                    script builds a universal (arm64 + Intel) binary itself.
#
# Output: dist/Multi-Claude Switcher.app  and  dist/<zip> (a ditto archive).
#
# macOS only. Requires the Xcode command line tools (clang, lipo, sips,
# iconutil, ditto, codesign) and Go on PATH.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

VERSION="${1:-dev}"
BIN="${2:-}"
APP_NAME="Multi-Claude Switcher"
DIST="dist"
APP_DIR="$DIST/$APP_NAME.app"
ICON_SRC="cmd/mcs-tray/assets/appicon-1024.png"
PLIST_TEMPLATE="packaging/Info.plist.template"

echo "==> Packaging $APP_NAME.app (version $VERSION)"

mkdir -p "$DIST"
rm -rf "$APP_DIR"

# 1. Build a universal mcs-menubar unless one was supplied. It uses Cocoa/WebKit
#    (CGO), so each arch is built with the matching clang.
if [[ -z "$BIN" ]]; then
	echo "==> Building universal mcs-menubar"
	LDFLAGS="-X github.com/miou1107/multi-claude-switcher/core.Version=$VERSION"
	CGO_ENABLED=1 GOARCH=arm64 CC="clang -arch arm64" \
		go build -ldflags "$LDFLAGS" -o "$DIST/mcs-menubar-arm64" ./cmd/mcs-menubar
	CGO_ENABLED=1 GOARCH=amd64 CC="clang -arch x86_64" \
		go build -ldflags "$LDFLAGS" -o "$DIST/mcs-menubar-amd64" ./cmd/mcs-menubar
	lipo -create -output "$DIST/mcs-menubar-universal" \
		"$DIST/mcs-menubar-arm64" "$DIST/mcs-menubar-amd64"
	rm -f "$DIST/mcs-menubar-arm64" "$DIST/mcs-menubar-amd64"
	BIN="$DIST/mcs-menubar-universal"
fi

# 2. Bundle skeleton.
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"
cp "$BIN" "$APP_DIR/Contents/MacOS/mcs-menubar"
chmod +x "$APP_DIR/Contents/MacOS/mcs-menubar"

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

# 5. Ad-hoc sign the bundle. No Apple Developer account, no notarization, so
#    Gatekeeper still quarantines a browser-downloaded copy (bypassed once — see
#    the README). The stable ad-hoc signature keeps the self-updater's in-place
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
