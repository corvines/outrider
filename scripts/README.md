# Release packaging

`release.sh` builds the standalone CLI archive. `release-desktop.sh` packages a
built Apple silicon app with a matching CLI. The desktop packager replaces the
bundled CLI with a fresh build and ad-hoc signs the bundle. It does not notarize it.

```sh
OUTRIDER_APP_BUNDLE=/absolute/path/Outrider.app \
OUTRIDER_RELEASE_DIR=/absolute/path/upload/dist \
sh scripts/release-desktop.sh
```

Copy `scripts/install.sh` to `upload/install.sh`. Publish the desktop archive
and `SHA256SUMS` under `/dist/` before publishing `/install.sh`. When replacing
cached files, invalidate `/dist/SHA256SUMS` and `/install.sh` in the CDN.
Keep the published standalone CLI archive in the output directory when building
the desktop release; its checksum is included for cached CLI-only installers.

The installer downloads `outrider_desktop_darwin_arm64.tar.gz`, verifies its
checksum and app signature, and installs the CLI through `outrider install`.
It places the app in `~/Applications/Outrider.app`, without launching it or
downloading a model. `OUTRIDER_APPLICATIONS_DIR` overrides the app destination.
It refuses to replace an existing app: quit the app and move the old bundle
aside before rerunning. The CLI installer retains its ownership checks.

This preview uses ad-hoc signing. macOS may require manual approval through
Privacy & Security on first launch. The installer does not remove quarantine
attributes or disable system security checks. CLI uninstall does not remove
the desktop bundle; move the app to Trash separately.

Run `go test ./scripts` for installer behavior tests. These use stub platform
and signing commands; they do not establish Gatekeeper acceptance.
