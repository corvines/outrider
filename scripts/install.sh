#!/bin/sh
# Installs the Outrider app and command. Served as the target of
# `curl -fsSL <url> | sh`, so it must run under plain sh, never prompt, and
# exit non-zero on any failure.
#
# OUTRIDER_DIST_BASE overrides where the tarball and checksums are fetched
# from, which is how this is tested against a local build. The install location
# belongs to `outrider install`, which this delegates to.
set -eu

DIST_BASE="${OUTRIDER_DIST_BASE:-https://get.corvines.com/dist}"
ARCHIVE="outrider_desktop_darwin_arm64.tar.gz"

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
	app="$work/Outrider.app"
	[ -x "$app/Contents/MacOS/outrider-dashboard" ] || fail "archive does not contain the desktop app"
	cmp -s "$work/outrider" "$app/Contents/MacOS/outrider" || fail "app and CLI versions differ"
	codesign --verify --deep --strict "$app" || fail "app signature verification failed"
	applications="${OUTRIDER_APPLICATIONS_DIR:-$HOME/Applications}"
	app_target="$applications/Outrider.app"
	[ ! -e "$app_target" ] && [ ! -L "$app_target" ] ||
		fail "$app_target already exists; quit it and move it aside before installing this preview"

	chmod 755 "$work/outrider"
	"$work/outrider" version >/dev/null 2>&1 || fail "the downloaded binary does not run"

	# The binary places itself. It writes an ownership marker alongside the
	# target, and `outrider uninstall` refuses to remove a target it cannot
	# prove it owns.
	placement="$("$work/outrider" install)" || fail "outrider install failed"
	target="$(printf '%s\n' "$placement" |
		sed -n 's/.*"target"[[:space:]]*:[[:space:]]*"\(.*\)".*/\1/p' | head -1)"
	[ -n "$target" ] || fail "outrider install did not report a target"
	mkdir -p "$applications" || fail "CLI installed, but could not create $applications"
	# Stage on the destination filesystem before publishing the complete bundle.
	app_stage="$(mktemp -d "$applications/.outrider-install.XXXXXX")" ||
		fail "CLI installed, but could not stage the app"
	if ! ditto "$app" "$app_stage/Outrider.app"; then
		fail "CLI installed, but app copy failed; staging directory: $app_stage"
	fi
	[ ! -e "$app_target" ] && [ ! -L "$app_target" ] ||
		fail "another app appeared at $app_target; staging directory: $app_stage"
	mv -n "$app_stage/Outrider.app" "$app_target" || fail "could not place app from $app_stage"
	[ ! -e "$app_stage/Outrider.app" ] || fail "another app appeared at $app_target; staging directory: $app_stage"
	rmdir "$app_stage"

	# First line on stdout, so a caller reads the path rather than searching
	# PATH or parsing the message below.
	echo "outrider-install-path=$target"
	echo "installed outrider to $target"
	echo "installed Outrider.app to $app_target"
	echo "Open Outrider.app in Finder to get started. Models download separately."
	echo "This preview is ad-hoc signed, not notarized. macOS may require Open Anyway in Privacy & Security."
	echo "The app was not launched automatically. Existing model downloads were not changed."
	directory="$(dirname "$target")"
	case ":$PATH:" in
	*":$directory:"*) ;;
	*) echo "outrider: $directory is not on PATH; add it to your shell profile" >&2 ;;
	esac
}

main "$@"
