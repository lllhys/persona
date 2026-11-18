import { describe, expect, it } from "vitest";
import { type LarkMessage } from "../../lib/inbox";
import { nextLarkPosition } from "../../lib/inbox-page";

describe("LarkThread 交互与发信逻辑", () => {
  it("nextLarkPosition 正确递增消息位置以保持列表顺序", () => {
    const msgs: LarkMessage[] = [
      { id: "m1", position: "100", text: "第一条" },
      { id: "m2", position: "105", text: "第二条" },
    ];
    expect(nextLarkPosition(msgs)).toBe("106");
  });

  it("当列表为空时提供默认初始 position", () => {
    const pos = nextLarkPosition([]);
    expect(pos).toBeTruthy();
    expect(Number(pos)).toBeGreaterThan(0);
  });
});
