import {DashboardService} from "../bindings/github.com/corvines/outrider/dashboard";

const app = document.querySelector<HTMLDivElement>("#app")!;

app.innerHTML = `
  <div class="shell">
    <aside class="sidebar">
      <div class="brand">
        <div><h1>Outrider</h1><small>local model server</small></div>
      </div>
      <nav class="nav" aria-label="Dashboard sections">
        <button class="active" type="button" data-target="overview">Overview</button>
        <button type="button" data-target="models-panel">Models</button>
        <button type="button" data-target="performance-panel">Performance</button>
        <button type="button" data-target="logs-panel">Logs</button>
      </nav>
      <div class="sidebar-footer">dashboard beta</div>
    </aside>
    <main class="content">
      <div class="topline">
        <div><div class="eyebrow">Outrider / Overview</div><h2 class="title">Serving status</h2></div>
        <button id="refresh" class="refresh" type="button">Refresh</button>
      </div>
      <section id="overview" class="status-card">
        <div class="status-row"><span id="status-dot" class="status-dot"></span><span id="status-label" class="status-label">Checking gateway…</span></div>
        <div id="status-detail" class="status-detail">Connecting to the local Outrider gateway</div>
        <div class="status-actions"><button id="stop-model" class="refresh" type="button" disabled>Stop model</button><span id="action-status" class="action-status"></span></div>
      </section>
      <section class="grid">
        <article class="card card-wide"><div class="card-title">Active model</div><div id="model" class="card-value">—</div><div id="model-note" class="card-note">No model loaded</div></article>
        <article id="performance-panel" class="card"><div class="card-title">Model memory</div><div id="memory" class="card-value">—</div><svg id="memory-chart" class="sparkline" viewBox="0 0 160 36" role="img" aria-label="Resident memory trend"><polyline /></svg><div class="card-note">resident set</div></article>
        <article class="card"><div class="card-title">Context</div><div id="context" class="card-value">—</div><div class="card-note">loaded model window</div></article>
        <article class="card card-wide"><div class="card-title">Gateway</div><div class="metric"><span>Endpoint</span><span id="endpoint">—</span></div><div class="metric"><span>Last updated</span><span id="updated">—</span></div></article>
        <article class="card"><div class="card-title">Advertised models</div><div id="model-count" class="card-value">—</div><div class="card-note">available to clients</div></article>
        <article id="models-panel" class="card card-wide"><div class="card-title">Model catalog</div><div id="models" class="model-list"><div class="empty">Loading catalog…</div></div></article>
        <article id="logs-panel" class="card card-wide"><div class="card-title">Logs</div><div class="empty">Gateway logs are available from the Outrider CLI. Live log streaming is next.</div></article>
      </section>
    </main>
  </div>
`;

const element = <T extends Element>(id: string) => document.getElementById(id)! as unknown as T;
const dot = element<HTMLSpanElement>("status-dot");
const label = element<HTMLSpanElement>("status-label");
const detail = element<HTMLDivElement>("status-detail");
const stopModel = element<HTMLButtonElement>("stop-model");
const actionStatus = element<HTMLSpanElement>("action-status");
const model = element<HTMLDivElement>("model");
const modelNote = element<HTMLDivElement>("model-note");
const memory = element<HTMLDivElement>("memory");
const memoryChart = element<SVGSVGElement>("memory-chart");
const context = element<HTMLDivElement>("context");
const endpoint = element<HTMLSpanElement>("endpoint");
const updated = element<HTMLSpanElement>("updated");
const modelCount = element<HTMLDivElement>("model-count");
const models = element<HTMLDivElement>("models");
const navButtons = document.querySelectorAll<HTMLButtonElement>(".nav button[data-target]");
let memorySamples: number[] = [];

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

function renderOffline(error: string) {
  dot.className = "status-dot offline";
  label.textContent = "Gateway offline";
  detail.textContent = error;
  model.textContent = "—";
  modelNote.textContent = "Start Outrider to load a model";
  memory.textContent = "—";
  memorySamples = [];
  memoryChart.querySelector("polyline")?.setAttribute("points", "");
  context.textContent = "—";
  endpoint.textContent = "—";
  updated.textContent = "—";
  modelCount.textContent = "—";
  models.innerHTML = `<div class="empty">No catalog available while Outrider is offline.</div>`;
  stopModel.disabled = true;
  actionStatus.textContent = "";
}

async function refresh() {
  try {
    const snapshot = await DashboardService.Snapshot();
    renderSnapshot(snapshot);
  } catch (error) {
    renderOffline(String(error));
  }
}

function renderSnapshot(snapshot: Awaited<ReturnType<typeof DashboardService.Snapshot>>) {
  if (snapshot.error) { renderOffline(snapshot.error); return; }
  const healthy = snapshot.gatewayHealth === "ok";
  const legacy = snapshot.gatewayHealth === "legacy";
  dot.className = `status-dot ${healthy ? "ok" : legacy ? "legacy" : ""}`;
  label.textContent = healthy ? "Gateway healthy" : legacy ? "Gateway connected (legacy)" : "Gateway unavailable";
  detail.textContent = legacy
    ? "Model catalog is available; update Outrider to enable controls"
    : snapshot.model.preset ? `${snapshot.model.preset} · ${snapshot.model.kind}` : "No model loaded";
  model.textContent = snapshot.model.preset || "No model loaded";
  modelNote.textContent = snapshot.model.startedAt ? `started ${new Date(snapshot.model.startedAt).toLocaleString()}` : "Ready for a model";
  const residentBytes = snapshot.model.residentBytes ?? 0;
  memory.textContent = formatBytes(residentBytes);
  renderMemoryChart(residentBytes);
  endpoint.textContent = snapshot.gatewayEndpoint || "—";
  updated.textContent = snapshot.updatedAt ? new Date(snapshot.updatedAt).toLocaleTimeString() : "—";
  const catalog = snapshot.models ?? [];
  modelCount.textContent = `${catalog.length}`;
  const activeModel = catalog.find((entry) => entry.id === snapshot.model.preset);
  context.textContent = formatContext(activeModel?.context ?? 0);
  stopModel.disabled = !healthy || snapshot.model.kind !== "running";
  models.innerHTML = catalog.length ? catalog.map((entry) => `
    <div class="model"><div><strong>${escapeHTML(entry.id)}</strong><br><small>${formatContext(entry.context)} context · ${escapeHTML(entry.quantization || "unknown quant")}</small></div><button class="model-action" type="button" data-model="${escapeHTML(entry.id)}" ${healthy ? "" : "disabled"}>${entry.id === snapshot.model.preset ? "Loaded" : healthy ? "Load" : "Update required"}</button></div>
  `).join("") : `<div class="empty">No runnable models advertised.</div>`;
}

let actionInFlight = false;

function setActionBusy(busy: boolean) {
  actionInFlight = busy;
  stopModel.disabled = busy;
  models.querySelectorAll<HTMLButtonElement>("button[data-model]").forEach((button) => { button.disabled = busy; });
}

async function runAction(message: string, action: () => Promise<Awaited<ReturnType<typeof DashboardService.Snapshot>>>) {
  if (actionInFlight) return;
  actionStatus.textContent = message;
  setActionBusy(true);
  try {
    const snapshot = await action();
    renderSnapshot(snapshot);
    actionStatus.textContent = snapshot.error ? "Action failed" : "Updated";
  } catch (error) {
    actionStatus.textContent = String(error);
  } finally {
    setActionBusy(false);
  }
}

function renderMemoryChart(bytes: number) {
  if (!bytes) return;
  memorySamples = [...memorySamples, bytes].slice(-24);
  const minimum = Math.min(...memorySamples);
  const maximum = Math.max(...memorySamples);
  const range = maximum - minimum || 1;
  const points = memorySamples.map((sample, index) => {
    const x = memorySamples.length === 1 ? 80 : (index / (memorySamples.length - 1)) * 160;
    const y = 33 - ((sample - minimum) / range) * 28;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(" ");
  memoryChart.querySelector("polyline")?.setAttribute("points", points);
}

function escapeHTML(value: string) {
  return value.replace(/[&<>'"]/g, (character) => ({"&":"&amp;", "<":"&lt;", ">":"&gt;", "'":"&#39;", "\"":"&quot;"})[character]!);
}

element<HTMLButtonElement>("refresh").addEventListener("click", refresh);
navButtons.forEach((button) => button.addEventListener("click", () => {
  navButtons.forEach((candidate) => candidate.classList.toggle("active", candidate === button));
  document.getElementById(button.dataset.target || "overview")?.scrollIntoView({behavior: "smooth", block: "start"});
}));
stopModel.addEventListener("click", () => void runAction("Stopping model…", () => DashboardService.StopModel()));
models.addEventListener("click", (event) => {
  if (!(event.target instanceof HTMLElement)) return;
  const button = event.target.closest<HTMLButtonElement>("button[data-model]");
  const modelID = button?.dataset.model;
  if (modelID) void runAction(`Loading ${modelID}…`, () => DashboardService.LoadModel(modelID));
});
void refresh();
window.setInterval(() => void refresh(), 3000);
