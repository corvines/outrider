# Outrider

> **In development. Not for use yet. Not accepting pull requests at the
> moment.**

Outrider makes it quick and reliable to run a known-good local model on an
M-series Apple Silicon Mac. It handles the model and runtime settings so you
do not need to learn the choices involved in a multi-model provider. The main
profile today is Qwen3.6 35B-A3B (`qwen35b-mtp`), with more models planned.
It serves an OpenAI-compatible endpoint at `http://127.0.0.1:11435/v1`.

Outrider also includes smaller helper models, such as Ling, for smoke testing
and simple chat, plus additional integration with Vera. These helpers give
you something to interact with before you have set up Vera with a paid
provider. A fuller Vera setup concierge is still being developed. The helper
chat is a starting point; the 35B profile is the main model offering.

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

Outrider can be used on its own or with an OpenAI-compatible client. Its Vera
features add a local starting point for users who have not connected a paid
provider yet. Basic helper chat and the main model serve different purposes;
Ling's chat capability does not represent the capability of the 35B model.
The serving backend is llama.cpp, and there is no cloud fallback.

## Docs

- [Install](docs/install.md)
- [Running models](docs/running-models.md)
- [Desktop app](docs/desktop-app.md)
- [Finding Outrider from another program](docs/discovery.md)
- [Gateway API](docs/gateway-api.md)
- [Install marker](docs/install-marker.md)

MIT, see [LICENSE](LICENSE).
