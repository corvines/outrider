import {describe, it, expect} from "bun:test";
import {setupState} from "../src/first_run";
import type {SetupSnapshot} from "../src/first_run";

const fresh = (): SetupSnapshot => ({gatewayHealth: "ok", model: {kind: "stopped", preset: ""}, models: [{id: "ling3-tiny", cached: false, sizeBytes: 4800000000}]});

describe("first run controls", () => {
  it("offers a download only when there is no usable cache", () => {
    const result = setupState(fresh(), false);
    expect(result.needsDownload).toBe(true);
    expect(result.canChat).toBe(true);
    expect(result.chatLabel).toContain("Download");
    expect(result.sizeBytes).toBe(4800000000);
  });
  it("reuses other cached models but not arbitrary downloaded files", () => {
    const snapshot = fresh();
    snapshot.models!.push({id: "file", custom: true, cached: true});
    expect(setupState(snapshot, false).needsDownload).toBe(true);
    snapshot.models!.push({id: "other", cached: true});
    expect(setupState(snapshot, false).candidate).toBe("other");
    expect(setupState(snapshot, false).needsDownload).toBe(false);
  });
  it("keeps the active model", () => {
    const snapshot = fresh();
    snapshot.model = {kind: "running", preset: "active", health: true};
    expect(setupState(snapshot, false).candidate).toBe("active");
    expect(setupState(snapshot, false).chatLabel).toBe("Chat");
  });
  it("offers retry offline and disables chat", () => {
    const snapshot = {...fresh(), gatewayHealth: "offline", serverError: "failed"};
    expect(setupState(snapshot, false).canChat).toBe(false);
    expect(setupState(snapshot, false).canStart).toBe(true);
    expect(setupState(snapshot, false).startLabel).toContain("Retry");
  });
  it("allows recovery after error or pause, not during an active load", () => {
    for (const phase of ["error", "paused", "downloading"]) {
      const snapshot = {...fresh(), loading: {phase}};
      expect(setupState(snapshot, false).canChat).toBe(phase !== "downloading");
      expect(setupState(snapshot, true).canStop).toBe(true);
    }
  });
  it("blocks duplicate chat and lifecycle actions", () => {
    expect(setupState(fresh(), true).canChat).toBe(false);
    expect(setupState({...fresh(), serverAction: "starting"}, false).canStop).toBe(false);
  });
});
