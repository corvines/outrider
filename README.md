# Outrider

> **In development. Not for use yet. Not accepting pull requests at the
> moment.**

Runs local models on Apple silicon and serves them at
`http://127.0.0.1:11435/v1`, which any OpenAI-compatible client can talk to.

```sh
go build ./cmd/outrider
./outrider install
outrider pull qwen35b-mtp
outrider start
outrider use qwen35b-mtp
```

The endpoint stays the same when you switch models.

Apple silicon only. Weights come from their publishers under their own
licenses.

## Why this exists

Open-weight models good enough for real work now fit on a 32 to 64 GB Apple
silicon Mac. Running one well still takes decisions most people have no way to
make: which weights, which quantization, how much context, which KV cache type,
which flags. Get them wrong and the model is slow, or wrong, or takes the
machine down. The hardware here is narrow enough that those decisions can be
made once and tested, which is why Outrider is Mac only and why the list of
models is short.

Outrider serves an OpenAI-compatible endpoint and nothing else. No agent, no
tools, no cloud fallback. It was built for an internal harness that needed a
local endpoint in the first minute, offline, with no account and no API key,
and it is useful to anyone who already owns the hardware and would rather not
pay for API compute. The serving backend is llama.cpp today and can change. The
endpoint does not.

## Docs

- [Install](docs/install.md)
- [Running models](docs/running-models.md)
- [Desktop app](docs/desktop-app.md)
- [Finding Outrider from another program](docs/discovery.md)
- [Gateway API](docs/gateway-api.md)
- [Install marker](docs/install-marker.md)

MIT, see [LICENSE](LICENSE).
