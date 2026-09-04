# Gateway API

The gateway listens on `127.0.0.1:11435` and speaks an OpenAI-compatible
subset. It is the only surface other programs are expected to use. The
`/admin/*` routes back the bundled dashboard and are not a stable contract.

Stable routes:

| method | path | purpose |
|---|---|---|
| GET | `/health` | liveness and the running build |
| GET | `/v1/models` | model catalog, see below |
| POST | `/v1/chat/completions` | proxied to the loaded model |
| POST | `/v1/completions` | proxied to the loaded model |
| POST | `/v1/embeddings` | proxied to the loaded model |
| POST | `/v1/responses` | proxied to the loaded model |

Requesting a model that is not in the catalog returns HTTP 404.

## GET /health

```json
{ "status": "ok", "version": "1.2.3", "commit": "abc123" }
```

`version` and `commit` identify the binary answering. The catalog is compiled
into the binary, so a gateway started before an upgrade keeps serving the
catalog it was built with. A client that installed a newer binary compares
these against `outrider version` and restarts the gateway when they differ.
Both are omitted by an embedder that did not set them.

## GET /v1/models

Standard OpenAI list shape, plus the facts a client needs to pick a model:
what it can do, what it costs to run, and whether it can serve a request now
or has to be downloaded first.

The four keys the OpenAI spec requires (`id`, `object`, `owned_by`, and
`created` where present) keep their spec meaning. Everything else is an added
key, and a client that does not recognise one ignores it. There is no second
route: a caller never has to shell out to the binary to learn any of this.

```json
{
  "object": "list",
  "data": [
    {
      "id": "qwen35b-mtp",
      "object": "model",
      "owned_by": "outrider",
      "description": "Qwen3.6 35B-A3B MTP primary local agent",
      "capabilities": ["completion", "speculation"],
      "quantization": "UD-Q4_K_M",
      "meta": { "n_ctx": 32768, "n_ctx_train": 131072 },
      "repo": "unsloth/Qwen3.6-35B-A3B-MTP-GGUF",
      "file": "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf",
      "min_memory_bytes": 34359738368,
      "speculation": "mtp",
      "weights": "mismatched",
      "size_bytes": 22663387424,
      "on_disk_bytes": 9126805504
    }
  ]
}
```

### capabilities

What the model can do. Always sent, as a list, so an absent key is never
mistaken for an unknown answer.

| value | meaning |
|---|---|
| `completion` | answers chat and completion requests. Every model has it. |
| `vision` | accepts images, because the profile ships a multimodal projector |
| `speculation` | draws candidate tokens from a draft path, see `speculation` |

Reported only where the profile proves it. Tool calling and thinking are not
inferable from a profile and are absent until a profile declares them.

### repo and file

Where the weights came from: the Hugging Face repository and the file within
it. Omitted for a profile that names a local path instead.

### min_memory_bytes

The physical memory the profile has been validated at. A machine below this
is refused before any download starts, so a client can present the model as
out of reach rather than offering a multi-gigabyte fetch that will not run.

### speculation

The draft-token mode in force, for example `mtp`. Omitted when the model
decodes one token at a time.

### weights

Whether the model's weights are on disk and usable.

| value | meaning |
|---|---|
| `present` | on disk at the declared size, ready to serve |
| `mismatched` | a file exists at the path but its size is not the declared size |
| `missing` | no file at the path |

Determined by one `stat` against the declared size. It is a size comparison,
not a checksum, so `present` means "plausible and ready to load", not
"verified byte for byte".

### size_bytes

The declared download size from the profile manifest. Always present, and
correct when the file is absent, which is when a caller needs it. It is not
the size of whatever currently sits on disk.

### on_disk_bytes

The observed size of the file at the model path. Omitted when `weights` is
`missing`, because there is no file to measure.

### Why mismatched is not called "partial"

The test is a size comparison, so a file **larger** than declared is also
`mismatched`. That is not an interrupted download, it is the wrong file at
that path. Reporting it as partial would be a claim this side cannot support.

Callers decide resumability from the numbers instead:

```
weights == "mismatched" && on_disk_bytes < size_bytes   ->  interrupted download
weights == "mismatched" && on_disk_bytes > size_bytes   ->  not resumable
```

This matters for progress UI. A client that renders a bar from a resumed
download starts it partway along; without the distinction, the wrong-file case
renders as a bar running backwards.

### Freshness

The catalog is built per request, from the same inspection the dashboard uses.
A model downloaded or deleted while the gateway runs is reflected on the next
call, with no restart.

The set of model **ids** is fixed at build time by the embedded profile
manifest. Weights availability changes at runtime; the roster does not.

### Compatibility

`weights`, `size_bytes`, and `on_disk_bytes` are additive. A client decoding
only the standard OpenAI fields is unaffected.

## Download progress

Downloads emit newline-delimited JSON on stderr:

```json
{"name":"...","downloaded":0,"total":0,"bytes_per_second":0,"eta_seconds":0,"done":false}
```

A client rendering a progress bar should read `weights` and `on_disk_bytes`
first, so a resumed download is presented as resuming rather than as a bar
starting at an unexplained offset.

## Machine-readable CLI

`--json` is accepted anywhere in the argument list, before or after the
subcommand. Output is also JSON automatically when stdout is not a terminal,
so a piped invocation needs no flag.

## check

`outrider check <profile>` reports a top-level `class` plus named sub-checks:

```json
{"profile":"...","class":"degraded","checks":[
  {"id":"physical_memory","result":"pass",
   "measured":"68719476736 bytes","required":"validated at 68719476736 bytes"},
  {"id":"runtime_capabilities","result":"warn",
   "measured":"runtime executable unavailable","required":"...","nextAction":"..."}]}
```

`result` is `pass`, `warn`, or `fail`; `nextAction` appears on non-pass. There
is no per-check `class`, only the top-level one, so a caller distinguishing
kinds of degradation reads sub-check ids.

Two in particular differ in kind:

- `runtime_capabilities` warns when the pinned runtime is not installed yet.
  Transient, and expected on a machine that has never run a model.
- `physical_memory` warns when the machine has less RAM than the profile was
  validated on. Permanent for that machine. It is a warning, never a failure,
  so a smaller machine still runs the profile.

`physical_memory` reports both sides in exact bytes, so a caller can derive a
memory recommendation from the machine rather than hold its own constant.
