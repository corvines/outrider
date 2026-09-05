# Outrider

> **Under development. Not ready to use.** This is public so the work is in
> the open, not because it works. Commands, profile names and file locations
> change without notice, nothing is kept working across an upgrade, and things
> are broken at any given moment.

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

Profiles are pinned. Each one names a repo, a file, a quantization and the
flags it was tested with. `outrider models` lists them. There aren't many, and
they get swapped out when better weights show up. Outrider won't start a
profile that needs more memory than the machine has.

Apple silicon only. Weights come from their publishers under their own
licenses.

## Docs

- [Install](docs/install.md)
- [Running models](docs/running-models.md)
- [Desktop app](docs/desktop-app.md)
- [Finding Outrider from another program](docs/discovery.md)
- [Gateway API](docs/gateway-api.md)
- [Install marker](docs/install-marker.md)

MIT, see [LICENSE](LICENSE).
