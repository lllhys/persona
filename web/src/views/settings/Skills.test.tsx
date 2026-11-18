// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { initI18n, t } from "../../i18n";
import { setPairedToken } from "../../lib/api";
import { SkillsGroup } from "./Skills";

vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia;
});
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let root: Root, host: HTMLDivElement;
let writes: Record<string, unknown>[], listed: number, installed: boolean;
let hold: ((r: Response) => void) | undefined;
let fail: string | undefined;
let expectedManager: string;
const preview = {
  name: "example-skill", description: "<img src=x onerror=alert(1)> Synthetic resource.",
  destination: "/example/skills/example-skill", machine: "Example computer",
  files: 2, bytes: 120, reference: "preview-ref", environment: "environment-ref",
  expires_at: "2099-01-01T00:00:00Z",
};
const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
const flush = async () => { for (let n = 0; n < 8; n++) await act(async () => { await Promise.resolve(); }); };
const buttons = () => [...document.querySelectorAll<HTMLButtonElement>("button")];
const button = (key: string) => {
  const b = buttons().find((b) => b.textContent === t(key));
  expect(b, `button ${key}`).toBeTruthy();
  return b!;
};
const click = async (key: string) => { await act(async () => button(key).click()); await flush(); };
async function upload(size = 5) {
  const file = new File(["x".repeat(size)], "synthetic.zip", { type: "application/zip" });
  await act(async () => {
    const input = document.querySelector<HTMLInputElement>('input[type="file"]')!;
    expect(input).toBeTruthy();
    Object.defineProperty(input, "files", { configurable: true, value: [file] });
    input.dispatchEvent(new Event("change", { bubbles: true }));
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
  await flush();
}
beforeEach(async () => {
  await initI18n("en");
  writes = []; listed = 0; installed = false; hold = undefined; fail = undefined; expectedManager = "example-manager";
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input).split("?")[0];
    expect(new Headers(init?.headers).get("X-Persona-Manager")).toBe(expectedManager);
    if (path.endsWith("/skills")) {
      listed++;
      return json({ machine: "Example computer", skills: installed ? [{ name: "example-skill", source: "user" }] : [] });
    }
    if (path.endsWith("/skills/import")) {
      const body = JSON.parse(String(init?.body)); writes.push(body);
      expect(body.path).toBeUndefined(); expect(body.machine_id).toBeUndefined();
      if (hold) return await new Promise<Response>((resolve) => { hold = resolve; });
      if (fail) return json({ error: "private filesystem detail must not be rendered", code: fail }, 409);
      if (body.op === "install") { installed = true; return json({ ...preview, reference: "undo-ref" }); }
      if (body.op === "undo") { installed = false; return json({ name: preview.name, recovery: "/example/recovery" }); }
      return json(preview);
    }
    throw new Error(`Unexpected request: ${path}`);
  }));
  await act(async () => root.render(<SkillsGroup manager="example-manager" />));
  await flush();
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

it("previews untrusted text, confirms explicitly, refreshes the old list and offers scoped Undo", async () => {
  expect(host.textContent).toContain(t("settings-skills.empty"));
  await click("settings-skills.upload");
  await upload();
  expect(writes.map((w) => w.op)).toEqual(["preview"]);
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(preview.description);
  expect(document.querySelector('[role="dialog"] img')).toBeNull();
  expect(document.body.textContent).toContain(preview.destination);
  expect(document.body.textContent).toContain(t("settings-skills.shared"));
  expect(document.activeElement?.textContent).toBe(t("common.cancel"));
  await click("settings-skills.install");
  expect(writes[1]).toMatchObject({ op: "install", reference: "preview-ref", environment: "environment-ref" });
  expect(writes[1].archive).toBe(writes[0].archive);
  expect(listed).toBeGreaterThan(1);
  expect(document.body.textContent).toContain(t("settings-skills.installed", { name: preview.name }));
  expect(button("settings-skills.close")).toBeTruthy();
  await click("settings-skills.undo");
  expect(writes[2]).toMatchObject({ op: "undo", reference: "undo-ref" });
  expect(document.body.textContent).toContain(t("settings-skills.undone"));
  expect(installed).toBe(false);
});

it("says where the skills live in one footer line, not as rows of the list", async () => {
  expect(host.textContent).toContain(t("settings-skills.footer_where", { machine: "Example computer" }));
  expect(host.querySelectorAll(".divide-y > *")).toHaveLength(1);
});

it("checks a ZIP dropped on the drop zone like a chosen one", async () => {
  await click("settings-skills.upload");
  const zone = document.querySelector<HTMLElement>('[data-testid="skill-zip-drop"]');
  expect(zone).toBeTruthy();
  const file = new File(["x".repeat(5)], "synthetic.zip", { type: "application/zip" });
  const drag = (type: string) => {
    const e = new Event(type, { bubbles: true, cancelable: true });
    Object.defineProperty(e, "dataTransfer", { value: { files: [file], types: ["Files"] } });
    zone!.dispatchEvent(e);
    return e;
  };
  await act(async () => { expect(drag("dragover").defaultPrevented).toBe(true); });
  expect(zone!.className).toContain("border-accent");
  await act(async () => {
    expect(drag("drop").defaultPrevented).toBe(true);
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
  await flush();
  expect(writes.map((w) => w.op)).toEqual(["preview"]);
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(preview.destination);
});

it.each(["text/uri-list", "text/plain"])("cancels %s drags without highlighting or starting a ZIP check", async (type) => {
  await click("settings-skills.upload");
  const zone = document.querySelector<HTMLElement>('[data-testid="skill-zip-drop"]')!;
  for (const name of ["dragover", "drop"]) {
    const event = new Event(name, { bubbles: true, cancelable: true });
    Object.defineProperty(event, "dataTransfer", { value: { files: [], types: [type] } });
    await act(async () => { zone.dispatchEvent(event); });
    expect(event.defaultPrevented).toBe(true);
    expect(zone.className).not.toContain("border-accent");
  }
  expect(writes).toHaveLength(0);
  expect(button("settings-skills.file_label")).toBeTruthy();
});

it.each(["dragover", "drop"])("prevents browser navigation on %s while keeping the pending ZIP check", async (type) => {
  hold = () => {};
  await click("settings-skills.upload");
  await upload();
  expect(writes.map((w) => w.op)).toEqual(["preview"]);
  const pending = hold;
  const zone = document.querySelector<HTMLElement>('[data-testid="skill-zip-drop"]')!;
  const file = new File(["another ZIP"], "another.zip", { type: "application/zip" });
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(event, "dataTransfer", { value: { files: [file], types: ["Files"] } });
  await act(async () => { zone.dispatchEvent(event); });
  await flush();
  expect(event.defaultPrevented).toBe(true);
  expect(zone.className).not.toContain("border-accent");
  expect(zone.textContent).toContain("synthetic.zip");
  expect(writes.map((w) => w.op)).toEqual(["preview"]);
  await act(async () => { pending?.(json(preview)); });
  await flush();
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(preview.destination);
});

it("cancels a preview without installing and returns keyboard focus to Upload ZIP", async () => {
  await click("settings-skills.upload");
  await upload();
  await click("common.cancel");
  expect(writes.map((w) => w.op)).toEqual(["preview", "cancel"]);
  expect(document.activeElement?.textContent).toBe(t("settings-skills.upload"));
  expect(installed).toBe(false);
});

it("rejects a large file before sending it", async () => {
  await click("settings-skills.upload");
  await upload((4 << 20) + 1);
  expect(writes).toHaveLength(0);
  expect(document.body.textContent).toContain(t("settings-skills.error_limits"));
});

it("shows actionable conflict errors without raw node details or overwrite actions", async () => {
  fail = "skill_conflict";
  await click("settings-skills.upload");
  await upload();
  expect(document.body.textContent).toContain(t("settings-skills.error_conflict"));
  expect(document.body.textContent).not.toContain("private filesystem");
  expect(buttons().some((b) => /overwrite/i.test(b.textContent ?? ""))).toBe(false);
});

it("retires a cancelled validation reply and never turns it into an install preview", async () => {
  hold = () => {};
  await click("settings-skills.upload");
  await upload();
  expect(document.body.textContent).toContain(t("settings-skills.checking"));
  await click("common.cancel");
  await act(async () => { hold?.(json(preview)); });
  await flush();
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(writes.some((w) => w.op === "install")).toBe(false);
});

it("fences old credential replies", async () => {
  hold = () => {};
  await click("settings-skills.upload");
  await upload();
  await act(async () => setPairedToken("synthetic-new-credential"));
  await act(async () => { hold?.(json(preview)); });
  await flush();
  expect(document.body.textContent).not.toContain(preview.destination);
  expect(writes.some((w) => w.op === "install")).toBe(false);
});

it("fences the old manager's pending preview after switching assistants", async () => {
  hold = () => {};
  await click("settings-skills.upload");
  await upload();
  const pending = hold;
  expectedManager = "other-manager";
  await act(async () => root.render(<SkillsGroup manager="other-manager" />));
  await act(async () => pending?.(json(preview)));
  await flush();
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(document.body.textContent).not.toContain(preview.destination);
  expect(writes.some((w) => w.op === "install")).toBe(false);
});

it("admits an install once and reconciles the list after an uncertain failure", async () => {
  await click("settings-skills.upload");
  await upload();
  hold = () => {};
  await act(async () => {
    button("settings-skills.install").click();
    button("settings-skills.install").click();
  });
  await flush();
  expect(writes.filter((w) => w.op === "install")).toHaveLength(1);
  expect(button("common.cancel").disabled).toBe(true);
  await act(async () => hold?.(json({ code: "timeout", error: "private detail" }, 502)));
  await flush();
  expect(document.body.textContent).toContain(t("settings-skills.error_timeout"));
  expect(listed).toBeGreaterThan(1);
  expect(document.body.textContent).not.toContain("private detail");
});
