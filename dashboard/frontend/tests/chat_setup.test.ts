import {describe, expect, it} from "bun:test";
import {chatSetup, downloadGreetings} from "../src/chat_setup";
import type {ChatSetupSnapshot} from "../src/chat_setup";

const fresh = (): ChatSetupSnapshot => ({gatewayHealth: "ok", model: {kind: "stopped", preset: ""}, models: [{id: "ling3-tiny", cached: false, sizeBytes: 4823894944}]});

describe("chat model setup", () => {
  it("waits for the catalog before offering a download", () => {
    expect(chatSetup(undefined, false, 0).enabled).toBe(false);
    expect(chatSetup({...fresh(), models: []}, false, 0).enabled).toBe(false);
  });
  it("offers explicit download consent with size and offline expectations", () => {
    const state = chatSetup(fresh(), false, 0);
    expect(state.kind).toBe("download");
    expect(state.action).toBe("prepare");
    expect(state.label).toContain("Download starter model");
    expect(state.detail).toContain("4.8 GB");
    expect(state.detail).toContain("Internet");
    expect(state.detail).toContain("offline");
    expect(state.ready).toBe(false);
  });
  it("has distinct messages without changing the download action", () => {
    expect(new Set(downloadGreetings).size).toBe(downloadGreetings.length);
    for (let index = 0; index < downloadGreetings.length * 2; index++) {
      const first = chatSetup(fresh(), false, index);
      expect(first.headline).toBe(downloadGreetings[index % downloadGreetings.length]);
      expect(chatSetup(fresh(), false, index)).toEqual(first);
      expect(first.action).toBe("prepare");
    }
  });
  it("offers load instead of download for existing weights", () => {
    const snapshot = fresh();
    snapshot.models!.push({id: "another-model", cached: true});
    const state = chatSetup(snapshot, false, 0);
    expect(state.kind).toBe("cached");
    expect(state.headline).toContain("another-model is already here");
    expect(state.label).toContain("Load");
    expect(state.detail).toContain("not download its weights again");
    expect(state.ready).toBe(false);
  });
  it("unlocks chat only for a healthy loaded model", () => {
    const snapshot = fresh();
    snapshot.model = {kind: "running", preset: "ling3-tiny", health: true};
    expect(chatSetup(snapshot, false, 0).ready).toBe(true);
    snapshot.model.health = false;
    expect(chatSetup(snapshot, false, 0).ready).toBe(false);
  });
  it("offers server recovery without pretending a download is needed", () => {
    const state = chatSetup({...fresh(), gatewayHealth: "offline", serverError: "Port busy"}, false, 0);
    expect(state.kind).toBe("server");
    expect(state.action).toBe("server");
    expect(state.detail).toBe("Port busy");
    expect(state.label).toContain("Retry");
  });
  it("supports progress, pause, and retry without enabling send", () => {
    const active = chatSetup({...fresh(), loading: {phase: "downloading", downloaded: 50, total: 100}}, true, 0);
    expect(active.headline).toContain("50%");
    expect(active.pause).toBe(true);
    expect(active.enabled).toBe(false);
    for (const phase of ["paused", "error"]) {
      const state = chatSetup({...fresh(), loading: {phase}}, false, 0);
      expect(state.enabled).toBe(true);
      expect(state.ready).toBe(false);
      expect(state.kind).toBe(phase);
    }
  });
  it("blocks repeated preparation actions", () => {
    expect(chatSetup(fresh(), true, 0).enabled).toBe(false);
    expect(chatSetup({...fresh(), serverAction: "starting"}, false, 0).enabled).toBe(false);
  });
});
