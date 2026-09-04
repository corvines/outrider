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

`scripts/install.sh` does those three steps in one command.

The binary lands in `~/.local/bin/outrider`, so no administrator privileges are
needed. Add `~/.local/bin` to `PATH` if it isn't there already.

Running `install` again upgrades an installation Outrider owns. To replace an
older binary it has no marker for, pass `--replace-unmanaged`.

`outrider uninstall` removes it and asks whether to delete the state root,
which is `~/Library/Caches/Outrider` unless `OUTRIDER_HOME` says otherwise.
Pass `--purge` or `--keep-state` to answer without the prompt.
