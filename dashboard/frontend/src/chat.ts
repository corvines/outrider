import {Browser, Clipboard} from "@wailsio/runtime";
import {DashboardService} from "../bindings/github.com/corvines/outrider/dashboard";
import {chatSetup, downloadGreetings} from "./chat_setup";
import type {ChatSetupSnapshot} from "./chat_setup";
import {isWebLink, renderMarkdown} from "./markdown";

type ChatState = Awaited<ReturnType<typeof DashboardService.ChatSnapshot>>;

export function chatTranscript(state: Pick<ChatState, "model" | "mode" | "messages">): string {
  return `# Outrider conversation\n\nModel: ${state.model}\nMode: ${state.mode}\n\n` + (state.messages ?? []).map(
    (message) => `## ${message.role === "user" ? "You" : "Assistant"}\n\n${message.content}`,
  ).join("\n\n");
}

export function mountChat(root: HTMLElement, actions: {models: () => void; prepare: () => void; server: () => void; pause: () => void}) {
  root.innerHTML = `
    <div class="chat-toolbar"><span id="chat-model">Local chat</span><div class="status-actions">
      <button id="chat-setup" class="refresh" type="button">Models</button>
      <button id="chat-copy" class="refresh" type="button" disabled>Copy chat</button>
      <button id="chat-new" class="refresh" type="button" disabled>New chat</button>
    </div></div>
    <p class="card-note">Try your local model here. No tools or web access. Chats stay in this window until you clear them or quit; copy anything you want to keep.</p>
    <div id="chat-scroll" class="chat-messages" aria-label="Conversation">
      <section id="chat-welcome" class="chat-welcome" aria-labelledby="chat-welcome-title">
        <h3 id="chat-welcome-title">What do you want this session for?</h3>
        <div class="chat-choices">
          <button class="refresh chat-choice" type="button" data-mode="help"><strong>Outrider & Vera help</strong><span>Setup guidance using the offline docs.</span></button>
          <button class="refresh chat-choice" type="button" data-mode="bare"><strong>Just chat</strong><span>The model as-is, with no system prompt (bare).</span></button>
        </div>
      </section>
      <section id="chat-model-card" class="chat-model-card" aria-labelledby="chat-model-headline">
        <h3 id="chat-model-headline"></h3><p id="chat-model-detail"></p>
        <div class="status-actions"><button id="chat-prepare" class="refresh" type="button"></button>
          <button id="chat-pause" class="refresh hidden" type="button">Pause</button>
          <button id="chat-other-model" class="refresh" type="button">Choose another model</button></div>
      </section>
      <div id="chat-history"></div>
    </div>
    <form id="chat-form" class="chat-composer">
      <label for="chat-input">Message your model</label>
      <textarea id="chat-input" rows="3" placeholder="Ask a question..." maxlength="32768"></textarea>
      <div class="chat-send-row"><span id="chat-status" role="status" aria-live="polite">Checking your local model...</span>
        <button id="chat-cancel" class="refresh hidden" type="button">Stop reply</button>
        <button id="chat-send" class="refresh" type="submit">Send</button>
      </div>
      <small class="card-note">Enter to send · Shift+Enter for a new line</small>
    </form>
    <div id="chat-clear-dialog" class="confirm-dialog hidden" role="dialog" aria-modal="true" aria-labelledby="chat-clear-title">
      <div class="confirm-card"><h3 id="chat-clear-title">Clear this conversation?</h3>
        <p>Copy chat first if you want to keep it. This starts an empty conversation.</p>
        <div class="confirm-actions"><button id="chat-keep" class="refresh" type="button">Keep chat</button>
          <button id="chat-clear" class="refresh" type="button">Clear chat</button></div>
      </div>
    </div>`;
  const get = <T extends HTMLElement>(id: string) => root.querySelector<T>(`#${id}`)!;
  const messages = get<HTMLDivElement>("chat-history");
  const scroll = get<HTMLDivElement>("chat-scroll");
  const input = get<HTMLTextAreaElement>("chat-input");
  const send = get<HTMLButtonElement>("chat-send");
  const cancel = get<HTMLButtonElement>("chat-cancel");
  const fresh = get<HTMLButtonElement>("chat-new");
  const copy = get<HTMLButtonElement>("chat-copy");
  const status = get<HTMLSpanElement>("chat-status");
  let state: ChatState | undefined;
  let busy = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let messageBodies: HTMLElement[] = [];
  let messageSources: string[] = [];
  let snapshot: ChatSetupSnapshot | undefined;
  let setupBusy = false;
  let greeting = Math.floor(Math.random() * downloadGreetings.length);
  let setupView = chatSetup(snapshot, setupBusy, greeting);
  let empty: HTMLElement | undefined;
  let localError = "";

  function notice(text: string, error = false) {
    localError = error ? text : "";
    status.textContent = text;
    status.classList.toggle("error", error);
  }

  function controls() {
    send.disabled = busy || !!state?.streaming || !state?.mode || !setupView.ready || setupBusy || !input.value.trim();
    fresh.disabled = busy || !!state?.streaming || !state?.mode;
    copy.disabled = !state?.messages?.length;
    cancel.classList.toggle("hidden", !state?.streaming);
    cancel.disabled = busy || !!state?.stopped;
    root.querySelectorAll<HTMLButtonElement>("[data-mode]").forEach((button) => { button.disabled = busy; });
  }

  function renderSetup() {
    setupView = chatSetup(snapshot, setupBusy, greeting);
    get("chat-model-card").classList.toggle("hidden", setupView.ready);
    get("chat-model-headline").textContent = setupView.headline;
    get("chat-model-detail").textContent = setupView.detail;
    const prepare = get<HTMLButtonElement>("chat-prepare");
    prepare.textContent = setupView.label;
    prepare.classList.toggle("hidden", !setupView.action);
    prepare.disabled = !setupView.enabled;
    get("chat-pause").classList.toggle("hidden", !setupView.pause);
    if (empty) empty.classList.toggle("hidden", !state?.mode || !setupView.ready);
    if (!busy && !state?.streaming && !state?.error && !localError) {
      notice(!setupView.ready ? "Get your model ready above. You can draft a message while you wait."
        : !state?.mode ? "Choose Help or Just chat above."
        : state.stopped ? "Reply stopped. Partial text is kept." : "Ready when you are.");
    }
    controls();
  }

  function render(next: ChatState) {
    const changed = !state || JSON.stringify(state.messages) !== JSON.stringify(next.messages);
    state = next;
    const mode = next.mode === "help" ? "Outrider & Vera help" : next.mode === "bare" ? "Just chat (bare)" : "Local chat";
    get("chat-model").textContent = next.model ? `${mode} · ${next.model} · on this Mac` : mode;
    get("chat-welcome").classList.toggle("hidden", !!next.mode);
    if (changed) {
      const follow = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight < 80;
      if (!next.messages?.length || !messageBodies.length) {
        messages.replaceChildren();
        messageBodies = [];
        messageSources = [];
        empty = undefined;
      }
      if (!next.messages?.length) {
        empty = document.createElement("p");
        empty.className = "empty";
        empty.textContent = "Your conversation will appear here.";
        messages.append(empty);
      }
      const nextMessages = next.messages ?? [];
      for (let index = 0; index < nextMessages.length; index++) {
        const message = nextMessages[index];
        if (!messageBodies[index]) {
          const article = document.createElement("article");
          article.className = `chat-message ${message.role === "user" ? "chat-user" : "chat-assistant"}`;
          article.setAttribute("aria-label", message.role === "user" ? "Your message" : "Assistant reply");
          const body = document.createElement("div");
          body.className = message.role === "assistant" ? "chat-text chat-markdown" : "chat-text";
          article.append(body);
          messages.append(article);
          messageBodies.push(body);
        }
        const body = messageBodies[index];
        const text = message.content || (next.streaming ? "Waiting for the model..." : "No reply text.");
        if (messageSources[index] !== text) {
          if (message.role === "assistant") body.innerHTML = renderMarkdown(text);
          else body.textContent = text;
          messageSources[index] = text;
        }
      }
      if (follow) scroll.scrollTop = scroll.scrollHeight;
    }
    notice(next.error || (next.streaming ? (next.stopped ? "Stopping reply..." : "Replying...") : "Ready"), !!next.error);
    renderSetup();
    clearTimeout(timer);
    if (next.streaming) timer = setTimeout(() => void poll(), 200);
  }

  async function poll() {
    try { render(await DashboardService.ChatSnapshot()); }
    catch (error) {
      notice(`Cannot read chat: ${String(error)}`, true);
      timer = setTimeout(() => void poll(), 1000);
    }
  }

  get<HTMLFormElement>("chat-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    if (busy || state?.streaming || !state?.mode || !setupView.ready || setupBusy || !input.value.trim()) return;
    busy = true;
    controls();
    notice("Sending...");
    const submitted = input.value;
    try {
      const next = await DashboardService.SendChat(submitted);
      if (input.value === submitted) input.value = "";
      render(next);
    } catch (error) {
      notice(String(error).replace(/^RuntimeError:\s*/, ""), true);
      try { snapshot = await DashboardService.Snapshot(); renderSetup(); } catch { /* Keep the send error visible. */ }
    }
    finally { busy = false; controls(); input.focus(); }
  });
  input.addEventListener("input", controls);
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      get<HTMLFormElement>("chat-form").requestSubmit();
    }
  });
  cancel.addEventListener("click", async () => {
    try { await DashboardService.CancelChat(); await poll(); }
    catch (error) { notice(String(error), true); }
  });
  fresh.addEventListener("click", () => {
    if (busy || state?.streaming) return;
    if (!state?.messages?.length) { void clearChat(); return; }
    get("chat-clear-dialog").classList.remove("hidden");
    get("chat-keep").focus();
  });
  get("chat-keep").addEventListener("click", () => {
    get("chat-clear-dialog").classList.add("hidden");
    fresh.focus();
  });
  get("chat-clear-dialog").addEventListener("keydown", (event) => {
    if (event.key === "Escape") get("chat-keep").click();
    if (event.key === "Tab") {
      event.preventDefault();
      get(document.activeElement === get("chat-keep") ? "chat-clear" : "chat-keep").focus();
    }
  });
  async function clearChat() {
    if (busy || state?.streaming) return;
    busy = true;
    controls();
    try {
      greeting = (greeting + 1) % downloadGreetings.length;
      render(await DashboardService.NewChat());
      get("chat-clear-dialog").classList.add("hidden");
      root.querySelector<HTMLButtonElement>("[data-mode]")?.focus();
    }
    catch (error) { notice(String(error), true); }
    finally { busy = false; renderSetup(); }
  }
  get("chat-clear").addEventListener("click", () => void clearChat());
  copy.addEventListener("click", async () => {
    if (!state) return;
    try { await Clipboard.SetText(chatTranscript(state)); notice("Chat copied. Review for private information before sharing."); }
    catch (error) { notice(`Could not copy chat: ${String(error)}`, true); }
  });
  messages.addEventListener("click", async (event) => {
    if (!(event.target instanceof Element)) return;
    const codeCopy = event.target.closest<HTMLButtonElement>(".markdown-code-copy");
    if (codeCopy) {
      const code = codeCopy.closest(".markdown-code")?.querySelector("pre code")?.textContent;
      if (code === undefined || code === null) return;
      try { await Clipboard.SetText(code); notice("Code copied."); }
      catch (error) { notice(`Could not copy code: ${String(error)}`, true); }
      return;
    }
    const link = event.target.closest<HTMLAnchorElement>("a[href]");
    if (!link) return;
    event.preventDefault();
    const url = link.getAttribute("href") || "";
    if (!isWebLink(url)) return;
    try { await Browser.OpenURL(url); }
    catch (error) { notice(`Could not open link: ${String(error)}`, true); }
  });
  get("chat-setup").addEventListener("click", actions.models);
  get("chat-other-model").addEventListener("click", actions.models);
  get("chat-prepare").addEventListener("click", () => {
    if (!setupView.enabled) return;
    notice("Preparing your model...");
    if (setupView.action === "server") actions.server(); else actions.prepare();
  });
  get("chat-pause").addEventListener("click", actions.pause);
  root.querySelectorAll<HTMLButtonElement>("[data-mode]").forEach((button) => button.addEventListener("click", async () => {
    if (busy) return;
    busy = true;
    controls();
    try { render(await DashboardService.SetChatMode(button.dataset.mode!)); input.focus(); }
    catch (error) { notice(String(error).replace(/^RuntimeError:\s*/, ""), true); }
    finally { busy = false; renderSetup(); }
  }));
  renderSetup();
  controls();
  void poll();
  return {
    focus: () => input.focus(),
    enter: () => { greeting = (greeting + 1) % downloadGreetings.length; renderSetup(); },
    updateSetup: (next: ChatSetupSnapshot | undefined, pending: boolean) => { snapshot = next; setupBusy = pending; renderSetup(); },
    showError: (error: string) => notice(error.replace(/^RuntimeError:\s*/, ""), true),
  };
}
