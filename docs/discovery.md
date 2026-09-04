# Finding Outrider from another program

Outrider writes nothing into anyone else's configuration. A program that wants
to use it reads these four things instead.

1. Ask `http://127.0.0.1:11435/v1/models`. If it answers, the gateway is up and
   there is nothing else to do.
2. If it does not answer, read the install marker at
   `~/.local/share/outrider/install.json` and take `target`. This is the step
   that works when `~/.local/bin` is not on `PATH`, which is the normal case
   right after an install.
3. Run `outrider status --json` for the endpoint this machine actually uses. It
   is not always the default port.
4. Run `outrider models --json` for the roster. Do not hardcode profile ids.

[Install marker](install-marker.md) and [Gateway API](gateway-api.md) are the
normative descriptions of those two surfaces.
