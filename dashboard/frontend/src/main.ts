import {DashboardService} from "../bindings/github.com/corvines/outrider/dashboard";

const app = document.querySelector<HTMLDivElement>("#app")!;

app.innerHTML = `
  <div class="shell">
    <aside class="sidebar">
      <div class="brand">
        <div><h1>Outrider</h1><small>local model server</small></div>
      </div>
      <nav class="nav" aria-label="Dashboard sections">
        <button class="active" type="button">Overview</button>
        <button type="button">Models</button>
        <button type="button">Performance</button>
        <button type="button">Logs</button>
      </nav>
      <div class="sidebar-footer">dashboard beta</div>
    </aside>
    <main class="content">
      <div class="topline">
        <div><div class="eyebrow">Outrider / Overview</div><h2 class="title">Serving status</h2></div>
        <button id="refresh" class="refresh" type="button">Refresh</button>
      </div>
      <section class="status-card">
        <div class="status-row"><span id="status-dot" class="status-dot"></span><span id="status-label" class="status-label">Checking gateway…</span></div>
        <div id="status-detail" class="status-detail">Connecting to the local Outrider gateway</div>
      </section>
      <section class="grid">
        <article class="card card-wide"><div class="card-title">Active model</div><div id="model" class="card-value">—</div><div id="model-note" class="card-note">No model loaded</div></article>
        <article class="card"><div class="card-title">Model memory</div><div id="memory" class="card-value">—</div><svg id="memory-chart" class="sparkline" viewBox="0 0 160 36" role="img" aria-label="Resident memory trend"><polyline /></svg><div class="card-note">resident set</div></article>
        <article class="card"><div class="card-title">Context</div><div id="context" class="card-value">—</div><div class="card-note">loaded model window</div></article>
        <article class="card card-wide"><div class="card-title">Gateway</div><div class="metric"><span>Endpoint</span><span id="endpoint">—</span></div><div class="metric"><span>Last updated</span><span id="updated">—</span></div></article>
        <article class="card"><div class="card-title">Advertised models</div><div id="model-count" class="card-value">—</div><div class="card-note">available to clients</div></article>
        <article class="card card-wide"><div class="card-title">Model catalog</div><div id="models" class="model-list"><div class="empty">Loading catalog…</div></div></article>
      </section>
    </main>
  </div>
`;

const element = <T extends HTMLElement>(id: string) => document.getElementById(id)! as T;
const dot = element<HTMLSpanElement>("status-dot");
const label = element<HTMLSpanElement>("status-label");
const detail = element<HTMLDivElement>("status-detail");
const model = element<HTMLDivElement>("model");
const modelNote = element<HTMLDivElement>("model-note");
const memory = element<HTMLDivElement>("memory");
const memoryChart = element<SVGSVGElement>("memory-chart");
const context = element<HTMLDivElement>("context");
const endpoint = element<HTMLSpanElement>("endpoint");
const updated = element<HTMLSpanElement>("updated");
const modelCount = element<HTMLDivElement>("model-count");
const models = element<HTMLDivElement>("models");
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
}

async function refresh() {
  try {
    const snapshot = await DashboardService.Snapshot();
    if (snapshot.error) { renderOffline(snapshot.error); return; }
    const healthy = snapshot.gatewayHealth === "ok";
    dot.className = `status-dot ${healthy ? "ok" : ""}`;
    label.textContent = healthy ? "Gateway healthy" : "Gateway unavailable";
    detail.textContent = snapshot.model.preset ? `${snapshot.model.preset} · ${snapshot.model.kind}` : "No model loaded";
    model.textContent = snapshot.model.preset || "No model loaded";
    modelNote.textContent = snapshot.model.startedAt ? `started ${new Date(snapshot.model.startedAt).toLocaleString()}` : "Ready for a model";
    memory.textContent = formatBytes(snapshot.model.residentBytes);
    renderMemoryChart(snapshot.model.residentBytes);
    endpoint.textContent = snapshot.gatewayEndpoint || "—";
    updated.textContent = snapshot.updatedAt ? new Date(snapshot.updatedAt).toLocaleTimeString() : "—";
    const catalog = snapshot.models ?? [];
    modelCount.textContent = `${catalog.length}`;
    const activeModel = catalog.find((entry) => entry.id === snapshot.model.preset);
    context.textContent = formatContext(activeModel?.context ?? 0);
    models.innerHTML = catalog.length ? catalog.map((entry) => `
      <div class="model"><div><strong>${escapeHTML(entry.id)}</strong><br><small>${formatContext(entry.context)} context · ${escapeHTML(entry.quantization || "unknown quant")}</small></div><span class="pill">available</span></div>
    `).join("") : `<div class="empty">No runnable models advertised.</div>`;
  } catch (error) {
    renderOffline(String(error));
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
void refresh();
window.setInterval(() => void refresh(), 3000);
