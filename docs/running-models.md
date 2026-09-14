# Running models

```sh
outrider models
outrider pull qwen35b-mtp
outrider start
outrider use qwen35b-mtp
outrider status
```

`pull` shows download rate and ETA. `status` shows gateway health, active
model, memory use, endpoint and log path. `outrider logs` prints the latest
model-server output. `--json` works on any informational command.

Clients connect to `http://127.0.0.1:11435/v1`, and that address stays the same
when `outrider use` switches models.

`outrider stop` stops the gateway and the active model.

## Cache

`outrider cache clean` lists interrupted downloads and quarantined files
without touching anything. Add `--apply` to remove what it listed. Partial
downloads and their resume metadata are kept for every profile, so an
interrupted `pull` can still resume.

## Download smoke check

The development profile `download-test` downloads a 1.19 MB TinyStories GGUF.
It exercises the profile downloader and checksum verification. It is not a
chat assistant. Enable development profiles when running the command:

```sh
OUTRIDER_DEV=1 outrider pull download-test
```

Run it again to check cache reuse. The first pull also installs the pinned
runtime if it is absent.
