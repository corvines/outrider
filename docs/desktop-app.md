# Desktop app

`Outrider.app` starts the local server for you, so
nothing has to be on `PATH` first. The bundle ships the `outrider` binary
inside it and runs that copy.

The app appears in the Dock and Command-Tab. Its menu-bar icon reopens the
window. Choose Download starter model & Chat once, or Load model & Chat to
reuse downloaded weights. The starter model requires 64 GB RAM. Overview has
Start server and Stop server; Models offers other model choices.

Chat offers setup help from the offline reference or Just chat with no system
prompt. Enter sends; Shift+Enter adds a line. Replies display Markdown and code
blocks have Copy controls. Stop reply cancels generation. Copy chat preserves
the conversation as Markdown; copy before New chat or quitting, because history
is not saved. Chat has no tools, web access, or attachments.

Building it needs the Wails v3 CLI:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
cd dashboard && wails3 task bundle
```

That writes `dist/Outrider.app`. `wails3` installs to `~/go/bin`, so add that
to `PATH` if the command is not found.

The app adopts a healthy server without restarting it. Quit Outrider stops
the server and model, including a server that was already running. A failed
stop keeps the app open and shows the error. Closing the window hides it and
keeps everything running. Command-Q and the menu-bar Quit use the same check.

## Gatekeeper

The build is signed ad hoc, not notarized, so macOS blocks the first launch
with a dialog saying the app cannot be opened. Control-click the app in Finder,
choose Open, then confirm. macOS remembers the choice and later launches open
normally.
