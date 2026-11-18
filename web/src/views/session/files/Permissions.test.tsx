// @vitest-environment jsdom
import { act, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setToken } from "../../../lib/api";
import { t } from "../../../i18n";
import { modeChangeWords, PermissionsDialog } from "./Permissions";

// Changing permissions on the web (#5831): the switches, the confirmation that says what changes, the bits sent.
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let host: HTMLDivElement, root: Root, sent: Record<string, unknown>[], changed: number[];
const flush = async () => { for (let i = 0; i < 10; i++) await act(async () => { await Promise.resolve(); }); };
const button = (label: string) => [...document.querySelectorAll("button")].find((b) => b.textContent?.trim() === label);

beforeEach(async () => {
  setToken("synthetic-A"); sent = []; changed = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const u = new URL(String(input), "http://local.invalid");
    if (u.pathname.endsWith("/stat")) return Response.json({ stats: [{ path: "~/run.sh", kind: "file", mode: 0o644, owner: "me", group: "staff" }] });
    const body = JSON.parse(String(init?.body ?? "{}"));
    sent.push(body);
    return Response.json({ path: body.path, mode: body.mode });
  }));
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
  await act(async () => root.render(<PermissionsDialog mid="m1" cwd="/w" path="~/run.sh" onClose={() => {}} onChanged={(m) => changed.push(m)} />)); await flush();
});
afterEach(() => { act(() => root.unmount()); host.remove(); document.body.innerHTML = ""; setToken(""); vi.unstubAllGlobals(); });

describe("PermissionsDialog", () => {
  it("says what changes, asks, then sends the bits", async () => {
    expect(document.body.textContent).toContain("rw-r--r-- · 644");
    expect(document.body.textContent).toContain("staff");
    for (const who of [t("files-info.owner"), t("files-perm.group"), t("files-perm.others")]) {
      const box = document.querySelector<HTMLInputElement>(`input[aria-label="${who}: ${t("files-perm.run")}"]`)!;
      await act(async () => box.click()); await flush();
    }
    expect(document.body.textContent).toContain("rwxr-xr-x · 755");
    await act(async () => button(t("files-perm.change"))!.click()); await flush();
    expect(document.body.textContent).toContain(t("files-perm.confirm_title", { name: "run.sh" }));
    expect(document.body.textContent).toContain("rw-r--r-- (644) → rwxr-xr-x (755)");
    expect(sent).toEqual([]);
    const confirm = [...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')].filter((b) => b.textContent?.trim() === t("files-perm.change")).at(-1)!;
    await act(async () => confirm.click()); await flush();
    expect(sent).toEqual([{ path: "~/run.sh", cwd: "/w", mode: 0o755 }]);
    expect(changed).toEqual([0o755]);
  });

  it("shows no error when its first read is cancelled (React mounting it twice in development)", async () => {
    act(() => root.unmount());
    vi.stubGlobal("fetch", vi.fn((_input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(new DOMException("signal is aborted without reason", "AbortError")));
      setTimeout(() => resolve(Response.json({ stats: [{ path: "~/run.sh", kind: "file", mode: 0o644, owner: "me", group: "staff" }] })), 0);
    })));
    root = createRoot(host);
    await act(async () => root.render(<StrictMode><PermissionsDialog mid="m1" cwd="/w" path="~/run.sh" onClose={() => {}} onChanged={() => {}} /></StrictMode>));
    await act(async () => { await new Promise((r) => setTimeout(r, 10)); }); await flush();
    expect(document.body.textContent).toContain("rw-r--r-- · 644");
    expect(document.body.textContent).not.toContain("aborted");
    expect(document.querySelector('[role="alert"]')).toBeNull();
  });

  it("words a change for whom it is", () => {
    expect(modeChangeWords(0o644, 0o744)).toBe(`${t("files-info.owner")}: ${t("files-info.access_6")} → ${t("files-info.access_7")}`);
    expect(modeChangeWords(0o600, 0o600)).toBe("");
  });
});
