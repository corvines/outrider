export type SetupSnapshot = {
  gatewayHealth: string;
  serverAction?: string;
  serverError?: string;
  loading?: {phase: string} | null;
  model: {kind: string; preset: string; health?: boolean | null};
  models: {id: string; cached: boolean; custom?: boolean; sizeBytes?: number}[] | null;
};

export function setupState(snapshot: SetupSnapshot, busy: boolean) {
  const ready = snapshot.gatewayHealth === "ok";
  const serverBusy = snapshot.serverAction === "starting" || snapshot.serverAction === "stopping";
  const loading = !!snapshot.loading && !["paused", "error"].includes(snapshot.loading.phase);
  const running = snapshot.model.kind === "running" && snapshot.model.health === true;
  const catalog = snapshot.models || [];
  const cached = catalog.filter((model) => model.cached && !model.custom);
  const candidate = running ? snapshot.model.preset
    : cached.find((model) => model.id === snapshot.model.preset)?.id
      || cached.find((model) => model.id === "ling3-tiny")?.id || cached[0]?.id;
  const needsDownload = !candidate;
  const starter = catalog.find((model) => model.id === "ling3-tiny");
  return {
    ready, serverBusy, loading, needsDownload,
    candidate: candidate || "ling3-tiny",
    sizeBytes: starter?.sizeBytes || 0,
    canChat: ready && !busy && !serverBusy && !loading && (!needsDownload || !!starter),
    canStart: !ready && !busy && !serverBusy,
    canStop: !serverBusy,
    startLabel: serverBusy ? (snapshot.serverAction === "starting" ? "Starting server..." : "Stopping server...")
      : snapshot.serverError ? "Retry server start" : "Start server",
    chatLabel: snapshot.loading?.phase === "paused" ? "Resume & Chat"
      : snapshot.loading?.phase === "error" ? "Retry & Chat"
      : needsDownload ? "Download starter model & Chat" : running ? "Chat" : "Load model & Chat",
  };
}
