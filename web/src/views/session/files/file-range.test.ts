import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, expect, it, vi } from "vitest";
import { rangeReader } from "./file-range";
import type { FileContent } from "../../../lib/files";

vi.mock("../../../lib/api", () => ({ fileDownloadURL: () => "/file" }));
afterEach(() => vi.unstubAllGlobals());
const file: FileContent = { path: "/example.pages", name: "example.pages", size: 10, is_dir: false, truncated: false, stream: true };
const read = (f = file) => rangeReader("machine", f, undefined, new AbortController().signal);

it("reads a correct 206 response and sends the requested range", async () => {
  const fetch = vi.fn(async (_url: string, _options: RequestInit) => new Response(new Uint8Array([2, 3, 4]), {
    status: 206, headers: { "Content-Range": "bytes 2-4/10", "Content-Length": "3" },
  }));
  vi.stubGlobal("fetch", fetch);
  expect([...await read()(2, 5)]).toEqual([2, 3, 4]);
  expect(new Headers(fetch.mock.calls[0][1]?.headers).get("Range")).toBe("bytes=2-4");
});

it.each<Record<string, string>>([
  { "Content-Range": "bytes 1-3/10" },
  { "Content-Range": "bytes 2-4/11" },
  { "Content-Range": "bytes 2-4/10", "Content-Length": "4" },
  {},
])("rejects mismatched 206 headers before reading the body: %j", async headers => {
  const pull = vi.fn(); const cancel = vi.fn();
  const body = new ReadableStream<Uint8Array>({ pull, cancel }, { highWaterMark: 0 });
  vi.stubGlobal("fetch", vi.fn(async () => new Response(body, { status: 206, headers })));
  await expect(read()(2, 5)).rejects.toThrow("Invalid file range response");
  expect(pull).not.toHaveBeenCalled(); expect(cancel).toHaveBeenCalledOnce();
});

it("cancels a chunked 206 when it exceeds the requested length", async () => {
  let pulls = 0; const cancel = vi.fn();
  const body = new ReadableStream<Uint8Array>({
    pull(controller) { pulls++; controller.enqueue(new Uint8Array(pulls === 1 ? [2, 3, 4] : [5])); }, cancel,
  }, { highWaterMark: 0 });
  vi.stubGlobal("fetch", vi.fn(async () => new Response(body, { status: 206, headers: { "Content-Range": "bytes 2-4/10" } })));
  await expect(read()(2, 5)).rejects.toThrow("too long");
  expect(pulls).toBe(2); expect(cancel).toHaveBeenCalledOnce();
});

it("rejects a truncated 206", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(new Uint8Array([2, 3]), { status: 206, headers: { "Content-Range": "bytes 2-4/10" } })));
  await expect(read()(2, 5)).rejects.toThrow("too short");
});

it("streams a 200 that ignored Range, extracts the requested bytes, and cancels the unread tail", async () => {
  let pulls = 0; const cancel = vi.fn();
  const body = new ReadableStream<Uint8Array>({
    pull(controller) { controller.enqueue(new Uint8Array(pulls++ === 0 ? [0, 1, 2] : [3, 4, 5])); }, cancel,
  }, { highWaterMark: 0 });
  vi.stubGlobal("fetch", vi.fn(async () => new Response(body, { status: 200, headers: { "Content-Length": "10" } })));
  expect([...await read()(2, 5)]).toEqual([2, 3, 4]);
  expect(pulls).toBe(2); expect(cancel).toHaveBeenCalledOnce();
});

it("rejects a 200 for an oversized file before reading a byte", async () => {
  const pull = vi.fn(); const cancel = vi.fn();
  const body = new ReadableStream<Uint8Array>({ pull, cancel }, { highWaterMark: 0 });
  vi.stubGlobal("fetch", vi.fn(async () => new Response(body, { status: 200 })));
  await expect(read({ ...file, size: (100 << 20) + 1 })(0, 3)).rejects.toThrow("Invalid file range response");
  expect(pull).not.toHaveBeenCalled(); expect(cancel).toHaveBeenCalledOnce();
});

it("rejects a truncated 200", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(new Uint8Array([0, 1, 2, 3]), { status: 200 })));
  await expect(read()(2, 5)).rejects.toThrow("too short");
});

it("cancels a blocked response when the caller aborts", async () => {
  let started!: () => void;
  const reading = new Promise<void>(resolve => { started = resolve; });
  const cancel = vi.fn();
  const body = new ReadableStream<Uint8Array>({ pull() { started(); }, cancel }, { highWaterMark: 0 });
  vi.stubGlobal("fetch", vi.fn(async () => new Response(body, { status: 206, headers: { "Content-Range": "bytes 2-4/10" } })));
  const controller = new AbortController();
  const promise = rangeReader("machine", file, undefined, controller.signal)(2, 5);
  await reading;
  controller.abort();
  await expect(promise).rejects.toMatchObject({ name: "AbortError" });
  expect(cancel).toHaveBeenCalledOnce(); expect(body.locked).toBe(false);
});

it("handles real HTTP 206, ignored Range 200, and an overlong chunked 206", async () => {
  let mode: "range" | "full" | "overflow" = "range";
  const server = createServer((req, res) => {
    expect(req.headers.range).toBe("bytes=2-4");
    if (mode === "full") {
      res.writeHead(200, { "Content-Length": "10" });
      res.end(Buffer.from([0, 1, 2, 3, 4, 5, 6, 7, 8, 9]));
    } else {
      res.writeHead(206, { "Content-Range": "bytes 2-4/10" });
      res.end(mode === "range" ? Buffer.from([2, 3, 4]) : Buffer.from([2, 3, 4, 5]));
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const nativeFetch = globalThis.fetch;
  const base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  vi.stubGlobal("fetch", (path: string, init: RequestInit) => nativeFetch(base + path, init));
  try {
    expect([...await read()(2, 5)]).toEqual([2, 3, 4]);
    mode = "full";
    expect([...await read()(2, 5)]).toEqual([2, 3, 4]);
    mode = "overflow";
    await expect(read()(2, 5)).rejects.toThrow("too long");
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) => server.close((err) => err ? reject(err) : resolve()));
  }
});
