// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FileContent } from "../../../lib/files";

vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});

const requested: string[] = [];
let referencedFile: FileContent;
vi.mock("../../../lib/api", async (original) => ({
  ...(await original<typeof import("../../../lib/api")>()),
  api: async (route: string) => {
    requested.push(route);
    const path = new URL(route, "https://hub.invalid").searchParams.get("path");
    if (path === "/w/README.md") {
      return { path, name: "README.md", size: 24, mime: "text/markdown", text: `![asset](${referencedFile.name})`, next: 0, revision: "doc-r1", truncated: false, is_dir: false };
    }
    if (path === referencedFile.path) return referencedFile;
    throw new Error(`unexpected request: ${route}`);
  },
}));
vi.mock("../../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../../ui/atoms")>()), toast: vi.fn() }));

const { FileView } = await import("./FileView");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const file = (over: Partial<FileContent>): FileContent => ({
  path: "/w/diagram.png",
  name: "diagram.png",
  size: 9,
  mime: "image/png",
  truncated: false,
  is_dir: false,
  ...over,
});
const flush = async () => {
  for (let i = 0; i < 16; i++) await act(async () => { await Promise.resolve(); await new Promise((resolve) => setTimeout(resolve, 0)); });
};

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  requested.length = 0;
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function open(mid: string, reply: FileContent) {
  referencedFile = reply;
  await act(async () => root.render(<FileView mid={mid} path="/w/README.md" cwd="/w" onClose={() => {}} onOpenPath={() => {}} />));
  await flush();
  const img = host.querySelector<HTMLImageElement>('[data-testid="file-markdown"] img');
  expect(img).not.toBeNull();
  expect(img!.dataset.machine).toBe(reply.path);
  expect(requested.filter((route) => new URL(route, "https://hub.invalid").searchParams.get("path") === reply.path)).toHaveLength(1);
  return img!;
}

describe("FileView Markdown machine images", () => {
  it("uses the encoded inline download URL for a streamed PNG descriptor", async () => {
    const img = await open("stream-machine", file({ stream: true, revision: "9-1" }));
    expect(img.getAttribute("src")).toBe("/api/v1/machines/stream-machine/file/download?path=%2Fw%2Fdiagram.png&cwd=&inline=1&token=");
  });

  it("keeps a legacy base64 PNG as a data URL", async () => {
    const img = await open("legacy-machine", file({ data: "cG5nLWRhdGE=" }));
    expect(img.getAttribute("src")).toBe("data:image/png;base64,cG5nLWRhdGE=");
  });

  it("does not turn a referenced non-image into an image source", async () => {
    const img = await open("text-machine", file({ path: "/w/notes.txt", name: "notes.txt", size: 10, mime: "text/plain", data: "bm90ZXMgYm9keQ==" }));
    expect(img.getAttribute("src")).toBeNull();
  });
});
