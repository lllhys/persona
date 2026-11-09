// Markdown's streaming path (#1963): the word fade while streaming or for a block that arrived whole, plain Markdown
// otherwise, the repair of what the growing end leaves open, highlighting and CJK emphasis.
import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Markdown } from "../Markdown";
import { codeHighlight, loadHighlight } from "./highlight";
import { forgetShown } from "./wordFade";
// The grammars and the plugin loaded while the file is collected, outside the test timeout (as highlight-lazy.test.tsx,
// #4955): the lazy imports in the tests then find them loaded instead of loading them under the full suite's load.
import "rehype-highlight";
import "lowlight";

const motion = vi.hoisted(() => ({ on: true }));
vi.mock("./motion", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./motion")>()),
  useReplyMotion: () => motion.on,
}));

const render = (props: Parameters<typeof Markdown>[0]) => renderToStaticMarkup(<Markdown {...props} />);

describe("Markdown while streaming", () => {
  it("fades the words in while streaming, and is plain Markdown once it ends", () => {
    const streaming = render({ text: "Hello there world", streaming: true });
    expect(streaming).toContain('class="stream-word"');
    expect(streaming.match(/data-wf-key/g)).toHaveLength(3);
    const done = render({ text: "Hello there world" });
    expect(done).not.toContain("stream-word");
    expect(done).not.toContain("data-wf-key");
    expect(done).toBe('<div class="prose-j  "><p>Hello there world</p></div>');
  });

  it("fades a block that arrived whole (fade without streaming)", () => {
    expect(render({ text: "整块到达的回答", fade: true })).toContain("stream-word");
  });

  it("does not fade again what was on screen already: the stream a block ends, an earlier mount of the block", () => {
    const text = "这段回答已经流式显示过了，到了之后不再淡入一遍";
    render({ text, streaming: true });
    // drawn again from scratch (its item takes the stream's place): all of it in, so plain from the start
    const again = render({ text, fade: true });
    expect(again).not.toContain("stream-word");
    expect(again).not.toContain("data-wf-key");
    expect(again).toBe(render({ text }));
    // a block nobody has seen yet still fades
    expect(render({ text: "另一段刚到的回答，从来没有显示过", fade: true })).toContain("stream-word");
  });

  it("fades nothing with 回复动效 off or reduced motion", () => {
    motion.on = false;
    try {
      expect(render({ text: "Hello there", streaming: true })).not.toContain("stream-word");
      expect(render({ text: "Hello there", fade: true })).not.toContain("stream-word");
      expect(render({ text: "这是 **重要", streaming: true })).toContain("<strong>"); // the repair is not the fade's

    } finally {
      motion.on = true;
    }
  });

  it("fades list dots with their first word", () => {
    expect(render({ text: "- one\n- two", streaming: true })).toContain("data-stream-marker");
  });

  it("closes what the growing end leaves open, only while streaming", () => {
    expect(render({ text: "这是 **重要", streaming: true })).toContain("<strong>");
    expect(render({ text: "这是 **重要" })).not.toContain("<strong>");
    expect(render({ text: "```ts\nconst a", streaming: true })).toContain("<pre>");
  });

  it("renders every chunk of a streaming text", () => {
    const html = render({ text: "first para\n\nsecond para\n\n- item", streaming: true });
    expect(html.match(/<p>/g)).toHaveLength(2);
    expect(html).toContain("<li");
  });

  it("has no caret", () => {
    expect(render({ text: "Hello", streaming: true })).not.toContain("stream-caret");
  });
});

describe("Markdown's code", () => {
  it("highlights a block naming its language once the highlighter is loaded, keeping copy and mermaid", async () => {
    const text = "```ts\nconst a = 1\n```";
    expect(render({ text })).not.toContain("hljs-keyword"); // not loaded yet: plain
    await loadHighlight();
    const html = render({ text });
    expect(html).toContain('<span class="hljs-keyword">const</span>');
    expect(html).toContain('aria-label="拷贝代码"');
    expect(render({ text: "```mermaid\ngraph TD; A-->B\n```" })).not.toContain("hljs");
    // not inside the fade's spans while streaming either
    expect(render({ text, streaming: true })).toContain('<span class="hljs-keyword">const</span>');
  });

  it("colors each block as rehype-highlight colored the whole reply, on every path (#2465)", async () => {
    const corpus = [
      "```ts\nconst a: number = 1 // one\nexport function f<T>(x: T) { return `${x}` }\n```",
      "```go\nfunc main() { fmt.Println(\"hi\") }\n```\n\n```python\n@cache\ndef f(rows): return [r for r in rows]  # keep\n```",
      "```bash\nfor f in *.ts; do echo \"$f\"; done\n```\n\n```tsx\nconst A = () => <div className=\"x\">{1}</div>;\n```",
      // sublanguages: css and js inside html
      "```html\n<style>a { color: red }</style>\n<script>let x = 1</script>\n<p class=\"a\">&amp;</p>\n```",
      "```diff\n- old\n+ new\n```\n\n```nosuchlang\nplain $5\n```\n\n```\nno language\n```\n\n```ts\n```",
      "```mermaid\ngraph TD; A-->B\n```\n\n```persona-view\nnot a card\n```\n\n$$\nx^2\n$$",
      "说明：\n\n> ```js\n> let a = 1\n> ```\n\n- 列表\n\n  ```json\n  {\"a\": [1, true, null]}\n  ```",
    ];
    const paths = [{}, { streaming: true }, { fade: true }];
    const draw = (inBlock: boolean) =>
      corpus.flatMap((text) =>
        paths.map((p) => {
          forgetShown();
          codeHighlight.inBlock = inBlock;
          try {
            return render({ text, ...p });
          } finally {
            codeHighlight.inBlock = true;
          }
        }),
      );
    codeHighlight.inBlock = false;
    await loadHighlight(); // rehype-highlight
    codeHighlight.inBlock = true;
    await loadHighlight(); // the grammars
    const plugin = draw(false);
    expect(plugin.join("")).toContain('class="hljs-keyword"');
    expect(plugin.join("")).toContain('<span class="css">'); // a sublanguage
    expect(draw(true)).toEqual(plugin);
  });
});

describe("CJK emphasis", () => {
  it("closes ** after full-width punctuation followed by CJK text", () => {
    expect(render({ text: "这是**「重要」**的事" })).toContain("<strong>「重要」</strong>");
  });
});
