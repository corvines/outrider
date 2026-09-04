#!/bin/sh
# Installs the outrider command. Served as the target of
# `curl -fsSL <url> | sh`, so it must run under plain sh, never prompt, and
# exit non-zero on any failure.
#
# OUTRIDER_DIST_BASE overrides where the tarball and checksums are fetched
# from, which is how this is tested against a local build. The install location
# belongs to `outrider install`, which this delegates to.
set -eu

DIST_BASE="${OUTRIDER_DIST_BASE:-https://github.com/corvines/outrider/releases/latest/download}"
ARCHIVE="outrider_darwin_arm64.tar.gz"

fail() {
	echo "outrider: $1" >&2
	exit 1
}

main() {
	[ "$(uname -s)" = "Darwin" ] || fail "requires macOS"
	[ "$(uname -m)" = "arm64" ] || fail "requires Apple Silicon"
	command -v curl >/dev/null 2>&1 || fail "requires curl"
	command -v shasum >/dev/null 2>&1 || fail "requires shasum"

	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT

	curl -fsSL "$DIST_BASE/$ARCHIVE" -o "$work/$ARCHIVE" ||
		fail "could not download $DIST_BASE/$ARCHIVE"
	curl -fsSL "$DIST_BASE/SHA256SUMS" -o "$work/SHA256SUMS" ||
		fail "could not download $DIST_BASE/SHA256SUMS"

	expected="$(grep " $ARCHIVE\$" "$work/SHA256SUMS" | cut -d' ' -f1)"
	[ -n "$expected" ] || fail "SHA256SUMS has no entry for $ARCHIVE"
	actual="$(shasum -a 256 "$work/$ARCHIVE" | cut -d' ' -f1)"
	[ "$expected" = "$actual" ] || fail "checksum mismatch for $ARCHIVE"

	tar -xzf "$work/$ARCHIVE" -C "$work" || fail "could not extract $ARCHIVE"
	[ -f "$work/outrider" ] || fail "$ARCHIVE does not contain outrider"

	chmod 755 "$work/outrider"
	"$work/outrider" version >/dev/null 2>&1 || fail "the downloaded binary does not run"

	# The binary places itself. It writes an ownership marker alongside the
	# target, and `outrider uninstall` refuses to remove a target it cannot
	# prove it owns.
	placement="$("$work/outrider" install)" || fail "outrider install failed"
	target="$(printf '%s\n' "$placement" |
		sed -n 's/.*"target"[[:space:]]*:[[:space:]]*"\(.*\)".*/\1/p' | head -1)"
	[ -n "$target" ] || fail "outrider install did not report a target"

	# First line on stdout, so a caller reads the path rather than searching
	# PATH or parsing the message below.
	echo "outrider-install-path=$target"
	echo "installed outrider to $target"
	directory="$(dirname "$target")"
	case ":$PATH:" in
	*":$directory:"*) ;;
	*) echo "outrider: $directory is not on PATH; add it to your shell profile" >&2 ;;
	esac
}

main "$@"
