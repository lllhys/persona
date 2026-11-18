import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { JSONStore } from "../src/store.js";

test("concurrent window and hub writes preserve both fields and clean temporary files", async t => {
  const directory = await mkdtemp(join(tmpdir(), "persona-store-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const store = new JSONStore(join(directory, "desktop-state.json"));
  const other = new JSONStore(join(directory, "desktop-state.json"));
  const originalRead = store.read.bind(store);
  let releaseRead: (() => void) | undefined;
  let reads = 0;
  store.read = async <T,>(): Promise<T | undefined> => {
    reads++;
    if (reads === 1) await new Promise<void>(done => { releaseRead = done; });
    return originalRead<T>();
  };
  const windowWrite = store.saveWindow({ x: 10, y: 20, width: 1000, height: 800, maximized: false });
  const hubWrite = Promise.resolve().then(() => other.update({ hub: "https://example.com/" }));
  await new Promise(done => setImmediate(done));
  assert.equal(reads, 1, "the second update must not read a stale snapshot before the first finishes");
  releaseRead?.();
  await Promise.all([windowWrite, hubWrite]);
  const state = JSON.parse(await readFile(join(directory, "desktop-state.json"), "utf8"));
  assert.equal(state.hub, "https://example.com/");
  assert.equal(state.window.width, 1000);
  assert.deepEqual(await readdir(directory), ["desktop-state.json"]);
});

test("a failed update leaves the original state and permits a later serialized write", async t => {
  const directory = await mkdtemp(join(tmpdir(), "persona-store-failure-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const file = join(directory, "desktop-state.json");
  const store = new JSONStore(file);
  const originalRead = store.read.bind(store);
  let failRead = false;
  store.read = async <T,>(): Promise<T | undefined> => {
    if (failRead) throw new Error("synthetic read failure");
    return originalRead<T>();
  };
  await store.update({ hub: "https://old.example/" });
  failRead = true;
  await assert.rejects(store.update({ hub: "https://new.example/", window: { width: 1200 } }), /synthetic read failure/);
  failRead = false;
  assert.equal(JSON.parse(await readFile(file, "utf8")).hub, "https://old.example/");
  await store.update({ window: { width: 1000 } });
  const state = JSON.parse(await readFile(file, "utf8"));
  assert.equal(state.hub, "https://old.example/");
  assert.equal(state.window.width, 1000);
});
