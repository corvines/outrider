# Install

Build it and install it for the current user:

```sh
go build ./cmd/outrider
./outrider install
```

Released builds are on the
[releases page](https://github.com/corvines/outrider/releases) as
`outrider_darwin_arm64.tar.gz`, with a `SHA256SUMS` alongside:

```sh
shasum -a 256 -c SHA256SUMS
tar -xzf outrider_darwin_arm64.tar.gz
./outrider install
```

For the desktop preview, run:

```sh
curl -fsSL https://get.corvines.com/install.sh | sh
```

`scripts/install.sh` verifies and installs the desktop archive plus the CLI.
The app lands in `~/Applications/Outrider.app`. Open it in Finder; models
download only after consent. The preview installer refuses to replace an
existing app: quit it and move it aside before reinstalling. Keep downloaded
models when asked during uninstall to avoid downloading them again.

The binary lands in `~/.local/bin/outrider`, so no administrator privileges are
needed. Add `~/.local/bin` to `PATH` if it isn't there already.

Running `install` again upgrades an installation Outrider owns. To replace an
older binary it has no marker for, pass `--replace-unmanaged`.

`outrider uninstall` removes it and asks whether to delete the state root,
which is `~/Library/Caches/Outrider` unless `OUTRIDER_HOME` says otherwise.
Pass `--purge` or `--keep-state` to answer without the prompt.
