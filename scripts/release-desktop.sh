#!/bin/sh
# Packages a built Apple silicon app and matching CLI for preview distribution.
set -eu

: "${OUTRIDER_APP_BUNDLE:?Set OUTRIDER_APP_BUNDLE to the built app bundle}"
: "${OUTRIDER_RELEASE_DIR:?Set OUTRIDER_RELEASE_DIR to the output directory}"

root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
app="$work/Outrider.app"
ditto "$OUTRIDER_APP_BUNDLE" "$app"
[ -x "$app/Contents/MacOS/outrider-dashboard" ]
lipo "$app/Contents/MacOS/outrider-dashboard" -verify_arch arm64
(cd "$root" && GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$work/outrider" ./cmd/outrider)
cp "$work/outrider" "$app/Contents/MacOS/outrider"
cp "$root/docs/llms.txt" "$app/Contents/Resources/llms.txt"
codesign --force --sign - "$app/Contents/MacOS/outrider"
codesign --force --deep --sign - "$app"
codesign --verify --deep --strict "$app"
cp "$app/Contents/MacOS/outrider" "$work/outrider"
cp "$root/docs/llms.txt" "$work/llms.txt"
mkdir -p "$OUTRIDER_RELEASE_DIR"
archive=outrider_desktop_darwin_arm64.tar.gz
COPYFILE_DISABLE=1 tar -czf "$OUTRIDER_RELEASE_DIR/$archive" -C "$work" outrider llms.txt Outrider.app
(cd "$OUTRIDER_RELEASE_DIR" && {
	shasum -a 256 "$archive"
	if [ -f outrider_darwin_arm64.tar.gz ]; then
		shasum -a 256 outrider_darwin_arm64.tar.gz
	fi
} > SHA256SUMS)
echo "Built preview: $OUTRIDER_RELEASE_DIR/$archive"
echo "Ad-hoc signed; not notarized."
