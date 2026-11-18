import { describe, expect, it } from "vitest";
import { nestUnderParents } from "./side-sessions";

type Row = { id: string; parent?: string };
const r = (id: string, parent?: string): Row => ({ id, parent });
const key = (x: Row) => x.id;
const parentKey = (x: Row) => x.parent;
const ids = (rows: readonly Row[]) => rows.map(key);
const nestedIds = (rows: readonly Row[], nested: ReadonlySet<Row>) => rows.filter((x) => nested.has(x)).map(key);

describe("nestUnderParents", () => {
  it("gives the same rows back when none names a loaded parent", () => {
    const rows = [r("a"), r("b", "gone"), r("c")];
    const out = nestUnderParents(rows, key, parentKey);
    expect(out.rows).toBe(rows);
    expect(out.nested.size).toBe(0);
    expect(rows.every((x) => out.lead.get(x) === x)).toBe(true);
  });

  it("puts side sessions under their parent, the group where its most recent member was", () => {
    // newest first: a side chat of p is the newest row, p itself is older than q
    const rows = [r("s1", "p"), r("q"), r("s2", "p"), r("p"), r("z")];
    const out = nestUnderParents(rows, key, parentKey);
    expect(ids(out.rows)).toEqual(["p", "s1", "s2", "q", "z"]);
    expect(nestedIds(out.rows, out.nested)).toEqual(["s1", "s2"]);
    // the group is as recent as s1, its most recent member
    expect(key(out.lead.get(rows[3])!)).toBe("s1");
    expect(key(out.lead.get(rows[2])!)).toBe("s1");
    expect(key(out.lead.get(rows[1])!)).toBe("q");
  });

  it("nests a side session of a side session under the topmost loaded one, one level in", () => {
    const rows = [r("p"), r("s", "p"), r("g", "s")];
    const out = nestUnderParents(rows, key, parentKey);
    expect(ids(out.rows)).toEqual(["p", "s", "g"]);
    expect(nestedIds(out.rows, out.nested)).toEqual(["s", "g"]);
  });

  it("nests under the nearest loaded ancestor when the one between is not loaded", () => {
    const rows = [r("g", "s"), r("p")]; // s (g's parent) is on another page
    const out = nestUnderParents(rows, key, parentKey);
    expect(ids(out.rows)).toEqual(["g", "p"]);
    expect(out.nested.size).toBe(0);
  });

  it("links nothing in a loop, and keeps every row once", () => {
    const rows = [r("a", "b"), r("b", "a"), r("c", "a"), r("d", "d")];
    const out = nestUnderParents(rows, key, parentKey);
    expect(ids(out.rows)).toEqual(["a", "b", "c", "d"]);
    expect(out.nested.size).toBe(0);
  });

  it("neither adds nor drops a row", () => {
    const rows = [r("s1", "p"), r("x", "y"), r("p"), r("y"), r("s2", "p"), r("w", "x")];
    const out = nestUnderParents(rows, key, parentKey);
    expect([...ids(out.rows)].sort()).toEqual([...ids(rows)].sort());
    expect(ids(out.rows)).toEqual(["p", "s1", "s2", "y", "x", "w"]);
  });
});
