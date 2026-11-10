// Pushes that say the to-dos changed come in bursts; each GET /todos is the whole list. A burst reads it at most twice
// (the read out when it starts, one more after), and the last read still sees every change of the burst.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { connect, getState, refreshTodos, reset, todosPushed } from "./store";

class FakeSocket {
  static OPEN = 1;
  static all: FakeSocket[] = [];
  readyState = 0;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: ((ev: { code: number }) => void) | null = null;
  constructor() {
    FakeSocket.all.push(this);
  }
  send() {}
  close() {
    this.readyState = 3;
  }
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
  push(msg: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(msg) });
  }
}

const todo = (title: string) => ({ id: "t1", title, created_at: "", updated_at: "", spaces: [] });

// Every GET /todos waits until the test answers it, in the order they went out.
let reads: ((title: string) => void)[] = [];
const settle = () => vi.advanceTimersByTimeAsync(0);

async function answerAll(title: string) {
  while (reads.length) {
    reads.shift()!(title);
    await settle();
  }
}

async function online() {
  connect();
  const s = FakeSocket.all[FakeSocket.all.length - 1];
  s.open();
  s.push({ type: "hello", server_version: "v1", event_stream: "s1", event_seq: 1 });
  await settle();
  await answerAll("from hello"); // the hello's own refresh
  return s;
}

beforeEach(() => {
  vi.useFakeTimers();
  FakeSocket.all = [];
  reads = [];
  vi.stubGlobal("WebSocket", FakeSocket);
  vi.stubGlobal("location", { protocol: "http:", host: "hub" });
  vi.stubGlobal("localStorage", { getItem: () => "tok", setItem: () => {}, removeItem: () => {} });
  vi.stubGlobal("window", { setTimeout: (f: () => void, ms: number) => setTimeout(f, ms), clearTimeout: (t: number) => clearTimeout(t) });
  vi.stubGlobal("fetch", vi.fn().mockImplementation(async (url: string) => {
    if (!new URL(url, "http://hub").pathname.endsWith("/todos")) return new Response("{}", { status: 200 });
    const title = await new Promise<string>((r) => reads.push(r));
    return new Response(JSON.stringify({ todos: [todo(title)] }), { status: 200 });
  }));
});

afterEach(() => {
  reset();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const todoReads = () => vi.mocked(fetch).mock.calls.filter(([url]) => new URL(String(url), "http://hub").pathname.endsWith("/todos")).length;

describe("to-do pushes", () => {
  it("a burst of pushes reads the list twice, not once per push", async () => {
    const s = await online();
    const before = todoReads();
    for (let i = 0; i < 10; i++) s.push({ type: "todos" });
    await settle();
    expect(todoReads() - before).toBe(1);

    await answerAll("after the burst");
    expect(todoReads() - before).toBe(2);
    expect(getState().todos.map((t) => t.title)).toEqual(["after the burst"]);
  });

  it("a push that lands while a read is out is read after it", async () => {
    const s = await online();
    const before = todoReads();
    s.push({ type: "todos" });
    await settle();
    s.push({ type: "todos" }); // a change the read already out may not have
    reads.shift()!("before the change");
    await settle();
    expect(todoReads() - before).toBe(2);
    reads.shift()!("with the change");
    await settle();
    expect(getState().todos.map((t) => t.title)).toEqual(["with the change"]);
    expect(reads).toHaveLength(0);
  });

  it("pushes one at a time each read the list", async () => {
    const s = await online();
    const before = todoReads();
    for (const title of ["one", "two", "three"]) {
      s.push({ type: "todos" });
      await settle();
      await answerAll(title);
      expect(getState().todos.map((t) => t.title)).toEqual([title]);
    }
    expect(todoReads() - before).toBe(3);
  });

  it("what the page changed itself reads at once, past a pushed read still out", async () => {
    await online();
    const before = todoReads();
    todosPushed();
    await settle();
    const own = refreshTodos();
    await settle();
    expect(todoReads() - before).toBe(2);
    reads[1]("after my change");
    await own;
    reads[0]("older");
    await settle();
    expect(getState().todos.map((t) => t.title)).toEqual(["after my change"]);
  });

  it("a failed pushed read still lets the next push read", async () => {
    const s = await online();
    vi.mocked(fetch).mockImplementationOnce(async () => new Response(JSON.stringify({ error: "down" }), { status: 503 }));
    const before = todoReads();
    s.push({ type: "todos" });
    await settle();
    expect(getState().todosError).toBeTruthy();
    s.push({ type: "todos" });
    await settle();
    await answerAll("back");
    expect(todoReads() - before).toBe(2);
    expect(getState()).toMatchObject({ todosError: null });
    expect(getState().todos.map((t) => t.title)).toEqual(["back"]);
  });
});
