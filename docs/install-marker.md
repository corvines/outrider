# Install marker

Outrider records what it installed in a small JSON file beside the binary. The
marker is how `outrider uninstall` proves it owns the thing it is about to
remove, and how another program finds a binary that is not on `PATH`.

It is a stable contract. Its location and its field names may change only
deliberately.

## Where it lives

There are two install routes, and each writes its own marker.

| route | binary | marker |
|---|---|---|
| `outrider install` | `~/.local/bin/outrider` | `~/.local/share/outrider/install.json` |
| signed PKG | `/usr/local/bin/outrider` | `/usr/local/share/outrider/install.json` |

The per-user route needs no privileges and is what the bootstrap script uses.
The system route is the packager's, because a PKG installer runs privileged.
The two are independent: a machine can carry both.

## What it holds

```json
{
  "schema": 1,
  "target": "/Users/x/.local/bin/outrider",
  "sha256": "5f2c..."
}
```

| field | meaning |
|---|---|
| `schema` | marker format version, `1` today |
| `target` | absolute path of the installed binary |
| `sha256` | digest of the installed copy |
| `link` | destination the target points at, for a symlink install |

`sha256` and `link` identify the install two different ways, and exactly one of
them is set. A normal install copies the binary and records its digest. A
development install (`outrider install --link`) symlinks instead and records
the destination, because a digest would go stale the moment the linked binary
is rebuilt.

## Reading it

A program that wants the install path should prefer `outrider install --json`,
which returns the same values without reading the file:

```json
{ "status": "installed", "target": "...", "marker": "...", "sha256": "..." }
```

Read the file directly only when the binary is what you are trying to find, so
you cannot run it yet. That is the case the marker exists for: the install
succeeded, `~/.local/bin` is not on this machine's `PATH`, and `target` is the
only thing that says where the binary went.

## What it costs to be wrong

`uninstall` refuses to remove anything it cannot match against the marker: a
missing marker beside a present binary, a `target` pointing somewhere else, a
digest that does not match, or a symlink pointing at a different destination.
Each is refused rather than guessed at, so a hand-placed binary is never
removed by Outrider.

The failure this prevents is real and was hit once: an early bootstrap script
placed the binary itself and wrote no marker, so install worked, the binary ran,
and uninstall refused. The script no longer places anything. It downloads,
verifies the checksum, and runs `outrider install`, which owns both the target
and the marker.
