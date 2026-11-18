import { describe, expect, it } from "vitest";
import { hitFolder, hitName, nameEntries, searchNames } from "./files-find-name";

// Same cases as the iPhone's FilesNavigationTests.findMatchesNamesBestFirst: both rank by one rule (#5827).
describe("searchNames", () => {
  const entries = nameEntries(["src/config/index.ts", "src/loadConfig.ts", "config.ts", "docs/app-config.md", "README.md"]);

  it("adds the folders the files are in", () => {
    expect(entries.filter((e) => e.dir).map((e) => e.path)).toEqual(["docs", "src", "src/config"]);
  });

  it("ranks the whole name, then a start, then a word, then anywhere", () => {
    const hits = searchNames(entries, "config");
    expect(hits.map((h) => h.path)).toEqual(["src/config", "config.ts", "docs/app-config.md", "src/loadConfig.ts"]);
    expect(hitName(hits[2])).toBe("app-config.md");
    expect(hitFolder(hits[2])).toBe("docs");
    expect(hitFolder(hits[1])).toBe("");
  });

  it("wants every word, in any case", () => {
    expect(searchNames(entries, "CONFIG ts").map((h) => h.path)).toEqual(["config.ts", "src/loadConfig.ts"]);
    expect(searchNames(entries, "   ")).toEqual([]);
    expect(searchNames(entries, "zzz")).toEqual([]);
  });
});
