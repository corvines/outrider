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

### Controlled download failures

These environment variables affect only `download-test`, only with
`OUTRIDER_DEV=1`. Other models and the runtime download are unaffected.

| Variable | Behavior |
| --- | --- |
| `OUTRIDER_DOWNLOAD_BPS=20480` | Limit body delivery to 20 KiB/s, about a minute for the tiny model. |
| `OUTRIDER_DOWNLOAD_DROP_AFTER=262144` | Interrupt the first successful response after 256 KiB, then allow retries to resume. |
| `OUTRIDER_DOWNLOAD_503_ATTEMPTS=2` | Inject two HTTP 503 responses before allowing the real request. Use `3` to exhaust the downloader's retries. |

Values are nonnegative integers; zero disables a control. Failure counts reset
for each download invocation. A cached model skips downloading and these
controls. Use a fresh `OUTRIDER_HOME` for a new transfer, or remove the test
model through the app.

```sh
OUTRIDER_DEV=1 OUTRIDER_DOWNLOAD_BPS=20480 \
  OUTRIDER_DOWNLOAD_DROP_AFTER=262144 \
  go run ./cmd/outrider pull download-test
```

For cancellation, omit the drop control, interrupt the CLI with Ctrl-C after
progress appears, then repeat the command to resume. In desktop UAT, launch
the rebuilt app executable with these variables and use its pause/retry
controls. The gateway must be started with the same environment; an already
running gateway retains its original settings.

Automated coverage uses a local HTTP server and requires no model download:

```sh
go test -race ./internal/llama -run TestDevelopmentDownload
```

It checks throttling, cancellation with retained partial bytes, range resume,
byte-for-byte recovery after a dropped connection, temporary 503 recovery,
retry exhaustion and a later successful retry, and isolation from normal
model downloads. These checks cover the downloader; desktop interaction and
real model inference require separate acceptance checks.
