# Outrider dashboard

The dashboard is a Wails v3 beta shell around Outrider's loopback gateway. Opening
the app starts the local server and shows any startup error with a Retry control.
Outrider appears in the Dock and Command-Tab. Its menu-bar icon also opens the
window and provides server and quit controls.
Start server and Stop server are available on Overview. Stop server stops the
gateway and resident model, including a server started before the app opened.
It keeps downloaded models on disk and interrupts requests from connected clients.

Overview offers Download starter model & Chat when no runnable model is cached.
Choosing it downloads Ling, verifies and loads it, then opens Chat inside the app.
The user does not open Terminal or need the CLI on PATH. If a model is already
loaded or cached, Chat reuses it. Models offers other choices; failed or paused
downloads can be retried. Nothing is downloaded merely by opening the app.

Chat streams text from the loaded local model. Stop reply cancels generation;
New chat clears the conversation after confirmation. Copy chat copies the current
conversation as Markdown. Conversations live in memory, not on disk; copy them
before clearing or quitting. There are no tools, web access, or file attachments.
Assistant replies render Markdown, including tables and fenced code with Copy.
Copy chat preserves the original Markdown. HTML is displayed as text, images
are not fetched, and clicked HTTP/HTTPS links open in the default browser.

Each new chat asks for its purpose: Outrider & Vera help uses the offline
reference, while Just chat (bare) sends no system prompt. Help instructions are
separate from the product reference at `Contents/Resources/llms.txt` in the app.
New chat clears the selected purpose too. Mode changes never relabel an existing
conversation or silently change its instructions.

Chat's model setup card offers a starter download when nothing runnable is cached,
or loads an existing model without downloading its weights again. Downloads need
explicit consent. The card shows size, progress, pause and retry controls. Its
greeting varies between visits, not during status polling. Send stays disabled
until a purpose is chosen and a model is ready; drafts remain editable meanwhile.

The app owns the server while it is open: it starts one on launch and stops it
on quit, including a server that was already running. An already healthy gateway
is adopted rather than restarted, so a loaded model stays in memory. Closing the
window hides it and leaves the server running. If stop fails, the app stays open
with the error instead of quitting.

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
