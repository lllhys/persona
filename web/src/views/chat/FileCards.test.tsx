import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PathStat } from "../../lib/path-presence";
import { DocumentCards, FileCards, ReplyFiles } from "./FileCards";

const stats: Record<string, PathStat> = {
  "/w/报告.docx": { kind: "file", size: 2048 },
  "/w/slides.pptx": { kind: "file", size: 4096 },
  "/w/run.csv": { kind: "file", size: 10 },
  "/w/gone.pdf": { kind: "missing" },
};
vi.mock("../../lib/path-presence", async (orig) => ({
  ...(await orig<typeof import("../../lib/path-presence")>()),
  usePathLinks: () => ({ stat: (p: string) => stats[p], key: "k" }),
}));

describe("documents an answer names (iOS DocumentCards)", () => {
  beforeEach(() => vi.stubGlobal("localStorage", { getItem: () => "tok" }));

  it("draws each as a card with its first page from file/thumb, its name and what it is", () => {
    const html = renderToStaticMarkup(<DocumentCards files={[{ kind: "file", path: "/w/报告.docx", name: "报告.docx" }]} mid="m1" cwd="/w" />);
    expect(html).toContain('data-testid="document-card"');
    expect(html).toMatch(/src="\/api\/v1\/machines\/m1\/file\/thumb\?path=%2Fw%2F%E6%8A%A5%E5%91%8A\.docx&amp;cwd=%2Fw&amp;size=128/);
    expect(html).toContain("报告.docx");
    expect(html).toContain("Word 文档");
    expect(html).toContain('title="/w/报告.docx"');
  });

  it("puts the documents that are there in cards, the other files in the plain cards, and leaves out the missing", () => {
    const html = renderToStaticMarkup(<ReplyFiles text={"写好了 `/w/报告.docx`、`/w/slides.pptx`，日志在 `/w/run.csv`，`/w/gone.pdf` 还没生成"} mid="m1" cwd="/w" />);
    expect(html.match(/data-testid="document-card"/g)).toHaveLength(2);
    expect(html).toContain("PowerPoint 演示文稿");
    expect(html).toContain("run.csv");
    expect(html).not.toContain("gone.pdf");
    // the plain card comes first, then the row of documents
    expect(html.indexOf("run.csv")).toBeLessThan(html.indexOf("报告.docx"));
  });
});

describe("files sent with your message (#8361, iOS AttachedFileCards)", () => {
  beforeEach(() => vi.stubGlobal("localStorage", { getItem: () => "tok" }));
  const files = (n: number) => Array.from({ length: n }, (_, i) => ({ kind: "file", path: `/u/${i + 1}.pdf`, name: `${i + 1}.pdf`, size: 10 }));

  it("shows four or fewer whole", () => {
    const html = renderToStaticMarkup(<FileCards files={files(4)} mid="m1" />);
    expect(html.match(/4\.pdf/g)).toBeTruthy();
    expect(html).not.toContain('data-testid="file-cards-more"');
  });

  it("folds more than four to three and a row for the rest", () => {
    const html = renderToStaticMarkup(<FileCards files={files(8)} mid="m1" />);
    expect(html).toContain("3.pdf");
    expect(html).not.toContain("4.pdf");
    expect(html).toContain('data-testid="file-cards-more"');
    expect(html).toContain("还有 5 个文件");
    expect(html).toContain('aria-expanded="false"');
  });

  it("does not fold an answer's files", () => {
    const html = renderToStaticMarkup(<FileCards files={files(8)} mid="m1" align="start" />);
    expect(html).toContain("8.pdf");
    expect(html).not.toContain('data-testid="file-cards-more"');
  });
});
