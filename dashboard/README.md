# Outrider dashboard

The dashboard is a Wails v3 beta shell around Outrider's loopback gateway. It
provides a native macOS menu-bar item and a web frontend; inference and model
lifecycle remain owned by the Outrider daemon.

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
