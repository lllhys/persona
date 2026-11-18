import { describe, expect, it } from "vitest";
import { filtered, layoutColumns, pathOf, type SourcesGraph } from "./sources-graph";

// The shape the manager answers (server/internal/subscriptionsui/graph.go), sources already in its order.
const g: SourcesGraph = {
  extensions: [
    { id: "persona", name: "Built into Persona", line: "2 sources" },
    { id: "apps", extension: "apps", name: "Connected apps", line: "2 sources" },
  ],
  reads: [
    { id: "d.persona", extension: "persona", title: "Read directly", direct: true },
    { id: "t.apps/outlook/search", extension: "apps", title: "Search messages" },
    { id: "t.apps/slack/list", extension: "apps", title: "List messages" },
  ],
  sources: [
    { id: "b.mail", read: "d.persona", kind: "builtin", name: "Mail", icon: "mail", color: "blue", line: "", reading: true, to: ["inbox", "kb"], duplicates: ["s.outlook"], state: "{}" },
    { id: "b.github", read: "d.persona", kind: "builtin", name: "GitHub", icon: "code", color: "green", line: "", reading: true, to: ["inbox"], state: "{}" },
    { id: "s.outlook", read: "t.apps/outlook/search", kind: "subscription", name: "Outlook", icon: "mail", color: "blue", line: "", reading: true, to: ["inbox", "kb"], duplicates: ["b.mail"], state: "{}" },
    { id: "s.slack", read: "t.apps/slack/list", kind: "subscription", name: "Slack", icon: "chat", color: "teal", line: "", reading: false, to: ["inbox"], state: "{}" },
  ],
  destinations: [
    { id: "inbox", name: "Inbox", line: "4 sources · 3 reading", sources: 4, reading: 3 },
    { id: "kb", name: "Knowledge base", line: "2 sources · 2 reading", sources: 2, reading: 2 },
  ],
};

describe("the relationship view (#5741)", () => {
  it("lights up a source's whole path and rings what may duplicate it", () => {
    const p = pathOf(g, { kind: "source", id: "s.outlook" })!;
    expect([...p.sources]).toEqual(["s.outlook"]);
    expect([...p.reads]).toEqual(["t.apps/outlook/search"]);
    expect([...p.extensions]).toEqual(["apps"]);
    expect([...p.destinations]).toEqual(["inbox", "kb"]);
    expect([...p.duplicates]).toEqual(["b.mail"]);
    expect(pathOf(g, null)).toBeNull();
  });

  it("lights up every source of an extension, or into a destination", () => {
    expect([...pathOf(g, { kind: "extension", id: "persona" })!.sources]).toEqual(["b.mail", "b.github"]);
    const kb = pathOf(g, { kind: "destination", id: "kb" })!;
    expect([...kb.sources]).toEqual(["b.mail", "s.outlook"]);
    expect([...kb.extensions].sort()).toEqual(["apps", "persona"]);
    expect(kb.duplicates.size).toBe(0);
  });

  it("puts each read level with its sources and each extension with its reads", () => {
    const L = layoutColumns(g, 1000);
    const mid = (k: string, h: number) => L.top[k] + h / 2;
    const src = (id: string) => mid("source:" + id, L.heights.source);
    expect(mid("read:d.persona", L.heights.read)).toBeCloseTo((src("b.mail") + src("b.github")) / 2);
    expect(mid("read:t.apps/slack/list", L.heights.read)).toBeCloseTo(src("s.slack"));
    expect(mid("extension:apps", L.heights.extension)).toBeCloseTo((src("s.outlook") + src("s.slack")) / 2);
    // a gap between extensions, none inside one
    expect(L.top["source:b.github"] - L.top["source:b.mail"]).toBeLessThan(L.top["source:s.outlook"] - L.top["source:b.github"]);
    expect(L.columns).toEqual([...L.columns].sort((a, b) => a - b));
    expect(L.width).toBeLessThanOrEqual(1000);
    expect(L.height).toBeGreaterThan(L.top["source:s.slack"] + L.heights.source);
  });

  it("filters the tree by destination and by sources that can't read now", () => {
    expect(filtered(g, "all")).toHaveLength(4);
    expect(filtered(g, "kb").map((s) => s.id)).toEqual(["b.mail", "s.outlook"]);
    expect(filtered(g, "idle").map((s) => s.id)).toEqual(["s.slack"]);
  });
});
