#!/bin/sh
# Builds the release tarball and checksum file that scripts/install.sh fetches.
#
# Output lands in dist/ as outrider_darwin_arm64.tar.gz and SHA256SUMS, which
# is the layout OUTRIDER_DIST_BASE points at.
set -eu

OUT_DIR="${OUTRIDER_RELEASE_DIR:-dist}"
ARCHIVE="outrider_darwin_arm64.tar.gz"

fail() {
	echo "release: $1" >&2
	exit 1
}

main() {
	command -v go >/dev/null 2>&1 || fail "requires go"
	command -v shasum >/dev/null 2>&1 || fail "requires shasum"

	root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
	cd "$root"

	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT

	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$work/outrider" ./cmd/outrider ||
		fail "build failed"

	mkdir -p "$OUT_DIR"
	tar -czf "$OUT_DIR/$ARCHIVE" -C "$work" outrider || fail "could not write $OUT_DIR/$ARCHIVE"
	(cd "$OUT_DIR" && shasum -a 256 "$ARCHIVE" > SHA256SUMS) || fail "could not write $OUT_DIR/SHA256SUMS"

	echo "built $OUT_DIR/$ARCHIVE"
	cat "$OUT_DIR/SHA256SUMS"
}

main "$@"
