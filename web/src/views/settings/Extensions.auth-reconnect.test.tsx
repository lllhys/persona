// @vitest-environment jsdom
// An extension's page after a reconnect the hub could not resume (#8854): an extension one signs in to reads its
// sign-in again (pushes missed meanwhile are not replayed); one with no sign-in asks nothing, since the hub would
// answer 404.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HubExtensionDetail } from "../../lib/extensions";

const hub = vi.hoisted(() => ({
  listeners: new Map<string, Set<(m: Record<string, unknown>) => void>>(),
  detail: null as unknown,
  authReads: 0,
}));
vi.mock("../../lib/extensions", async (original) => ({
  ...(await original<typeof import("../../lib/extensions")>()),
  listExtensionsInfo: () => Promise.resolve({ extensions: [], canInstall: false, markets: false }),
  extensionDetail: () => Promise.resolve(hub.detail),
}));
vi.mock("../../lib/extension-auth", async (original) => ({
  ...(await original<typeof import("../../lib/extension-auth")>()),
  getAuth: () => {
    hub.authReads++;
    return Promise.resolve({ title: "Account", state: "signed_in", account: "me@example.com" });
  },
}));
vi.mock("../../lib/store", async (original) => ({
  ...(await original<typeof import("../../lib/store")>()),
  onHubEvent: (type: string, l: (m: Record<string, unknown>) => void) => {
    if (!hub.listeners.has(type)) hub.listeners.set(type, new Set());
    hub.listeners.get(type)!.add(l);
    return () => void hub.listeners.get(type)?.delete(l);
  },
}));
vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: vi.fn() }));
import { Extensions } from "./Extensions";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};
const hello = async (resumed: boolean) => {
  await act(async () => hub.listeners.get("hello")?.forEach((l) => l({ type: "hello", resumed })));
  await flush();
};
const detail = (over: Partial<HubExtensionDetail>): HubExtensionDetail =>
  ({ id: "example-ext", name: "Example", kind: "process", status: "running", source: "installed", enabled: true, ...over }) as HubExtensionDetail;

beforeEach(() => {
  hub.listeners.clear();
  hub.authReads = 0;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("an extension's page after a reconnect", () => {
  it("reads the sign-in again of an extension one signs in to, only when the stream was not resumed", async () => {
    hub.detail = detail({ auth: { title: "Account", state: "signed_out" } });
    act(() => root.render(<Extensions open="example-ext" onOpen={() => {}} />));
    await flush();
    await hello(true);
    expect(hub.authReads).toBe(0);
    await hello(false);
    expect(hub.authReads).toBe(1);
  });

  it("asks nothing of an extension with no sign-in", async () => {
    hub.detail = detail({});
    act(() => root.render(<Extensions open="example-ext" onOpen={() => {}} />));
    await flush();
    await hello(false);
    await hello(false);
    expect(hub.authReads).toBe(0);
  });
});
