# Outrider

> **In development. Not for use yet. Not accepting pull requests at the
> moment.**

Outrider aims to make downloading, configuring, and running local models simple
on M-series Apple Silicon Macs. It serves them at `http://127.0.0.1:11435/v1`
for OpenAI-compatible clients, with particular attention to compatibility with
Vera and the features it builds on local models.

The starter model, `ling3-tiny`, is a helper for setup questions and basic local
chat. Its inclusion is not a promise of strong general reasoning or coding
ability. Choose a model suited to the task; making it easy to run does not
change what it can do.

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

Outrider keeps a small set of model profiles with selected weights,
quantization, context limits, and runtime settings. The goal is to make local
model provisioning predictable on supported Apple Silicon hardware without
requiring users to tune the model server themselves.

Vera is a primary integration target for this local endpoint. Outrider provides
model serving and basic chat for setup and model checks; Vera provides the
agent environment and its additional features. Compatibility with Vera does
not mean every local model can support every task equally well. Outrider has
no cloud fallback. Its serving backend is llama.cpp.

## Docs

- [Install](docs/install.md)
- [Running models](docs/running-models.md)
- [Desktop app](docs/desktop-app.md)
- [Finding Outrider from another program](docs/discovery.md)
- [Gateway API](docs/gateway-api.md)
- [Install marker](docs/install-marker.md)

MIT, see [LICENSE](LICENSE).
