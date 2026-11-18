// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AlwaysPermissions } from "./AlwaysPermissions";
import { t } from "../../i18n";

const request = vi.hoisted(() => vi.fn());
vi.mock("../../lib/api", () => ({ api: (...args: unknown[]) => request(...args), enc: encodeURIComponent, errorText: (e: Error) => e.message }));
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const grants = [{ key_hash: "a", tool_name: "Bash", created_at: "2026-11-06T01:00:00Z" }, { key_hash: "b", tool_name: "Read", created_at: "2026-11-06T01:00:00Z" }];
let host: HTMLDivElement, root: Root;
const buttons = () => [...host.querySelectorAll("button")];
const render = () => act(async () => root.render(<AlwaysPermissions session="chat/one" />));
beforeEach(() => {
  sessionStorage.clear(); request.mockReset();
  request.mockResolvedValue({ grants });
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
});
afterEach(() => { act(() => root.unmount()); host.remove(); });

it("loads this conversation's grants and removes only the accepted revoke", async () => {
  await render();
  expect(request).toHaveBeenCalledWith("/assistant/conversations/chat%2Fone/always");
  expect(host.textContent).toContain("Bash"); expect(host.textContent).toContain("Read");
  let finish!: (value: unknown) => void;
  request.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
  await act(async () => buttons()[0].click());
  expect(request).toHaveBeenLastCalledWith("/assistant/conversations/chat%2Fone/always/a", { method: "DELETE" });
  expect(buttons().every((button) => button.disabled)).toBe(true);
  expect(host.textContent).toContain("Bash");
  await act(async () => finish({ restart_required: false }));
  expect(host.textContent).not.toContain("Bash"); expect(host.textContent).toContain("Read");
});

it("keeps the grant on failure and lets the user retry", async () => {
  await render(); request.mockRejectedValueOnce(new Error("Cannot revoke"));
  await act(async () => buttons()[0].click());
  expect(host.querySelector('[role="alert"]')?.textContent).toBe("Cannot revoke");
  expect(host.textContent).toContain("Bash"); expect(buttons()[0].disabled).toBe(false);
  request.mockResolvedValueOnce({ restart_required: false });
  await act(async () => buttons()[0].click());
  expect(host.textContent).not.toContain("Bash");
});

it("keeps a restart warning when clearing the process failed", async () => {
  await render(); request.mockResolvedValueOnce({ restart_required: true });
  await act(async () => buttons()[0].click());
  expect(host.querySelector('[role="status"]')?.textContent).toBe(t("chat-sessioninfo.always_restart"));
  act(() => root.unmount()); root = createRoot(host);
  request.mockResolvedValue({ grants: [] }); await render();
  expect(host.textContent).toContain(t("chat-sessioninfo.always_restart"));
  expect(host.textContent).toContain(t("chat-sessioninfo.always_empty"));
});

it("shows a failed list separately from an empty list and retries the read", async () => {
  request.mockRejectedValueOnce(new Error("Cannot load")); await render();
  expect(host.textContent).toContain("Cannot load");
  expect(host.textContent).not.toContain(t("chat-sessioninfo.always_empty"));
  request.mockResolvedValueOnce({ grants: [] }); await act(async () => buttons()[0].click());
  expect(host.textContent).toContain(t("chat-sessioninfo.always_empty"));
});
