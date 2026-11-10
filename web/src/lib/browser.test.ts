import { describe, expect, it } from "vitest";
import { browserView, browserWatching, readySteps, type MachineBrowser } from "./browser";

const on = (patch: Partial<MachineBrowser> = {}): MachineBrowser => ({ enabled: true, ...patch });

describe("浏览器（ego lite）区块", () => {
  it("关着时什么都不显示，离线时说连上后才能用", () => {
    expect(browserView({ enabled: false, installed: true }, true).kind).toBe("off");
    expect(browserView(on({ installed: true }), false).kind).toBe("offline");
  });

  it("没装时给安装，装失败时说原因并给重新安装", () => {
    expect(browserView(on(), true)).toMatchObject({ kind: "install", retry: false });
    const failed = browserView(on({ job: { kind: "install", state: "failed", error: "签名不对" } }), true);
    expect(failed).toMatchObject({ kind: "install", retry: true });
    expect(failed.kind === "install" && failed.text).toContain("签名不对");
  });

  it("安装中显示进度", () => {
    expect(browserView(on({ job: { kind: "install", state: "running", step: "正在下载" } }), true)).toEqual({ kind: "installing", text: "正在下载" });
  });

  it("装好没做首次设置时给四步引导", () => {
    const v = browserView(on({ installed: true, screen_sharing: true, job: { kind: "open", state: "done" } }), true);
    expect(v.kind).toBe("setup");
    if (v.kind !== "setup") return;
    expect(v.steps.map((s) => s.id)).toEqual(["open", "import", "finish", "recheck"]);
  });

  it("做完设置但没跑通时说 node 给的原因", () => {
    expect(browserView(on({ installed: true, set_up: true, note: "ego-browser 没响应" }), true)).toEqual({ kind: "unready", text: "ego-browser 没响应" });
  });

  it("就绪时列出导入和使用统计，统计开着时可以再检查一次", () => {
    const v = browserView(on({ installed: true, set_up: true, ready: true, version: "0.6.2", analytics: "on" }), true);
    expect(v).toMatchObject({ kind: "ready", recheck: true });
    expect(v.kind === "ready" && v.text).toContain("0.6.2");
    expect(readySteps({ enabled: true, imported: true, analytics: "off" }).map((s) => [s.id, s.done])).toEqual([["setup", true], ["import", true], ["analytics", true]]);
    expect(readySteps({ enabled: true }).map((s) => s.id)).toEqual(["setup", "import"]);
  });

  it("任务在跑、或打开了 ego lite 在做首次设置时每 2 秒再问", () => {
    expect(browserWatching(on({ job: { kind: "install", state: "running" } }), true)).toBe(true);
    expect(browserWatching(on({ installed: true, job: { kind: "open", state: "done" } }), true)).toBe(true);
    expect(browserWatching(on({ installed: true, set_up: true, job: { kind: "open", state: "done" } }), true)).toBe(false);
    expect(browserWatching(on({ job: { kind: "install", state: "running" } }), false)).toBe(false);
    expect(browserWatching({ enabled: false, job: { kind: "install", state: "running" } }, true)).toBe(false);
  });
});
