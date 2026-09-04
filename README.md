# Outrider

Outrider runs pinned local models behind an OpenAI-compatible loopback endpoint on Apple silicon. Each profile declares the memory it needs, and Outrider refuses one the machine is too small to run rather than letting it swap.

## Scope

Outrider ships a short list of profiles, on purpose. There is a small one for answering questions about the tool itself, and a large one for real work on a machine with 32 GB or more. `outrider models` is the list; this file does not repeat it.

It is not a general GGUF runner. A profile pins a repository, a file, a quantization and the flags that were validated with them, and only those combinations are supported. Profiles are replaced when better weights arrive, so a profile that is here today can be gone in a later version.

Apple silicon only. Model weights are downloaded from their publishers and carry their own licenses, which are not this one.

## Install

Download `outrider_darwin_arm64.tar.gz` and `SHA256SUMS` from the [latest release](https://github.com/corvines/outrider/releases/latest), then verify, unpack and install for the current user:

```sh
shasum -a 256 -c SHA256SUMS
tar -xzf outrider_darwin_arm64.tar.gz
./outrider install
```

`scripts/install.sh` runs those three steps as one command. To build from source instead, `go build ./cmd/outrider` and run `./outrider install`.

This installs `~/.local/bin/outrider` without administrator privileges. Add `~/.local/bin` to `PATH` if needed. Running `install` again upgrades an Outrider-owned installation; `outrider uninstall` removes it safely, asking first whether to delete the state root (`~/Library/Caches/Outrider` by default, or `OUTRIDER_HOME`). Pass `--purge` or `--keep-state` to answer without a prompt. To replace an older unmarked Outrider binary, run `./outrider install --replace-unmanaged` explicitly.

## Run a model

```sh
outrider models
outrider pull qwen35b-mtp
outrider start
outrider use qwen35b-mtp
outrider status
```

`pull` shows download rate and ETA. `status` shows gateway health, active model, memory use, endpoint, and log path. `outrider logs` prints the latest model-server output.

Point Vera or another OpenAI-compatible client at:

```text
http://127.0.0.1:11435/v1
```

The gateway keeps that endpoint stable when `outrider use` changes models. Add `--json` to any informational command for machine-readable output.

Stop both the gateway and active model with:

```sh
outrider stop
```

Inspect interrupted downloads and quarantined cache files without changing anything:

```sh
outrider cache clean
```

Add `--apply` to remove the listed cleanup candidates. The active Gemma 4 26B partial and its resume metadata are always preserved.

## Desktop app

`Outrider.app` is a menu bar app that starts the local server for you, so nothing has to be on `PATH` first. The bundle ships the `outrider` binary inside it and runs that copy.

Building it needs the Wails v3 CLI:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
cd dashboard && wails3 task bundle
```

That writes `dist/Outrider.app`. `wails3` installs to `~/go/bin`, so add that to `PATH` if the command is not found.

The app adopts a server that is already running and leaves it alone when you quit. If it started the server itself, quitting stops it. Closing the window keeps everything running; quit from the menu bar to shut down.

### Gatekeeper

The build is signed ad hoc, not notarized, so macOS blocks the first launch with a dialog saying the app cannot be opened. To run it anyway, Control-click the app in Finder, choose Open, then confirm in the dialog. macOS remembers the choice, and later launches open normally.

## How another program finds Outrider

A harness configures itself against Outrider by reading it. Outrider writes
nothing into anyone else's configuration.

1. Ask `http://127.0.0.1:11435/v1/models`. If it answers, the gateway is up and
   there is nothing else to do.
2. If it does not answer, read the install marker at
   `~/.local/share/outrider/install.json` and take `target`. This is the step
   that works when `~/.local/bin` is not on `PATH`, which is the normal case
   directly after an install.
3. Run `outrider status --json` for the endpoint this machine actually uses.
   It is not always the default port.
4. Run `outrider models --json` for the roster. Do not hardcode profile ids.

`docs/install-marker.md` and `docs/gateway-api.md` are the normative
descriptions of those two surfaces.

## Reference

- [Gateway API](docs/gateway-api.md) - the HTTP surface other programs use.
- [Install marker](docs/install-marker.md) - where an install lands and how it
  is proved, for anything that has to find the binary itself.
