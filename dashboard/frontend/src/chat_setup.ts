import {setupState} from "./first_run";
import type {SetupSnapshot} from "./first_run";

export const downloadGreetings = [
  "Before I can chat, I need a model to do the thinking. Shall we download one?",
  "Almost ready to say hello. I just need a model on this Mac first.",
  "First a model, then a world of questions. Let's download one to get ready.",
  "I've got the chat window. Now I need the model. Let's download one to get started.",
];

export type ChatSetupSnapshot = SetupSnapshot & {
  error?: string;
  loading?: {phase: string; model?: string; error?: string; downloaded?: number; total?: number} | null;
};

export function chatSetup(snapshot: ChatSetupSnapshot | undefined, busy: boolean, greeting: number) {
  const view = {kind: "checking", headline: "Checking who's home...", detail: "Looking for a local model.", action: "", label: "", enabled: false, pause: false, ready: false};
  if (!snapshot) return view;
  const setup = setupState(snapshot, busy);
  if (!setup.ready || setup.serverBusy) {
    return {...view, kind: "server", headline: setup.serverBusy ? setup.startLabel : "The chat room is ready. Let's wake up the server.",
      detail: snapshot.serverError || "Your downloaded models stay on this Mac when the server is stopped.",
      action: "server", label: setup.startLabel, enabled: setup.canStart};
  }
  if (setup.loading) {
    const loading = snapshot.loading!;
    const percent = loading.total ? ` (${Math.min(100, Math.floor((loading.downloaded || 0) / loading.total * 100))}%)` : "";
    return {...view, kind: "loading", headline: `Getting ${loading.model || setup.candidate} ready${percent}...`,
      detail: "You can leave this page open. Chat will be ready when the model is loaded.", pause: true};
  }
  const failed = snapshot.loading?.phase === "error";
  const paused = snapshot.loading?.phase === "paused";
  if (snapshot.model.kind === "running" && snapshot.model.health === true && !failed && !paused) {
    return {...view, kind: "ready", ready: true, headline: "", detail: ""};
  }
  const headline = paused ? "Taking a breather. Ready to pick up where we left off?"
    : failed ? "A small hiccup. Let's try getting your model ready again."
    : setup.needsDownload ? downloadGreetings[greeting % downloadGreetings.length]
    : `${setup.candidate} is already here. Let's wake it up for a chat.`;
  const size = setup.sizeBytes ? ` (${(setup.sizeBytes / 1e9).toFixed(1)} GB)` : "";
  const detail = setup.needsDownload
    ? `Ling is the starter model${size}. Internet is needed for this download and any missing runtime files. Once ready, you can chat offline.`
    : "We'll reuse the downloaded model, not download its weights again. Missing runtime files may still need internet.";
  return {...view, kind: paused ? "paused" : failed ? "error" : setup.needsDownload ? "download" : "cached", headline,
    detail: snapshot.loading?.error || snapshot.error || detail,
    action: "prepare", label: setup.chatLabel, enabled: setup.canChat};
}
