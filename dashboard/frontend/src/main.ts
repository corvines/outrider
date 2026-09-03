import {DashboardService} from "../bindings/github.com/corvines/outrider/dashboard";

const app = document.querySelector<HTMLDivElement>("#app")!;

app.innerHTML = `
  <div class="shell">
    <aside class="sidebar">
      <div class="brand"><h1>Outrider</h1><small>local model server</small></div>
      <nav class="nav" aria-label="Dashboard sections">
        <button class="active" type="button" data-target="overview">Overview</button>
        <button type="button" data-target="models">Models</button>
        <button type="button" data-target="performance">Performance</button>
        <button type="button" data-target="logs">Logs</button>
      </nav>
      <div class="sidebar-footer">dashboard beta</div>
    </aside>
    <main class="content">
      <div class="topline">
        <div><div id="eyebrow" class="eyebrow">Outrider / Overview</div><h2 id="page-title" class="title">Serving status</h2></div>
        <div class="top-actions"><span id="action-status" class="action-status"></span><button id="refresh" class="refresh" type="button">Refresh</button></div>
      </div>

      <div id="page-overview" class="page">
        <section class="status-card">
          <div class="status-row"><span id="status-dot" class="status-dot"></span><span id="status-label" class="status-label">Checking gateway…</span></div>
          <div id="status-detail" class="status-detail">Connecting to the local Outrider gateway</div>
          <div class="status-actions"><button id="stop-model" class="refresh" type="button" disabled>Stop model</button></div>
        </section>
        <section class="grid">
          <article class="card card-wide"><div class="card-title">Active model</div><div id="model" class="card-value">—</div><div id="model-note" class="card-note">No model loaded</div></article>
          <article class="card"><div class="card-title">Model memory</div><div id="memory" class="card-value">—</div><svg id="memory-chart" class="sparkline" viewBox="0 0 180 54" role="img" aria-label="Resident memory trend"><line class="chart-axis" x1="24" y1="8" x2="24" y2="42" /><line class="chart-axis" x1="24" y1="42" x2="176" y2="42" /><line class="chart-grid" x1="24" y1="8" x2="176" y2="8" /><polyline /><text class="chart-label" data-axis-high x="0" y="11">—</text><text class="chart-label" data-axis-low x="0" y="45">—</text><text class="chart-label" data-axis-old x="24" y="53">older</text><text class="chart-label" data-axis-now x="153" y="53">now</text></svg><div class="card-note">resident set</div></article>
          <article class="card"><div class="card-title">Context</div><div id="context" class="card-value">—</div><div class="card-note">loaded model window</div></article>
          <article class="card card-wide"><div class="card-title">Gateway</div><div class="metric"><span>Endpoint</span><span id="endpoint">—</span></div><div class="metric"><span>Last updated</span><span id="updated">—</span></div></article>
          <article class="card"><div class="card-title">Advertised models</div><div id="model-count" class="card-value">—</div><div class="card-note">available to clients</div></article>
        </section>
      </div>

      <div id="page-models" class="page hidden">
        <div class="page-intro"><div class="eyebrow">Model catalog</div><p>Choose which local model Outrider should serve.</p></div>
        <article class="card card-full"><div id="models" class="model-list"><div class="empty">Loading catalog…</div></div></article>
      </div>

      <div id="page-performance" class="page hidden">
        <div class="page-intro"><div class="eyebrow">Performance</div><p>Runtime signals from the resident model and gateway.</p></div>
        <section class="grid">
          <article class="card card-wide"><div class="card-title">Resident memory</div><div id="performance-memory" class="card-value">—</div><svg id="performance-chart" class="sparkline" viewBox="0 0 180 54" role="img" aria-label="Resident memory trend"><line class="chart-axis" x1="24" y1="8" x2="24" y2="42" /><line class="chart-axis" x1="24" y1="42" x2="176" y2="42" /><line class="chart-grid" x1="24" y1="8" x2="176" y2="8" /><polyline /><text class="chart-label" data-axis-high x="0" y="11">—</text><text class="chart-label" data-axis-low x="0" y="45">—</text><text class="chart-label" data-axis-old x="24" y="53">older</text><text class="chart-label" data-axis-now x="153" y="53">now</text></svg><div class="card-note">sampled while the dashboard is open</div></article>
          <article class="card"><div class="card-title">Context window</div><div id="performance-context" class="card-value">—</div><div class="card-note">active model window</div></article>
          <article class="card card-wide"><div class="card-title">Active model</div><div id="performance-model" class="card-value">—</div><div class="card-note">resident model</div></article>
          <article class="card"><div class="card-title">Gateway</div><div id="performance-endpoint" class="card-value">—</div><div id="performance-updated" class="card-note">—</div></article>
        </section>
      </div>

      <div id="page-logs" class="page hidden">
        <div class="page-intro"><div class="eyebrow">Logs</div><p>Inspect the gateway when a model load or request needs diagnosis.</p></div>
        <article class="card card-full"><div class="card-title">Gateway log</div><pre id="logs-content" class="log-empty">Live log streaming is not wired yet. Use <code>outrider logs</code> for the current run.</pre></article>
      </div>
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
const eyebrow = element<HTMLDivElement>("eyebrow");
const pageTitle = element<HTMLHeadingElement>("page-title");
const navButtons = document.querySelectorAll<HTMLButtonElement>(".nav button[data-target]");
const pages = document.querySelectorAll<HTMLElement>(".page");
let memorySamples: number[] = [];

const pageMeta: Record<string, {eyebrow: string; title: string}> = {
  overview: {eyebrow: "Outrider / Overview", title: "Serving status"},
  models: {eyebrow: "Outrider / Models", title: "Model catalog"},
  performance: {eyebrow: "Outrider / Performance", title: "Runtime signals"},
  logs: {eyebrow: "Outrider / Logs", title: "Gateway logs"},
};

function showPage(target: string) {
  pages.forEach((page) => page.classList.toggle("hidden", page.id !== `page-${target}`));
  navButtons.forEach((button) => button.classList.toggle("active", button.dataset.target === target));
  const meta = pageMeta[target] || pageMeta.overview;
  eyebrow.textContent = meta.eyebrow;
  pageTitle.textContent = meta.title;
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
  stopModel.disabled = true;
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
    ? "Catalog is read-only; restart Outrider from the current build to enable controls"
    : snapshot.model.preset ? `${snapshot.model.preset} · ${snapshot.model.kind}` : "No model loaded";
  setModelText(snapshot.model.preset || "No model loaded");
  modelNote.textContent = snapshot.model.startedAt ? `started ${new Date(snapshot.model.startedAt).toLocaleString()}` : "Ready for a model";
  const residentBytes = snapshot.model.residentBytes ?? 0;
  setMemoryText(formatBytes(residentBytes));
  renderMemoryChart(residentBytes);
  endpoint.textContent = snapshot.gatewayEndpoint || "—";
  updated.textContent = snapshot.updatedAt ? new Date(snapshot.updatedAt).toLocaleTimeString() : "—";
  performanceEndpoint.textContent = snapshot.gatewayEndpoint || "—";
  performanceUpdated.textContent = snapshot.updatedAt ? `updated ${new Date(snapshot.updatedAt).toLocaleTimeString()}` : "—";
  const catalog = snapshot.models ?? [];
  modelCount.textContent = `${catalog.length}`;
  const activeModel = catalog.find((entry) => entry.id === snapshot.model.preset);
  setContextText(formatContext(activeModel?.context ?? 0));
  stopModel.disabled = !healthy || snapshot.model.kind !== "running";
  models.innerHTML = catalog.length ? catalog.map((entry) => `
    <div class="model"><div><strong>${escapeHTML(entry.id)}</strong><br><small>${formatContext(entry.context)} context · ${escapeHTML(entry.quantization || "unknown quant")}</small></div><button class="model-action" type="button" data-model="${escapeHTML(entry.id)}" ${healthy ? "" : "disabled"}>${entry.id === snapshot.model.preset ? "Loaded" : healthy ? "Load" : "Read-only"}</button></div>
  `).join("") : `<div class="empty">No runnable models advertised.</div>`;
}

let actionInFlight = false;

function setActionBusy(busy: boolean) {
  actionInFlight = busy;
  stopModel.disabled = busy;
  models.querySelectorAll<HTMLButtonElement>("button[data-model]").forEach((button) => { button.disabled = busy || button.dataset.model === undefined; });
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
    clearChart(memoryChart);
    clearChart(performanceChart);
    return;
  }
  const range = maximum - minimum || 1;
  const points = memorySamples.map((sample, index) => {
    const x = memorySamples.length === 1 ? 100 : 24 + (index / (memorySamples.length - 1)) * 152;
    const y = 42 - ((sample - minimum) / range) * 34;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(" ");
  memoryChart.querySelector("polyline")?.setAttribute("points", points);
  performanceChart.querySelector("polyline")?.setAttribute("points", points);
}

function clearChart(chart: SVGSVGElement) {
  chart.querySelector("polyline")?.setAttribute("points", "");
}

function updateChartLabels(chart: SVGSVGElement, minimum: number, maximum: number) {
  chart.querySelector("[data-axis-high]")!.textContent = formatBytes(maximum);
  chart.querySelector("[data-axis-low]")!.textContent = formatBytes(minimum);
}

function escapeHTML(value: string) {
  return value.replace(/[&<>'"]/g, (character) => ({"&":"&amp;", "<":"&lt;", ">":"&gt;", "'":"&#39;", "\"":"&quot;"})[character]!);
}

navButtons.forEach((button) => button.addEventListener("click", () => showPage(button.dataset.target || "overview")));
element<HTMLButtonElement>("refresh").addEventListener("click", refresh);
stopModel.addEventListener("click", () => void runAction("Stopping model…", () => DashboardService.StopModel()));
models.addEventListener("click", (event) => {
  if (!(event.target instanceof HTMLElement)) return;
  const button = event.target.closest<HTMLButtonElement>("button[data-model]");
  const modelID = button?.dataset.model;
  if (modelID) void runAction(`Loading ${modelID}…`, () => DashboardService.LoadModel(modelID));
});

showPage("overview");
void refresh();
window.setInterval(() => void refresh(), 3000);
