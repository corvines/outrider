import {DashboardService} from "../bindings/github.com/corvines/outrider/dashboard";
import {setupState} from "./first_run";
import {mountChat} from "./chat";

const app = document.querySelector<HTMLDivElement>("#app")!;

app.innerHTML = `
  <div class="shell">
    <aside class="sidebar">
      <div class="brand"><div class="brand-row"><span class="brand-mark" aria-hidden="true"></span><h1>Outrider</h1></div><small>local model server</small></div>
      <nav class="nav" aria-label="Dashboard sections">
        <button class="active" type="button" data-target="overview">Overview</button>
        <button type="button" data-target="chat">Chat</button>
        <button type="button" data-target="models">Models</button>
        <button type="button" data-target="performance">Performance</button>
        <button type="button" data-target="logs">Logs</button>
      </nav>
      <div class="sidebar-footer">dashboard beta</div>
    </aside>
    <main class="content">
      <div class="topline">
        <h2 id="page-title" class="title">Serving status</h2>
        <div class="top-actions"><span id="action-status" class="action-status"></span><button id="refresh" class="refresh" type="button">Refresh</button></div>
      </div>

      <div id="loading-progress" class="loading-progress hidden"><div class="loading-progress-row"><span id="loading-label">Loading model…</span><span id="loading-percent">—</span><button id="pause-model" class="refresh" type="button" disabled>Pause loading</button></div><div class="progress-track"><div id="loading-bar" class="progress-fill"></div></div><div id="loading-detail" class="card-note"></div></div>

      <div id="page-overview" class="page">
        <section class="status-card">
          <div class="status-row"><span id="status-dot" class="status-dot"></span><span id="status-label" class="status-label">Checking gateway…</span></div>
          <div id="status-detail" class="status-detail">Connecting to the local Outrider gateway</div>
          <div class="status-actions"><button id="start-server" class="refresh" type="button">Start server</button><button id="stop-server" class="refresh" type="button">Stop server</button><button id="quit-server" class="refresh" type="button">Quit Outrider</button><button id="stop-model" class="refresh" type="button" disabled>Unload model</button></div>
          <p class="card-note">Stop server ends model requests in all connected chats. Downloaded models stay on disk.</p>
        </section>
        <section class="status-card" aria-labelledby="starter-title">
          <h3 id="starter-title">Start chatting</h3>
          <p id="starter-description" class="status-detail">Start the server to check your downloaded models.</p>
          <div class="status-actions"><button id="start-chat" class="refresh" type="button" disabled>Chat</button><button id="choose-model" class="refresh" type="button">Choose another model</button></div>
          <p class="card-note">Chat stays inside Outrider. The starter model downloads only when you choose Download.</p>
        </section>
        <section class="rows">
          <div class="row"><div class="row-head"><span class="row-label">Active model</span><span id="model-note" class="row-note">No model loaded</span></div><div id="model" class="row-value">—</div></div>
          <div class="row"><div class="row-head"><span class="row-label">Context</span><span class="row-note">loaded model window</span></div><div id="context" class="row-value">—</div></div>
          <div class="row"><div class="row-head"><span class="row-label">Advertised models</span><span class="row-note">available to clients</span></div><div id="model-count" class="row-value">—</div></div>
          <div class="row"><div class="row-head"><span class="row-label">Endpoint</span></div><div id="endpoint" class="row-value">—</div></div>
          <div class="row"><div class="row-head"><span class="row-label">Last updated</span></div><div id="updated" class="row-value">—</div></div>
        </section>
        <article class="card chart-card"><div class="card-title">Model memory</div><div id="memory" class="card-value">—</div><div class="chart-wrap"><div class="chart-y-labels"><span class="chart-label" data-axis-high>—</span><span class="chart-label" data-axis-low>—</span></div><svg id="memory-chart" class="sparkline" viewBox="0 0 360 170" preserveAspectRatio="none" role="img" aria-label="Resident memory trend"><line class="chart-axis" x1="42" y1="14" x2="42" y2="132" /><line class="chart-axis" x1="42" y1="132" x2="350" y2="132" /><line class="chart-grid" x1="42" y1="14" x2="350" y2="14" /><polyline /></svg><div class="chart-x-labels"><span>older</span><span>now</span></div></div><div class="card-note">resident set</div></article>
      </div>

      <div id="page-chat" class="page hidden"></div>

      <div id="page-models" class="page hidden">
        <div class="page-intro"><p>Download, load, or remove local models.</p><form id="download-form" class="download-form"><input id="download-path" type="text" placeholder="Hugging Face path or HTTPS URL" aria-label="Model URL or path"><button class="refresh" type="submit">Add &amp; download</button></form></div>
        <article class="card card-full"><div id="models" class="model-list"><div class="empty">Loading catalog…</div></div></article>
      </div>

      <div id="page-performance" class="page hidden">
        <div class="page-intro"><p>Runtime signals from the resident model and gateway.</p></div>
        <section class="rows">
          <div class="row"><div class="row-head"><span class="row-label">Context window</span><span class="row-note">active model window</span></div><div id="performance-context" class="row-value">—</div></div>
          <div class="row"><div class="row-head"><span class="row-label">Active model</span><span class="row-note">resident model</span></div><div id="performance-model" class="row-value">—</div></div>
          <div class="row"><div class="row-head"><span class="row-label">Gateway</span><span id="performance-updated" class="row-note">—</span></div><div id="performance-endpoint" class="row-value">—</div></div>
        </section>
        <article class="card chart-card"><div class="card-title">Resident memory</div><div id="performance-memory" class="card-value">—</div><div class="chart-wrap"><div class="chart-y-labels"><span class="chart-label" data-axis-high>—</span><span class="chart-label" data-axis-low>—</span></div><svg id="performance-chart" class="sparkline" viewBox="0 0 360 170" preserveAspectRatio="none" role="img" aria-label="Resident memory trend"><line class="chart-axis" x1="42" y1="14" x2="42" y2="132" /><line class="chart-axis" x1="42" y1="132" x2="350" y2="132" /><line class="chart-grid" x1="42" y1="14" x2="350" y2="14" /><polyline /></svg><div class="chart-x-labels"><span>older</span><span>now</span></div></div><div class="card-note">sampled while the dashboard is open</div></article>
      </div>

      <div id="page-logs" class="page hidden">
        <div class="page-intro"><p>Inspect the gateway when a model load or request needs diagnosis.</p></div>
        <article class="card card-full logs-card"><div class="card-title">Current run log</div><pre id="logs-content" class="log-empty">Waiting for the current run log…</pre><div id="logs-file" class="card-note"></div></article>
      </div>
    </main>
  </div>
  <div id="delete-dialog" class="confirm-dialog hidden" role="dialog" aria-modal="true" aria-labelledby="delete-dialog-title">
    <div class="confirm-card">
      <div id="delete-dialog-title" class="card-title">Delete model?</div>
      <p id="delete-dialog-message">This removes the local model file.</p>
      <div class="confirm-actions"><button id="delete-cancel" class="model-action" type="button">No, keep it</button><button id="delete-confirm" class="model-action model-delete" type="button">Yes, delete</button></div>
    </div>
  </div>
`;

const element = <T extends Element>(id: string) => document.getElementById(id)! as unknown as T;
const dot = element<HTMLSpanElement>("status-dot");
const label = element<HTMLSpanElement>("status-label");
const detail = element<HTMLDivElement>("status-detail");
const startServer = element<HTMLButtonElement>("start-server");
const stopServer = element<HTMLButtonElement>("stop-server");
const quitServer = element<HTMLButtonElement>("quit-server");
const startChat = element<HTMLButtonElement>("start-chat");
const starterDescription = element<HTMLParagraphElement>("starter-description");
const stopModel = element<HTMLButtonElement>("stop-model");
const pauseModel = element<HTMLButtonElement>("pause-model");
const loadingProgress = element<HTMLDivElement>("loading-progress");
const loadingLabel = element<HTMLSpanElement>("loading-label");
const loadingPercent = element<HTMLSpanElement>("loading-percent");
const loadingBar = element<HTMLDivElement>("loading-bar");
const loadingDetail = element<HTMLDivElement>("loading-detail");
const actionStatus = element<HTMLSpanElement>("action-status");
const model = element<HTMLDivElement>("model");
const modelNote = element<HTMLDivElement>("model-note");
const memory = element<HTMLDivElement>("memory");
const memoryChart = element<SVGSVGElement>("memory-chart");
const performanceMemory = element<HTMLDivElement>("performance-memory");
const performanceChart = element<SVGSVGElement>("performance-chart");
const context = element<HTMLDivElement>("context");
const performanceContext = element<HTMLDivElement>("performance-context");
const performanceModel = element<HTMLDivElement>("performance-model");
const endpoint = element<HTMLSpanElement>("endpoint");
const updated = element<HTMLSpanElement>("updated");
const performanceEndpoint = element<HTMLDivElement>("performance-endpoint");
const performanceUpdated = element<HTMLDivElement>("performance-updated");
const modelCount = element<HTMLDivElement>("model-count");
const models = element<HTMLDivElement>("models");
const logsContent = element<HTMLPreElement>("logs-content");
const logsFile = element<HTMLDivElement>("logs-file");
const downloadForm = element<HTMLFormElement>("download-form");
const downloadPath = element<HTMLInputElement>("download-path");
const deleteDialog = element<HTMLDivElement>("delete-dialog");
const deleteDialogMessage = element<HTMLParagraphElement>("delete-dialog-message");
const deleteCancel = element<HTMLButtonElement>("delete-cancel");
const deleteConfirm = element<HTMLButtonElement>("delete-confirm");
const pageTitle = element<HTMLHeadingElement>("page-title");
const content = document.querySelector<HTMLElement>(".content")!;
const navButtons = document.querySelectorAll<HTMLButtonElement>(".nav button[data-target]");
const pages = document.querySelectorAll<HTMLElement>(".page");
let memorySamples: number[] = [];
let pendingDeleteModel = "";
let latestSnapshot: Awaited<ReturnType<typeof DashboardService.Snapshot>> | undefined;
let serverInFlight = false;
let chatView: ReturnType<typeof mountChat> | undefined;

const pageMeta: Record<string, {title: string}> = {
  overview: {title: "Serving status"},
  chat: {title: "Chat"},
  models: {title: "Model catalog"},
  performance: {title: "Runtime signals"},
  logs: {title: "Gateway logs"},
};

function showPage(target: string) {
  const enteringChat = target === "chat" && !content.classList.contains("chat-active");
  pages.forEach((page) => page.classList.toggle("hidden", page.id !== `page-${target}`));
  navButtons.forEach((button) => button.classList.toggle("active", button.dataset.target === target));
  content.classList.toggle("models-active", target === "models");
  content.classList.toggle("chat-active", target === "chat");
  const meta = pageMeta[target] || pageMeta.overview;
  pageTitle.textContent = meta.title;
  if (enteringChat) chatView?.enter();
}

function formatBytes(bytes: number) {
  if (!bytes) return "—";
  const units = ["B", "KiB", "MiB", "GiB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit += 1; }
  return `${value.toFixed(unit ? 1 : 0)} ${units[unit]}`;
}

function formatContext(tokens: number) {
  if (!tokens) return "—";
  return tokens >= 1000 ? `${(tokens / 1000).toFixed(tokens % 1000 ? 1 : 0)}k` : `${tokens}`;
}

function setMemoryText(value: string) {
  memory.textContent = value;
  performanceMemory.textContent = value;
}

function setContextText(value: string) {
  context.textContent = value;
  performanceContext.textContent = value;
}

function setModelText(value: string) {
  model.textContent = value;
  performanceModel.textContent = value;
}

function renderOffline(error: string) {
  chatView?.updateSetup({gatewayHealth: "offline", model: {kind: "stopped", preset: ""}, models: [], serverError: error}, serverInFlight || actionInFlight);
  dot.className = "status-dot offline";
  label.textContent = "Gateway offline";
  detail.textContent = error;
  setModelText("—");
  modelNote.textContent = "Start Outrider to load a model";
  setMemoryText("—");
  memorySamples = [];
  memoryChart.querySelector("polyline")?.setAttribute("points", "");
  performanceChart.querySelector("polyline")?.setAttribute("points", "");
  setContextText("—");
  endpoint.textContent = "—";
  updated.textContent = "—";
  performanceEndpoint.textContent = "—";
  performanceUpdated.textContent = "—";
  modelCount.textContent = "—";
  models.innerHTML = `<div class="empty">No catalog available while Outrider is offline.</div>`;
  renderLogs([], "");
  stopModel.disabled = true;
  pauseModel.disabled = true;
  loadingProgress.classList.add("hidden");
  startChat.disabled = true;
  starterDescription.textContent = "Start the server to check your downloaded models.";
}

async function refresh() {
  try {
    const snapshot = await DashboardService.Snapshot();
    renderSnapshot(snapshot);
  } catch (error) {
    renderOffline(String(error));
  }
}

function catalogUnavailable(snapshot: Awaited<ReturnType<typeof DashboardService.Snapshot>>) {
  return snapshot.gatewayHealth === "offline" && !(snapshot.models && snapshot.models.length);
}

function downloadStatus(entry: {cached: boolean; path?: string}) {
  if (entry.cached) return "On disk";
  if (entry.path) return "Incomplete download";
  return "Not downloaded";
}

function renderSnapshot(snapshot: Awaited<ReturnType<typeof DashboardService.Snapshot>>) {
  latestSnapshot = snapshot;
  chatView?.updateSetup(snapshot, actionInFlight || serverInFlight);
  const setup = setupState(snapshot, actionInFlight);
  startServer.disabled = !setup.canStart || serverInFlight;
  startServer.textContent = setup.startLabel;
  startServer.classList.toggle("hidden", setup.ready && !setup.serverBusy);
  stopServer.disabled = !setup.canStop || serverInFlight;
  quitServer.disabled = !setup.canStop || serverInFlight;
  startChat.disabled = !setup.canChat || serverInFlight;
  startChat.textContent = setup.chatLabel;
  starterDescription.textContent = !setup.ready ? "Start the server to check your downloaded models."
    : setup.loading ? "Preparing the model. Progress is shown above; you can pause and resume."
    : setup.needsDownload ? `Download Ling, the starter model${setup.sizeBytes ? ` (${formatBytes(setup.sizeBytes)})` : ""}? Runtime files may also be downloaded. You can choose another model instead.`
    : `${setup.candidate} is already downloaded. Chat will reuse it without downloading the weights again.`;
  if (catalogUnavailable(snapshot)) {
    renderOffline(snapshot.serverError || snapshot.error || "Server stopped. Click Start server when ready.");
    label.textContent = setup.serverBusy ? setup.startLabel : "Server stopped";
    return;
  }
  const healthy = snapshot.gatewayHealth === "ok";
  const legacy = snapshot.gatewayHealth === "legacy";
  const loading = snapshot.loading;
  dot.className = `status-dot ${healthy ? "ok" : legacy ? "legacy" : ""}`;
  label.textContent = setup.serverBusy ? setup.startLabel : setup.loading ? "Preparing model" : healthy ? "Server running" : legacy ? "Gateway connected (legacy)" : "Server stopped";
  detail.textContent = snapshot.serverError || snapshot.error
    ? snapshot.serverError || snapshot.error || ""
    : legacy
    ? "Catalog is read-only; restart Outrider from the current build to enable controls"
    : loading ? `Preparing ${loading.model}…` : snapshot.model.preset ? `${snapshot.model.preset} · ${snapshot.model.kind}` : "No model loaded";
  renderLoading(loading);
  setModelText(snapshot.model.preset || "No model loaded");
  modelNote.textContent = snapshot.model.startedAt ? `started ${new Date(snapshot.model.startedAt).toLocaleString()}` : "Ready for a model";
  const residentBytes = snapshot.model.residentBytes ?? 0;
  setMemoryText(formatBytes(residentBytes));
  renderMemoryChart(residentBytes);
  renderLogs(snapshot.logLines ?? [], snapshot.logFile ?? "");
  endpoint.textContent = snapshot.gatewayEndpoint || "—";
  updated.textContent = snapshot.updatedAt ? new Date(snapshot.updatedAt).toLocaleTimeString() : "—";
  performanceEndpoint.textContent = snapshot.gatewayEndpoint || "—";
  performanceUpdated.textContent = snapshot.updatedAt ? `updated ${new Date(snapshot.updatedAt).toLocaleTimeString()}` : "—";
  const catalog = snapshot.models ?? [];
  modelCount.textContent = `${catalog.length}`;
  const activeModel = catalog.find((entry) => entry.id === snapshot.model.preset);
  setContextText(formatContext(activeModel?.context ?? 0));
  stopModel.disabled = actionInFlight || serverInFlight || !healthy || snapshot.model.kind !== "running" || setup.loading;
  pauseModel.disabled = !healthy || !setup.loading || serverInFlight;
  pauseModel.textContent = loading?.phase === "paused" ? "Paused" : "Pause loading";
  actionStatus.classList.toggle("error", !!snapshot.error);
  models.innerHTML = catalog.length ? catalog.map((entry) => {
    const active = entry.id === snapshot.model.preset && snapshot.model.kind === "running";
    const disabled = actionInFlight || serverInFlight || !healthy || setup.loading ? "disabled" : "";
    let actions = "";
    if (entry.protected) actions += `<span class="protected-badge">Protected</span>`;
    const canReveal = !!entry.path;
    actions += `<button class="model-action" type="button" data-action="reveal" data-model="${escapeHTML(entry.id)}" ${canReveal ? "" : "disabled"}>Show in Finder</button>`;
    if (entry.custom) {
      actions += `<span class="loaded-badge downloaded-badge">Downloaded</span><button class="model-action model-delete" type="button" data-action="delete" data-model="${escapeHTML(entry.id)}" ${disabled}>Delete</button>`;
    } else if (active) {
      actions += `<span class="loaded-badge">Loaded</span><button class="model-action" type="button" data-action="unload" data-model="${escapeHTML(entry.id)}" ${disabled}>Unload</button>`;
    } else if (!entry.cached) {
      actions += `<button class="model-action" type="button" data-action="download" data-model="${escapeHTML(entry.id)}" ${disabled}>Download</button>`;
      if (entry.canDelete) actions += `<button class="model-action model-delete" type="button" data-action="delete" data-model="${escapeHTML(entry.id)}" ${disabled}>Delete</button>`;
    } else {
      actions += `<button class="model-action" type="button" data-action="load" data-model="${escapeHTML(entry.id)}" ${disabled}>Load</button>`;
      if (entry.canDelete) actions += `<button class="model-action model-delete" type="button" data-action="delete" data-model="${escapeHTML(entry.id)}" ${disabled}>Delete</button>`;
    }
    const spec = entry.custom ? `${formatBytes(entry.sizeBytes ?? 0)} · downloaded file` : `${formatContext(entry.context)} context · ${escapeHTML(entry.quantization || "unknown quant")}`;
    const pathLine = entry.path ? `<small class="model-path">${escapeHTML(entry.path)}</small>` : "";
    return `<div class="model"><div class="model-copy"><strong>${escapeHTML(entry.id)}</strong><br><small>${downloadStatus(entry)} · ${spec}</small>${pathLine}</div><div class="model-actions">${actions}</div></div>`;
  }).join("") : `<div class="empty">No models available.</div>`;
}

function renderLoading(loading: Awaited<ReturnType<typeof DashboardService.Snapshot>>["loading"]) {
  if (!loading) {
    loadingProgress.classList.add("hidden");
    return;
  }
  loadingProgress.classList.remove("hidden");
  loadingLabel.textContent = `${loading.phase} ${loading.model}`;
  if (loading.error) {
    loadingDetail.textContent = loading.error;
    loadingBar.style.width = "0";
    loadingPercent.textContent = "—";
    return;
  }
  const downloaded = loading.downloaded ?? 0;
  const total = loading.total ?? 0;
  const hasTotal = total > 0;
  const percent = hasTotal ? Math.min(100, (downloaded / total) * 100) : 0;
  loadingPercent.textContent = hasTotal ? `${percent.toFixed(0)}%` : "—";
  loadingBar.style.width = `${percent}%`;
  const rate = (loading.bytesPerSecond ?? 0) > 0 ? ` · ${formatBytes(loading.bytesPerSecond ?? 0)}/s` : "";
  const eta = (loading.etaSeconds ?? 0) > 0 ? ` · ${loading.etaSeconds}s remaining` : "";
  loadingDetail.textContent = `${formatBytes(downloaded)} / ${formatBytes(total)}${rate}${eta}`;
}

function renderLogs(lines: string[], logFile: string) {
  logsContent.innerHTML = lines.length ? lines.map(formatLogLine).join("\n") : "No log output yet.";
  logsFile.textContent = logFile ? `Source: ${logFile}` : "";
  logsContent.scrollTop = logsContent.scrollHeight;
}

function formatLogLine(line: string) {
  const severity = /\b[Ee](?:rror)?\b|failed|exception/i.test(line)
    ? "log-error"
    : /\b[Ww](?:arn(?:ing)?)?\b/i.test(line)
      ? "log-warn"
      : "log-info";
  return `<span class="log-line ${severity}">${escapeHTML(line)}</span>`;
}

let actionInFlight = false;

function setActionBusy(busy: boolean) {
  actionInFlight = busy;
  chatView?.updateSetup(latestSnapshot, busy || serverInFlight);
  if (!busy) { if (latestSnapshot) renderSnapshot(latestSnapshot); return; }
  startChat.disabled = true;
  stopModel.disabled = true;
  models.querySelectorAll<HTMLButtonElement>("button[data-model]").forEach((button) => { button.disabled = true; });
}

async function runAction(message: string, action: () => Promise<Awaited<ReturnType<typeof DashboardService.Snapshot>>>, success = "Updated") {
  if (actionInFlight || serverInFlight) return;
  actionStatus.textContent = message;
  setActionBusy(true);
  try {
    const snapshot = await action();
    renderSnapshot(snapshot);
    actionStatus.textContent = snapshot.error || (snapshot.loading?.phase === "paused" ? "Paused. Choose Resume & Chat when ready." : success);
    actionStatus.classList.toggle("error", !!snapshot.error);
  } catch (error) {
    actionStatus.textContent = String(error);
    actionStatus.classList.add("error");
    chatView?.showError(String(error));
    void refresh();
  } finally {
    setActionBusy(false);
  }
}

async function runServerAction(action: "start" | "stop" | "quit") {
  if (serverInFlight) return;
  serverInFlight = true;
  chatView?.updateSetup(latestSnapshot, true);
  startServer.disabled = stopServer.disabled = quitServer.disabled = startChat.disabled = true;
  actionStatus.textContent = action === "start" ? "Starting server..." : "Stopping server...";
  try {
    const snapshot = await (action === "start" ? DashboardService.StartServer()
      : action === "quit" ? DashboardService.QuitAndStopServer() : DashboardService.StopServer());
    latestSnapshot = snapshot;
    actionStatus.textContent = snapshot.serverError || (action === "start" ? "Server running" : "Server stopped");
    actionStatus.classList.toggle("error", !!snapshot.serverError);
  } catch (error) {
    actionStatus.textContent = String(error);
    actionStatus.classList.add("error");
    chatView?.showError(String(error));
  } finally {
    serverInFlight = false;
    if (latestSnapshot) renderSnapshot(latestSnapshot);
  }
}

function renderMemoryChart(bytes: number) {
  if (!bytes) {
    clearChart(memoryChart);
    clearChart(performanceChart);
    return;
  }
  memorySamples = [...memorySamples, bytes].slice(-24);
  const minimum = Math.min(...memorySamples);
  const maximum = Math.max(...memorySamples);
  updateChartLabels(memoryChart, minimum, maximum);
  updateChartLabels(performanceChart, minimum, maximum);
  if (minimum === maximum) {
    const points = "42,73 350,73";
    memoryChart.querySelector("polyline")?.setAttribute("points", points);
    performanceChart.querySelector("polyline")?.setAttribute("points", points);
    return;
  }
  const range = maximum - minimum || 1;
  const points = memorySamples.map((sample, index) => {
    const x = memorySamples.length === 1 ? 196 : 42 + (index / (memorySamples.length - 1)) * 308;
    const y = 132 - ((sample - minimum) / range) * 118;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(" ");
  memoryChart.querySelector("polyline")?.setAttribute("points", points);
  performanceChart.querySelector("polyline")?.setAttribute("points", points);
}

function clearChart(chart: SVGSVGElement) {
  chart.querySelector("polyline")?.setAttribute("points", "");
}

function updateChartLabels(chart: SVGSVGElement, minimum: number, maximum: number) {
  const container = chart.parentElement!;
  container.querySelector<HTMLElement>("[data-axis-high]")!.textContent = formatBytes(maximum);
  container.querySelector<HTMLElement>("[data-axis-low]")!.textContent = formatBytes(minimum);
}

function escapeHTML(value: string) {
  return value.replace(/[&<>'"]/g, (character) => ({"&":"&amp;", "<":"&lt;", ">":"&gt;", "'":"&#39;", "\"":"&quot;"})[character]!);
}

navButtons.forEach((button) => button.addEventListener("click", () => showPage(button.dataset.target || "overview")));
element<HTMLButtonElement>("refresh").addEventListener("click", refresh);
startServer.addEventListener("click", () => void runServerAction("start"));
stopServer.addEventListener("click", () => void runServerAction("stop"));
quitServer.addEventListener("click", () => void runServerAction("quit"));
element<HTMLButtonElement>("choose-model").addEventListener("click", () => showPage("models"));
chatView = mountChat(element<HTMLDivElement>("page-chat"), {
  models: () => showPage("models"),
  prepare: prepareChat,
  server: () => void runServerAction("start"),
  pause: () => pauseModel.click(),
});
function prepareChat() {
  if (!latestSnapshot) return;
  const choice = setupState(latestSnapshot, false);
  showPage("chat");
  void runAction(choice.needsDownload ? "Downloading the starter model..." : "Preparing chat...",
    async () => {
      const snapshot = await DashboardService.StartChat(choice.needsDownload);
      if (!snapshot.error && !snapshot.serverError && !snapshot.loading && snapshot.model.health && snapshot.model.kind === "running") {
        showPage("chat");
        chatView?.focus();
      }
      return snapshot;
    }, "Chat ready");
}
startChat.addEventListener("click", prepareChat);
stopModel.addEventListener("click", () => void runAction("Stopping model…", () => DashboardService.StopModel()));
pauseModel.addEventListener("click", async () => {
  pauseModel.disabled = true;
  try { renderSnapshot(await DashboardService.PauseModel()); }
  catch (error) { actionStatus.textContent = String(error); }
});
downloadForm.addEventListener("submit", (event) => {
  event.preventDefault();
  const path = downloadPath.value.trim();
  if (!path) {
    actionStatus.textContent = "Enter a model URL or path";
    return;
  }
  void runAction(`Downloading ${path}…`, async () => {
    const snapshot = await DashboardService.DownloadPath(path);
    if (!snapshot.error) downloadPath.value = "";
    return snapshot;
  });
});
models.addEventListener("click", (event) => {
  if (!(event.target instanceof HTMLElement)) return;
  const button = event.target.closest<HTMLButtonElement>("button[data-model]");
  const modelID = button?.dataset.model;
  const action = button?.dataset.action;
  if (!modelID || !action) return;
  if (action === "delete") {
    pendingDeleteModel = modelID;
    deleteDialogMessage.textContent = `Delete the cached ${modelID} model from this computer?`;
    deleteDialog.classList.remove("hidden");
    deleteConfirm.focus();
  } else if (action === "unload") {
    void runAction("Unloading model…", () => DashboardService.StopModel());
  } else if (action === "download") {
    void runAction(`Downloading ${modelID}…`, () => DashboardService.DownloadModel(modelID));
  } else if (action === "reveal") {
    void runAction(`Showing ${modelID} in Finder…`, () => DashboardService.RevealModel(modelID));
  } else {
    void runAction(`Loading ${modelID}…`, () => DashboardService.LoadModel(modelID));
  }
});

deleteCancel.addEventListener("click", () => {
  pendingDeleteModel = "";
  deleteDialog.classList.add("hidden");
});
deleteConfirm.addEventListener("click", () => {
  const modelID = pendingDeleteModel;
  pendingDeleteModel = "";
  deleteDialog.classList.add("hidden");
  if (!modelID) return;
  void runAction(`Deleting ${modelID}…`, () => DashboardService.DeleteModel(modelID));
});

showPage("overview");
void refresh();
void runServerAction("start");
window.setInterval(() => void refresh(), 3000);
