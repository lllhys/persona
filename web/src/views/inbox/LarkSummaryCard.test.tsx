import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { isSummaryCard, LarkSummaryCard } from "./LarkSummaryCard";

const draw = (meta: Record<string, unknown>, fallback = "") =>
  renderToStaticMarkup(<LarkSummaryCard meta={meta} fallbackTitle={fallback} renderParts={() => <i>parts</i>} />);

describe("LarkSummaryCard", () => {
  it("is used only for cards with a variant", () => {
    expect(isSummaryCard({ variant: "vote" })).toBe(true);
    expect(isSummaryCard({ title: "x" })).toBe(false);
  });

  it("draws a merge-forward with its previews and count", () => {
    const html = draw({ variant: "merge-forward", title: "示例 A 和示例 B 的聊天记录", messageCount: 3,
      messages: [{ id: "1", senderName: "示例 A", text: "第一条" }, { id: "2", senderName: "示例 B", isRecalled: true, text: "看不到" }] });
    expect(html).toContain("示例 A 和示例 B 的聊天记录");
    expect(html).toContain("示例 A：第一条");
    expect(html).toContain("消息已撤回");
    expect(html).not.toContain("看不到");
    expect(html).toContain("3 条");
  });

  it("draws group and contact cards", () => {
    const group = draw({ variant: "share-group", title: "示例群", isDissolved: true, fields: [{ label: "成员", value: "12" }] });
    expect(group).toContain("示例群");
    expect(group).toContain("成员 12");
    expect(group).toContain("群名片");
    expect(group).toContain("已解散");
    expect(draw({ variant: "share-user" }, "示例 C")).toContain("个人名片");
  });

  it("draws a meeting with its status, facts and the meeting number to copy", () => {
    const html = draw({ variant: "video-chat", title: "设计评审", eyebrow: "视频会议", status: "进行中",
      fields: [{ label: "发起人", value: "示例 A" }], actions: [{ type: "copyMeetingNumber", value: "123" }] });
    expect(html).toContain('data-variant="video-chat"');
    for (const s of ["设计评审", "视频会议", "进行中", "发起人", "示例 A", "复制会议号"]) expect(html).toContain(s);
  });

  it("draws a vote's options", () => {
    const html = draw({ variant: "vote", title: "周会时间", vote: { maxPick: 1, options: [{ content: "周一" }, { content: "周二" }] } });
    expect(html).toContain("周一");
    expect(html).toContain("周二");
    expect(html).toContain("rounded-full");
  });
});
