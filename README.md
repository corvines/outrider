# Outrider

> **In development. Not for use yet. Not accepting pull requests at the
> moment.**

Outrider makes it quick and reliable to run local models on M-series Apple
Silicon Macs without learning the choices involved in a multi-model provider.
It serves an OpenAI-compatible endpoint at `http://127.0.0.1:11435/v1`.

The models fall into two categories:

1. Local-frontier models to use for real work. Today that is Qwen3.6 35B-A3B
   (`qwen35b-mtp`), with more models planned. Outrider provides a known-good
   configuration so you can get running quickly and reliably.
2. Everything else to experiment with and smoke-test. This includes Ling,
   simple chat, download checks, and experiments with Vera functionality.
   These models are included for exploration and testing; their presence
   does not mean they offer local-frontier capability.

Vera integration adds a place to start before connecting a paid provider,
including simple local chat. A fuller setup concierge is still in development.

For desktop use, open `Outrider.app`, choose Download starter model & Chat
once, then chat inside the app. A downloaded model is reused instead of
downloaded again. The starter model requires 64 GB RAM. No Terminal is needed
for desktop use. See [Desktop app](docs/desktop-app.md).

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

Choosing weights, quantization, context limits, and runtime settings is work
that Outrider handles through a small set of tested profiles. The aim is to
get a known-good model running on supported hardware with little setup.

Outrider can be used on its own or with an OpenAI-compatible client, with
additional features for Vera. Its serving backend is llama.cpp, and there is
no cloud fallback.

## Docs

- [Install](docs/install.md)
- [Running models](docs/running-models.md)
- [Desktop app](docs/desktop-app.md)
- [Finding Outrider from another program](docs/discovery.md)
- [Gateway API](docs/gateway-api.md)
- [Install marker](docs/install-marker.md)

MIT, see [LICENSE](LICENSE).
