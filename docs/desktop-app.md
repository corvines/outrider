# Desktop app

`Outrider.app` is a menu bar app that starts the local server for you, so
nothing has to be on `PATH` first. The bundle ships the `outrider` binary
inside it and runs that copy.

Building it needs the Wails v3 CLI:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
cd dashboard && wails3 task bundle
```

That writes `dist/Outrider.app`. `wails3` installs to `~/go/bin`, so add that
to `PATH` if the command is not found.

The app adopts a server that is already running and leaves it alone when you
quit. If it started the server itself, quitting stops it. Closing the window
keeps everything running; quit from the menu bar to shut down.

## Gatekeeper

The build is signed ad hoc, not notarized, so macOS blocks the first launch
with a dialog saying the app cannot be opened. Control-click the app in Finder,
choose Open, then confirm. macOS remembers the choice and later launches open
normally.
