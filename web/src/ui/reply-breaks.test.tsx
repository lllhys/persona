// A reply's lines as a chat shows them (#2088): steps numbered on from 5 under a line of text are a list, and a single
// line break is one. The same cases as the Apple apps' ReplyBreaksTests.
import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Markdown } from "./Markdown";
import { replyBreaks } from "./reply-breaks";

// A reply that showed as one block, made up: a bold line with steps 5–7 under it, then another bold line with its sentence.
const reply = [
  "**Also fine, and cheaper**",
  "5. Attach the screenshot from before",
  "6. Note which build it was",
  "7. Send it to the reviewer",
  "",
  "**How to tell it worked**",
  "The page opens without a reload.",
].join("\n");

describe("replyBreaks", () => {
  it("puts steps going on at 5 under a line of text in their own list", () => {
    expect(replyBreaks(reply)).toBe(reply.replace("cheaper**\n5.", "cheaper**\n\n5."));
  });

  it("gives a heading under a line of text a blank line", () => {
    expect(replyBreaks("Done.\n## Next\nMore.")).toBe("Done.\n\n## Next\nMore.");
    expect(replyBreaks("Done.\n#hashtag")).toBe("Done.\n#hashtag");
  });

  it("leaves lists as they are", () => {
    for (const text of ["1. one\n2. two\n3. three", "4. four\nwritten on\n5. five", "- a\n- b\n5. five", "- a\n  more of a\n6. six"]) {
      expect(replyBreaks(text)).toBe(text);
    }
    expect(replyBreaks("Text\n1. one\n2. two")).toBe("Text\n\n1. one\n2. two");
  });

  it("leaves code as it is", () => {
    for (const text of ["```\nprint(1)\n5. not a list\n```", "````md\nText\n5. item\n```\n# not a heading\n````\nAfter", "Text\n\n    code\n    5. still code"]) {
      expect(replyBreaks(text)).toBe(text);
    }
  });

  it("leaves other lines as they are, and takes steps out of a table", () => {
    for (const text of ["One line", "Line one\nLine two", "Version 3.14 is out\nand 2.0 too", "Paid $5.\nThen 10 more", ""]) {
      expect(replyBreaks(text)).toBe(text);
    }
    expect(replyBreaks("| a | b |\n| - | - |\n| 1 | 2 |\n5. step")).toBe("| a | b |\n| - | - |\n| 1 | 2 |\n\n5. step");
  });
});

describe("a reply in the chat", () => {
  it("reads line by line", () => {
    const html = renderToStaticMarkup(<Markdown text={reply} lineBreaks />);
    expect(html).toContain('<ol start="5">');
    expect(html).toMatch(/How to tell it worked<\/strong><br\/?>\s*The page opens/);
  });

  it("is unchanged elsewhere: a plan or a quote keeps CommonMark's paragraphs", () => {
    const html = renderToStaticMarkup(<Markdown text={reply} />);
    expect(html).not.toContain("<ol");
    expect(html).not.toContain("<br");
  });
});
