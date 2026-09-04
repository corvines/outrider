# Outrider dashboard

The dashboard is a Wails v3 beta shell around Outrider's loopback gateway. Opening
the app starts the local server. Closing the window hides it and leaves the
server running. Quit Outrider from the menu bar stops the server.

## Development

From this directory, with the Wails v3 CLI on `PATH`:

```sh
wails3 dev
```

The app reads the gateway from `OUTRIDER_PORT` (default `11435`). Build a
single native application with:

```sh
wails3 build
```
