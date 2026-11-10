import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentModel, LauncherModels } from "./launcher-models";
import { canSwitchModel, isCurrentModel, isModelCommand, modelBusy, onModelPicker, pickerKey, pickerPatch, requestModelPicker, sessionLauncher, effortTabs } from "./model-switch";
import type { Session } from "./types";

const session = (extra: Partial<Session> = {}): Session => ({ machine_id: "m", session_id: "s", status: "idle", agent: "claude", ...extra }) as Session;
const all = ["low", "medium", "high", "xhigh", "max"];
const models: AgentModel[] = [
  { id: "default", name: "Default", default: true, resolved: "claude-opus-5-5", efforts: all },
  { id: "opus", name: "Opus 5.5", resolved: "claude-opus-5-5", efforts: all },
  { id: "haiku", name: "Haiku 5.5", resolved: "claude-haiku-5-5" },
  { id: "gateway/glm", name: "glm", efforts: ["low", "high"] },
];
const list: LauncherModels = { agent: "claude", models };
const key = (k: string, extra: Record<string, unknown> = {}) => ({ key: k, ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...extra });

describe("which sessions can switch model", () => {
  it("is a Claude Code terminal session that takes messages", () => {
    expect(canSwitchModel(session())).toBe(true);
    expect(canSwitchModel(session({ agent: undefined }))).toBe(true);
  });

  it("is not Codex, a runtime's, a Persona conversation, an app-server thread or an offline session", () => {
    expect(canSwitchModel(undefined)).toBe(false);
    expect(canSwitchModel(session({ agent: "codex" }))).toBe(false);
    expect(canSwitchModel(session({ agent: "traex" }))).toBe(false);
    expect(canSwitchModel(session({ assistant: true }))).toBe(false);
    expect(canSwitchModel(session({ source_kind: "codex_app_server", capabilities: { send: true } as Session["capabilities"] }))).toBe(false);
    expect(canSwitchModel(session({ status: "offline" }))).toBe(false);
  });

  it("honors explicit runtime model capability and its launcher", () => {
    const agy = session({ agent: "agy", source_kind: "runtime", launcher: "agy", capabilities: { send: true, model: true } as Session["capabilities"] });
    expect(canSwitchModel(agy)).toBe(true);
    expect(sessionLauncher(agy)).toBe("agy");
    expect(canSwitchModel({ ...agy, capabilities: { ...agy.capabilities!, model: false } })).toBe(false);
    expect(canSwitchModel({ ...agy, status: "offline" })).toBe(false);
    expect(canSwitchModel({ ...agy, assistant: true })).toBe(false);
  });

  it("is busy while the agent works or waits on you", () => {
    expect(modelBusy(session())).toBe("");
    expect(modelBusy(session({ status: "working" }))).toContain("正在工作");
    expect(modelBusy(session({ state: "needs_you" }))).toContain("正在工作");
  });

  it("asks the launcher the session runs with, plain claude without one", () => {
    expect(sessionLauncher({ launcher: "gateway" })).toBe("gateway");
    expect(sessionLauncher({})).toBe("claude");
  });
});

describe("the current model", () => {
  it("matches the list's id or what it stands for, ignoring case", () => {
    expect(isCurrentModel(models[1], "opus")).toBe(true);
    expect(isCurrentModel(models[1], "Claude-Opus-5-5")).toBe(true);
    expect(isCurrentModel(models[2], "claude-opus-5-5")).toBe(false);
    expect(isCurrentModel(models[1], undefined)).toBe(false);
    expect(isCurrentModel(models[1], "")).toBe(false);
  });
});

describe("the popover's keys", () => {
  const counts = { models: 4, efforts: 5 };

  it("moves through the models with up and down, stopping at the ends", () => {
    expect(pickerKey({ model: 1, effort: 2 }, counts, key("ArrowDown")).state).toEqual({ model: 2, effort: 2 });
    expect(pickerKey({ model: 1, effort: 2 }, counts, key("ArrowUp")).state).toEqual({ model: 0, effort: 2 });
    expect(pickerKey({ model: 3, effort: 2 }, counts, key("ArrowDown")).state.model).toBe(3);
    expect(pickerKey({ model: 0, effort: 2 }, counts, key("ArrowUp")).state.model).toBe(0);
    expect(pickerKey({ model: -1, effort: 2 }, counts, key("ArrowDown")).state.model).toBe(0);
  });

  it("moves through the effort tabs with left and right", () => {
    expect(pickerKey({ model: 1, effort: 2 }, counts, key("ArrowRight")).state.effort).toBe(3);
    expect(pickerKey({ model: 1, effort: 2 }, counts, key("ArrowLeft")).state.effort).toBe(1);
    expect(pickerKey({ model: 1, effort: 4 }, counts, key("ArrowRight")).state.effort).toBe(4);
    expect(pickerKey({ model: 1, effort: 0 }, counts, key("ArrowLeft")).state.effort).toBe(0);
    expect(pickerKey({ model: 1, effort: -1 }, { models: 4, efforts: 0 }, key("ArrowRight")).state.effort).toBe(-1);
  });

  it("applies with Enter and cancels with Esc", () => {
    expect(pickerKey({ model: 1, effort: 2 }, counts, key("Enter"))).toMatchObject({ action: "apply", handled: true });
    expect(pickerKey({ model: 1, effort: 2 }, counts, key("Escape"))).toMatchObject({ action: "cancel", handled: true });
  });

  it("leaves other keys, chords and an input method's keys alone", () => {
    for (const k of [key("a"), key("ArrowDown", { ctrlKey: true }), key("Enter", { metaKey: true }), key("Enter", { isComposing: true }), key("ArrowUp", { keyCode: 229 })]) {
      expect(pickerKey({ model: 1, effort: 2 }, counts, k).handled).toBe(false);
    }
  });
});

describe("the effort tabs of a model", () => {
  it("are the model's own efforts, in the order of the tabs", () => {
    expect(effortTabs(list, models[3], all)).toEqual(["low", "high"]);
    expect(effortTabs(list, models[1], all)).toEqual(all);
  });

  it("are none for a model that takes no effort, and all while there is no list to go by", () => {
    expect(effortTabs(list, models[2], all)).toEqual([]);
    expect(effortTabs(null, undefined, all)).toEqual(all);
    expect(effortTabs({ agent: "claude", models: [] }, models[1], all)).toEqual(all);
  });
});

describe("what applying sends", () => {
  it("sends only what differs from the session", () => {
    expect(pickerPatch({ model: models[1], current: "claude-haiku-5-5", effort: "high", currentEffort: "high" })).toEqual({ model: "opus" });
    expect(pickerPatch({ model: models[1], current: "opus", effort: "max", currentEffort: "high" })).toEqual({ effort: "max" });
    expect(pickerPatch({ model: models[2], current: "opus", effort: "low", currentEffort: "high" })).toEqual({ model: "haiku", effort: "low" });
    expect(pickerPatch({ model: models[1], current: "opus", effort: "high", currentEffort: "high" })).toEqual({});
  });

  it("sends no model when none is chosen or the model cannot be switched", () => {
    expect(pickerPatch({ model: undefined, current: "opus", effort: "", currentEffort: "high" })).toEqual({});
  });
});

describe("the /model command", () => {
  it("is /model alone", () => {
    expect(isModelCommand("/model")).toBe(true);
    expect(isModelCommand("/model ")).toBe(true);
    expect(isModelCommand("  /model  ")).toBe(true);
    expect(isModelCommand("/model opus")).toBe(false);
    expect(isModelCommand("/models")).toBe(false);
    expect(isModelCommand("use /model")).toBe(false);
  });
});

describe("opening the popover from the composer", () => {
  afterEach(() => vi.restoreAllMocks());

  it("reaches the conversation's chip and nobody else's", () => {
    const open = vi.fn();
    const off = onModelPicker("m/s", open);
    expect(requestModelPicker("m/other")).toBe(false);
    expect(requestModelPicker("m/s")).toBe(true);
    expect(open).toHaveBeenCalledTimes(1);
    off();
    expect(requestModelPicker("m/s")).toBe(false); // an agent without a switch: the composer sends as before
  });

  it("keeps the newer chip when an older one unsubscribes late", () => {
    const a = vi.fn(), b = vi.fn();
    const offA = onModelPicker("m/s", a);
    const offB = onModelPicker("m/s", b);
    offA();
    expect(requestModelPicker("m/s")).toBe(true);
    expect(b).toHaveBeenCalled();
    offB();
  });
});
