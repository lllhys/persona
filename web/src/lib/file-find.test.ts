import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./api";
import {
  cachedIndex,
  clearIndexes,
  createFileSearch,
  fileContext,
  highlightRuns,
  hitPath,
  INDEX_TTL_MS,
  indexFresh,
  lacksFind,
  loadFileIndex,
  mentionOf,
  MAX_FILE_HITS,
  searchTerms,
  walkTree,
  type FileIndex,
} from "./file-find";
import type { FsListing } from "./files";
import type { TermSpace } from "./terminals";

const paths = (hits: { path: string }[]) => hits.map((h) => h.path);
const marked = (h: { path: string; matched: number[] }) => h.matched.map((i) => h.path[i]).join("");

describe("matching", () => {
  const files = ["src/AppView.tsx", "src/app/index.ts", "src/lib/shapes.ts", "docs/approach.md", "README.md", "web/src/lib/palette.ts", "web/src/views/Palette.tsx", "web/src/views/PaletteFiles.test.tsx"];

  it("matches nothing for an empty query", () => {
    expect(createFileSearch(files).search("")).toEqual([]);
    expect(createFileSearch(files).search("   ")).toEqual([]);
  });
  it("needs the letters in order, and ignores case", () => {
    expect(paths(createFileSearch(files).search("READ"))).toEqual(["README.md"]);
    expect(createFileSearch(files).search("mdr")).toEqual([]);
  });
  it("puts a word start before letters scattered in the middle of words", () => {
    // "ap" starts the words in app/ and AppView and approach, but is only inside "shapes"
    const got = paths(createFileSearch(files).search("ap"));
    expect(got).toContain("src/lib/shapes.ts");
    expect(got.indexOf("src/lib/shapes.ts")).toBeGreaterThan(got.indexOf("src/AppView.tsx"));
    expect(got.indexOf("src/lib/shapes.ts")).toBeGreaterThan(got.indexOf("docs/approach.md"));
  });
  it("counts the humps of camelCase as word starts", () => {
    const list = ["src/xAppView.tsx", "src/xapplication.tsx"];
    // "av": the A and V of xAppView are word starts; xapplication has no v after an a
    expect(paths(createFileSearch(list).search("av"))).toEqual(["src/xAppView.tsx"]);
    const both = ["src/mapview.ts", "src/MapView.ts"];
    const hits = createFileSearch(both).search("mv");
    expect(hits[0].path).toBe("src/MapView.ts");
    expect(marked(hits[0])).toBe("MV");
  });
  it("prefers the file's own name to the folders above it", () => {
    const list = ["palette/other.txt", "web/palette.ts"];
    expect(paths(createFileSearch(list).search("palette"))).toEqual(["web/palette.ts", "palette/other.txt"]);
  });
  it("prefers an exact or leading name, and then the shorter path", () => {
    const list = ["a/very/deep/folder/index.ts", "index.ts", "index.test.ts", "indexes/readme.md"];
    expect(paths(createFileSearch(list).search("index"))[0]).toBe("index.ts");
    expect(paths(createFileSearch(list).search("index")).indexOf("index.test.ts")).toBeLessThan(paths(createFileSearch(list).search("index")).indexOf("a/very/deep/folder/index.ts"));
  });
  it("does not rank a file lower for being in deeper folders", () => {
    const list = ["Sources/App/Views/ForecastView.swift", "Tests/AppTests/ForecastViewModelTests.swift", "Sources/App/Models/ForecastViewModel.swift"];
    expect(paths(createFileSearch(list).search("fore"))).toEqual(["Sources/App/Views/ForecastView.swift", "Sources/App/Models/ForecastViewModel.swift", "Tests/AppTests/ForecastViewModelTests.swift"]);
    expect(paths(createFileSearch(list).search("fvm"))[0]).toBe("Sources/App/Models/ForecastViewModel.swift");
  });
  it("needs every term, in any order (AND)", () => {
    expect(paths(createFileSearch(files).search("views palette"))).toEqual(expect.arrayContaining(["web/src/views/Palette.tsx", "web/src/views/PaletteFiles.test.tsx"]));
    expect(paths(createFileSearch(files).search("palette views"))).toHaveLength(2);
    expect(paths(createFileSearch(files).search("palette lib"))).toEqual(["web/src/lib/palette.ts"]);
    expect(createFileSearch(files).search("palette docs")).toEqual([]);
  });
  it("lists the letters that matched, for drawing them", () => {
    const [hit] = createFileSearch(["web/src/views/Palette.tsx"]).search("pal tsx");
    expect(marked(hit)).toBe("Paltsx");
    expect(hit.matched).toEqual([...hit.matched].sort((a, b) => a - b));
    expect(new Set(hit.matched).size).toBe(hit.matched.length);
  });
  it("takes the best alignment, not the first", () => {
    // the first p, p of "src/pp/x" would do, but the contiguous word "pp" later is better; either way it is a match
    const [hit] = createFileSearch(["a/p_x/p_p/zzz.txt"]).search("pp");
    expect(hit.matched.length).toBe(2);
    expect(hit.path[hit.matched[0]]).toBe("p");
  });
  it("lists at most 50 by default, best first, and as many as asked", () => {
    const many = Array.from({ length: 200 }, (_, i) => `dir/file${i}.ts`);
    expect(createFileSearch(many).search("file")).toHaveLength(MAX_FILE_HITS);
    expect(createFileSearch(many).search("file", 7)).toHaveLength(7);
  });
  it("splits the query on any space", () => {
    expect(searchTerms("  a   b\tc ")).toEqual(["a", "b", "c"]);
    expect(searchTerms("")).toEqual([]);
  });
  it("works with a path of one character and a term longer than the path", () => {
    expect(paths(createFileSearch(["a"]).search("a"))).toEqual(["a"]);
    expect(createFileSearch(["a"]).search("ab")).toEqual([]);
  });
});

describe("drawing a hit", () => {
  it("cuts a path into matched and plain runs", () => {
    const [hit] = createFileSearch(["web/src/Palette.tsx"]).search("pal");
    const dir = hit.path.slice(0, hit.path.lastIndexOf("/") + 1);
    const name = hit.path.slice(dir.length);
    expect(highlightRuns(dir, 0, hit.matched)).toEqual([{ text: dir, hit: false }]);
    expect(highlightRuns(name, dir.length, hit.matched)).toEqual([
      { text: "Pal", hit: true },
      { text: "ette.tsx", hit: false },
    ]);
  });
  it("is one plain run without matches, and nothing for no text", () => {
    expect(highlightRuns("a/b", 0, [])).toEqual([{ text: "a/b", hit: false }]);
    expect(highlightRuns("", 0, [1])).toEqual([]);
  });
  it("copies a path as a mention", () => {
    expect(mentionOf({ path: "src/a b.ts" })).toBe("@src/a b.ts");
  });
});

describe("narrowing the last query", () => {
  const files = Array.from({ length: 300 }, (_, i) => `pkg${i % 7}/mod${i}/Widget${i}.ts`).concat(["other/readme.md"]);

  it("gives the same answer as searching the whole list", () => {
    const s = createFileSearch(files);
    for (const q of ["w", "wi", "wid", "wid 1", "wid 12", "wid 120"]) expect(s.search(q)).toEqual(createFileSearch(files).search(q));
  });
  it("only looks at the last matches while the query goes on from the last one", () => {
    const seen: string[] = [];
    const watched = new Proxy(files, {
      get(target, key, recv) {
        if (key === Symbol.iterator) seen.push("scan");
        return Reflect.get(target, key, recv);
      },
    });
    const s = createFileSearch(watched);
    s.search("widget 1");
    const scans = seen.length;
    s.search("widget 12"); // narrows: no scan of the whole list
    expect(seen.length).toBe(scans);
    s.search("read"); // not a continuation: the whole list again
    expect(seen.length).toBe(scans + 1);
  });
  it("is not fooled by a shorter, a changed or a cleared query", () => {
    const s = createFileSearch(files);
    s.search("widget 12");
    expect(s.search("widget")).toEqual(createFileSearch(files).search("widget"));
    expect(s.search("readme")).toEqual(createFileSearch(files).search("readme"));
    expect(s.search("")).toEqual([]);
    expect(s.search("readme")).toEqual(createFileSearch(files).search("readme"));
  });
  it("narrows by words too: a word added to the query", () => {
    const s = createFileSearch(files);
    s.search("mod1");
    expect(paths(s.search("mod1 pkg3"))).toEqual(paths(createFileSearch(files).search("mod1 pkg3")));
  });
});

describe("where to search", () => {
  const spaces = [{ tabs: [{ panes: [{ id: "p1", cwd: "/work/app" }, { id: "p2", cwd: "" }] }] }] as unknown as TermSpace[];
  const cwdOf = (mid: string, sid: string) => (mid === "m1" && sid === "s1" ? "/work/app" : undefined);
  const spacesOf = (mid: string) => (mid === "m1" ? spaces : undefined);

  it("is the session's directory on a conversation page", () => {
    expect(fileContext({ name: "session", mid: "m1", sid: "s1" }, { mid: "m1", sid: "s1" }, cwdOf, spacesOf)).toEqual({ mid: "m1", cwd: "/work/app" });
    expect(fileContext({ name: "session", mid: "m1", sid: "gone" }, { mid: "m1", sid: "gone" }, cwdOf, spacesOf)).toBeNull();
  });
  it("is the open pane's directory on the terminal page", () => {
    expect(fileContext({ name: "terminal", mid: "m1", id: "p1" }, undefined, cwdOf, spacesOf)).toEqual({ mid: "m1", cwd: "/work/app" });
    expect(fileContext({ name: "terminal", mid: "m1", id: "p2" }, undefined, cwdOf, spacesOf)).toBeNull();
    expect(fileContext({ name: "terminal", mid: "m1" }, undefined, cwdOf, spacesOf)).toBeNull();
    expect(fileContext({ name: "terminal", mid: "m2", id: "p1" }, undefined, cwdOf, spacesOf)).toBeNull();
  });
  it("is nothing elsewhere", () => {
    expect(fileContext({ name: "home" }, undefined, cwdOf, spacesOf)).toBeNull();
  });
  it("opens a hit by its path under the directory", () => {
    expect(hitPath({ mid: "m1", cwd: "/work/app" }, { path: "src/a.ts" })).toBe("/work/app/src/a.ts");
  });
});

describe("walking a node that cannot list in one go", () => {
  const tree: Record<string, { name: string; dir: boolean }[]> = {
    "/r": [{ name: "z.txt", dir: false }, { name: "src", dir: true }, { name: "node_modules", dir: true }, { name: ".git", dir: true }],
    "/r/src": [{ name: "a.ts", dir: false }, { name: "deep", dir: true }],
    "/r/src/deep": [{ name: "b.ts", dir: false }],
    "/r/node_modules": [{ name: "never.js", dir: false }],
  };
  const list = vi.fn(async (dir: string): Promise<FsListing> => {
    if (!tree[dir]) throw new Error("gone");
    return { path: dir, entries: tree[dir].map((e) => ({ ...e, size: 0 })), truncated: false };
  });
  afterEach(() => list.mockClear());

  it("lists breadth first and skips folders that are caches", async () => {
    const got = await walkTree(list, "/r");
    expect(got).toEqual({ files: ["z.txt", "src/a.ts", "src/deep/b.ts"], truncated: false });
    expect(list).not.toHaveBeenCalledWith("/r/node_modules");
    expect(list).not.toHaveBeenCalledWith("/r/.git");
  });
  it("stops at its limits and says so", async () => {
    expect((await walkTree(list, "/r", { folders: 2, files: 100, parallel: 1 })).truncated).toBe(true);
    const some = await walkTree(list, "/r", { folders: 10, files: 1, parallel: 2 });
    expect(some.files).toHaveLength(1);
    expect(some.truncated).toBe(true);
  });
  it("skips a folder that cannot be read", async () => {
    const got = await walkTree(async (dir) => (dir === "/r/src" ? Promise.reject(new Error("denied")) : list(dir)), "/r");
    expect(got.files).toEqual(["z.txt"]);
  });
});

describe("the index", () => {
  const ctx = { mid: "m1", cwd: "/work/app" };
  afterEach(() => {
    clearIndexes();
    vi.useRealTimers();
  });
  const index = (...files: string[]): FileIndex => ({ files, truncated: false });

  it("is asked for once while it is fresh, and shared by those who ask meanwhile", async () => {
    const fetcher = vi.fn(async () => index("a.ts"));
    const [one, two] = await Promise.all([loadFileIndex(ctx, fetcher), loadFileIndex(ctx, fetcher)]);
    expect(one).toBe(two);
    expect(await loadFileIndex(ctx, fetcher)).toBe(one);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(cachedIndex(ctx)).toBe(one);
  });
  it("is asked for again once it is old, and keeps the old one meanwhile", async () => {
    vi.useFakeTimers();
    const fetcher = vi.fn(async () => index("a.ts"));
    await loadFileIndex(ctx, fetcher);
    vi.advanceTimersByTime(INDEX_TTL_MS + 1);
    expect(indexFresh(ctx)).toBe(false);
    expect(cachedIndex(ctx)).toBeDefined();
    await loadFileIndex(ctx, fetcher);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
  it("is kept per computer and directory", async () => {
    const fetcher = vi.fn(async (c: { mid: string; cwd: string }) => index(`${c.mid}${c.cwd}`));
    await loadFileIndex(ctx, fetcher);
    await loadFileIndex({ ...ctx, cwd: "/other" }, fetcher);
    await loadFileIndex({ ...ctx, mid: "m2" }, fetcher);
    expect(fetcher).toHaveBeenCalledTimes(3);
  });
  it("does not keep a failure", async () => {
    const fetcher = vi.fn().mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce(index("a.ts"));
    await expect(loadFileIndex(ctx, fetcher)).rejects.toThrow("offline");
    expect(cachedIndex(ctx)).toBeUndefined();
    expect((await loadFileIndex(ctx, fetcher)).files).toEqual(["a.ts"]);
  });
  it("tells an older node's refusal from other failures", () => {
    expect(lacksFind(new ApiError(404, "not found", {}, false))).toBe(true);
    expect(lacksFind(new ApiError(500, "unknown method fs.find", {}, true))).toBe(true);
    expect(lacksFind(new ApiError(500, "not a directory: /x", {}, true))).toBe(false);
    expect(lacksFind(new Error("offline"))).toBe(false);
  });
});
