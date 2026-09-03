# Outrider

Outrider runs pinned local models behind an OpenAI-compatible loopback endpoint on Apple silicon. The current production profiles are validated on Macs with 64 GB of memory.

## Install

Build or download the `outrider` binary, then install it for the current user:

```sh
./outrider install
```

This installs `~/.local/bin/outrider` without administrator privileges. Add `~/.local/bin` to `PATH` if needed. Running `install` again upgrades an Outrider-owned installation; `outrider uninstall` removes it safely. To replace an older unmarked Outrider binary, run `./outrider install --replace-unmanaged` explicitly.

## Run a model

```sh
outrider models
outrider pull qwen35b-mtp
outrider start
outrider use qwen35b-mtp
outrider status
```

Use `gemma4-26b` instead for the official Gemma 4 26B-A4B QAT model. `pull` shows download rate and ETA. `status` shows gateway health, active model, memory use, endpoint, and log path. `outrider logs` prints the latest model-server output.

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
