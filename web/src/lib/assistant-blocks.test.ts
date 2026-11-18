import { describe, expect, it } from "vitest";
import { assistantBlocks, blockKey, blockOf, cardReply, checkCard, openTarget } from "./assistant-blocks";

// The vectors every app and the hub are held to (sdk/go/wire/assistantblocks_test.go reads the same file).
const FENCES = JSON.parse(
  (import.meta.glob("../../../schema/extension/examples/assistant-text/fences.json", { query: "?raw", import: "default", eager: true }) as Record<string, string>)[
    "../../../schema/extension/examples/assistant-text/fences.json"
  ],
) as { name: string; text: string; indexes: number[]; complete: boolean[] }[];
const EXAMPLES = import.meta.glob("../../../schema/extension/examples/assistant/*.json", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const card = (fallback = "示例卡片") => JSON.stringify({ fallback, sections: [] });

describe("persona-view 块的编号（共享向量）", () => {
  it("has the shared vectors", () => expect(FENCES.length).toBeGreaterThanOrEqual(6));
  for (const c of FENCES) {
    it(c.name, () => {
      const bs = assistantBlocks(c.text);
      expect(bs.map((b) => b.index)).toEqual(c.indexes);
      expect(bs.map((b) => b.complete)).toEqual(c.complete);
    });
  }

  it("counts like the hub: a persona-card is not numbered, bad blocks are, outer examples are not, longer fences close", () => {
    const valid = card("Example");
    const text =
      'Intro\n```persona-card\n{"type":"stat","title":"Old","value":3}\n```\n' +
      "```persona-view\ninvalid\n```\n" +
      `~~~persona-view\n${valid}\n~~~\n` +
      `\`\`\`\`markdown\n\`\`\`persona-view\n${valid}\n\`\`\`\n\`\`\`\`\n` +
      `\`\`\`\`persona-view\n${valid}\n\`\`\`\`\n`;
    expect(assistantBlocks(text).map((b) => b.index)).toEqual([-1, 0, 1, 2]);
    expect(blockKey("uuid#0", 2)).toBe("uuid#0/2");
  });

  it("tells a drawn block by its JSON; two identical ones are not told apart", () => {
    const text = `a\n\`\`\`persona-view\n${card("一")}\n\`\`\`\nb\n\`\`\`persona-view\n${card("二")}\n\`\`\`\n`;
    expect(blockOf(text, JSON.stringify(JSON.parse(card("二")), null, 2))?.index).toBe(1);
    expect(blockOf(text + `\`\`\`persona-view\n${card("二")}\n\`\`\`\n`, card("二"))).toBeUndefined();
    expect(blockOf(text, card("三"))).toBeUndefined();
  });

  it("CR and CRLF line endings, any case of the language", () => {
    const bs = assistantBlocks(`x\r\`\`\`Persona-View\r${card()}\r\`\`\`\r`);
    expect(bs).toHaveLength(1);
    expect(bs[0].raw.trim()).toBe(card());
  });
});

describe("卡片的客户端检查", () => {
  for (const [path, text] of Object.entries(EXAMPLES)) {
    it(`accepts ${path.split("/").pop()}`, () => {
      const c = JSON.parse(text);
      expect("raw" in checkCard(JSON.stringify(c.view ?? c))).toBe(true);
    });
  }

  it("needs the exact fallback field", () => {
    expect(checkCard('{"Fallback":"x","sections":[]}')).toMatchObject({ problem: "fallback" });
    expect(checkCard('{"fallback":"  ","sections":[]}')).toMatchObject({ problem: "fallback" });
  });

  it("refuses secrets, nested pages, repeated ids and actions of more than one kind, keeping the fallback", () => {
    const secret = { fallback: "fb", sections: [{ items: [{ type: "field", name: "p", kind: "secret" }] }] };
    expect(checkCard(JSON.stringify(secret))).toEqual({ problem: "secret", fallback: "fb" });
    const nested = { fallback: "fb", sections: [], pages: { a: { sections: [], pages: {} } } };
    expect(checkCard(JSON.stringify(nested))).toMatchObject({ problem: "nested_pages" });
    const twice = { fallback: "fb", sections: [{ items: [{ type: "button", title: "a", action: { id: "x", reply: "a" } }, { type: "button", title: "b", action: { id: "x", reply: "b" } }] }] };
    expect(checkCard(JSON.stringify(twice))).toMatchObject({ problem: "action_id" });
    const both = { fallback: "fb", sections: [{ items: [{ type: "button", title: "a", action: { id: "x", reply: "a", pop: true } }] }] };
    expect(checkCard(JSON.stringify(both))).toMatchObject({ problem: "action_kind" });
    const missing = { fallback: "fb", sections: [{ items: [{ type: "button", title: "a", action: { id: "x", push: { page: "nope" } } }] }] };
    expect(checkCard(JSON.stringify(missing))).toMatchObject({ problem: "action_push" });
    expect(checkCard("x".repeat(70_000))).toMatchObject({ problem: "too_large" });
  });

  it("wraps one component with its fallback", () => {
    expect(checkCard('{"type":"text","text":"hi","fallback":"hi"}')).toEqual({ raw: { fallback: "hi", sections: [{ items: [{ type: "text", text: "hi" }] }] } });
  });
});

describe("open 目标（CheckAssistantOpen）", () => {
  it("opens https and the persona targets", () => {
    expect(openTarget("https://example.com/a?b=1")).toEqual({ kind: "web", url: "https://example.com/a?b=1" });
    expect(openTarget("persona://session/m1/s%201")).toEqual({ kind: "session", mid: "m1", sid: "s 1" });
    expect(openTarget("persona://task/t1")).toEqual({ kind: "task", id: "t1" });
    expect(openTarget("persona://kb/people/a.md")).toEqual({ kind: "kb", path: ["people", "a.md"] });
    expect(openTarget("persona://file/m1/%2Ftmp%2Fa.txt")).toEqual({ kind: "file", mid: "m1", path: "/tmp/a.txt" });
  });

  it("refuses everything else", () => {
    for (const bad of [
      "http://example.com",
      "https://user:pw@example.com",
      "javascript:alert(1)",
      "persona://kb/../secret",
      "persona://session/m1",
      "persona://session/m1/a%2Fb",
      "persona://task/t1?x=1",
      "persona://file/m1/relative",
      "persona://session/m1/%zz",
      "persona://task/a\u0000b",
      "https://example.com\\x",
      "file:///etc/passwd",
    ])
      expect(openTarget(bad), bad).toBeUndefined();
  });
});

describe("<persona-action> 尾标签", () => {
  it("reads the tag exactly as the hub writes it", () => {
    const msg = '按这些信息安排任务\n优先级：高\n<persona-action view="msg-1/0" action="submit">{"values":{"priority":"high","task":"a \\u003cb\\u003e"}}</persona-action>';
    expect(cardReply(msg)).toEqual({ text: "按这些信息安排任务\n优先级：高", view: "msg-1/0", action: "submit", values: { priority: "high", task: "a <b>" } });
    expect(cardReply('选 B\n<persona-action view="a&amp;b&#34;" action="b">{"values":{}}</persona-action>')).toMatchObject({ view: 'a&b"', action: "b" });
  });

  it("anything looser is an ordinary message", () => {
    for (const bad of [
      '选 B\n<persona-action view="v" action="b">{"values":{}}</persona-action>\n', // not last
      '选 B <persona-action view="v" action="b">{"values":{}}</persona-action>', // not its own line
      '选 B\n<persona-action action="b" view="v">{"values":{}}</persona-action>', // other order
      '选 B\n<persona-action view="v" action="b">{"values":{"a":1}}</persona-action>', // not a string
      '选 B\n<persona-action view="v" action="b">{"values":{},"x":1}</persona-action>', // another key
      '选 B\n<persona-action view="v" action="b">{"html":"yes","values":{}}</persona-action>', // html not true
      '选 B\n<persona-action view="" action="b">{"values":{}}</persona-action>',
      "选 B",
    ])
      expect(cardReply(bad), bad).toBeUndefined();
  });

  it("an HTML card's answer hides the same way (view_actions.go writes html: true, #6281)", () => {
    expect(cardReply('Open the map\n<persona-action view="msg-b/0" action="map">{"html":true,"values":{}}</persona-action>')).toEqual({ text: "Open the map", view: "msg-b/0", action: "map", values: {} });
  });
});
