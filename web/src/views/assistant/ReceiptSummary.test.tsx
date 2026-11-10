import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ReceiptSummary } from "./Tasks";

describe("ReceiptSummary (#1508)", () => {
  it("renders the agent's last words as Markdown, folded to four lines", () => {
    const html = renderToStaticMarkup(
      <ReceiptSummary text={"## 根因\nhub 和 node 不同步\n\n**网页的改动**\n- **底部的「收起」**：改成 `Collapse`\n- 调 `POST /api/v1/sessions/{mid}/{sid}/interrupt`"} />,
    );
    expect(html).toContain("line-clamp-4");
    expect(html).toContain("prose-compact");
    expect(html).toContain("<h2>根因</h2>");
    expect(html).toContain("<strong>网页的改动</strong>");
    expect(html).toMatch(/<li><strong>底部的「收起」<\/strong>：改成 <code[^>]*>Collapse<\/code><\/li>/); // inline code a click copies (#2984)
    expect(html).toMatch(/<code[^>]*>POST \/api\/v1\/sessions\/\{mid\}\/\{sid\}\/interrupt<\/code>/);
    expect(html).not.toContain("**");
    expect(html).not.toContain("## ");
    expect(html).not.toContain("`");
  });

  it("keeps a start error as plain text", () => {
    const html = renderToStaticMarkup(<ReceiptSummary text="没开起来：找不到 **cc**" plain />);
    expect(html).toContain("没开起来：找不到 **cc**");
    expect(html).not.toContain("prose-compact");
  });

  it("opens links where they belong", () => {
    const html = renderToStaticMarkup(<ReceiptSummary text="见 [PR](https://github.com/o/r/pull/7)" />);
    expect(html).toMatch(/<a href="https:\/\/github.com\/o\/r\/pull\/7"[^>]*>PR<\/a>/);
  });
});
