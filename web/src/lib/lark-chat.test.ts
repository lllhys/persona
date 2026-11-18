import { afterEach, describe, expect, it, vi } from "vitest";
import { docOutline, readSummary, type Get } from "../views/link/feishu-doc";
import type { LarkMessage } from "./inbox";
import {
  cardButtons, cardIconName, chatImages, chatMembers, draftKey, duration, fileSize, loadDraft, pressCardAction, profileRows, quickBar, readMark,
  mergeAround, ReadReceipts, readStates, saveDraft, toggledReactions, withEveryone,
} from "./lark-chat";

const mine = (id: string, extra: Partial<LarkMessage> = {}): LarkMessage => ({ id, position: id, is_mine: true, ...extra });

describe("cardButtons：卡片上能按的按钮", () => {
  it("回调按钮、链接按钮、溢出菜单的选项都收进来，什么也不做的画成不可按", () => {
    const meta = {
      header: { tag: "card_header", children: [{ tag: "button", actionId: "x", actionMethod: 2, children: [{ tag: "plain_text", text: "标题里的" }] }] },
      body: { tag: "body", children: [
        { tag: "markdown", children: [{ tag: "plain_text", text: "说明" }] },
        { tag: "column_set", children: [
          { tag: "button", style: { type: "primary" }, actionId: "approve", actionMethod: 2, children: [{ tag: "plain_text", text: "批准发布" }] },
          { tag: "button", url: "https://example.com/p", children: [{ tag: "plain_text", text: "看详情" }] },
          { tag: "button", actionId: "old", actionMethod: 1, children: [{ tag: "plain_text", text: "旧式" }] },
          { tag: "button", disabled: true, actionId: "off", actionMethod: 2, text: "已处理" },
        ] },
        { tag: "overflow", options: [{ text: "重新构建", actionId: "rebuild", actionMethod: 2 }, { text: "打开流水线", url: "javascript:alert(1)" }] },
      ] },
    };
    expect(cardButtons(meta)).toEqual([
      { label: "批准发布", kind: "primary", action: "approve", url: undefined, disabled: false },
      { label: "看详情", kind: "default", url: "https://example.com/p", action: undefined, disabled: false },
      { label: "旧式", kind: "default", action: undefined, url: undefined, disabled: true },
      { label: "已处理", kind: "default", action: "off", url: undefined, disabled: true },
      { label: "重新构建", kind: "default", action: "rebuild", url: undefined, disabled: false },
      { label: "打开流水线", kind: "default", action: undefined, url: undefined, disabled: true },
    ]);
    expect(cardButtons(undefined)).toEqual([]);
  });

  it("只有图标的按钮照留，带上图标和提示；什么都没有的不要", () => {
    const meta = { body: { children: [
      { tag: "button", actionId: "like", actionMethod: 2, tooltip: "点赞", children: [{ tag: "standard_icon", token: "thumbsup-like_outlined" }] },
      { tag: "button", url: "https://example.com/x", children: [{ tag: "custom_icon", image: { url: "/api/v1/lark/image/k1" } }, { tag: "plain_text", text: " " }] },
      { tag: "button", actionId: "blank", actionMethod: 2, children: [{ tag: "plain_text", text: "  " }] },
    ] } };
    expect(cardButtons(meta)).toEqual([
      { label: "", tooltip: "点赞", icon: { token: "thumbsup-like_outlined", image: undefined }, kind: "default", action: "like", url: undefined, disabled: false },
      { label: "", icon: { token: undefined, image: "/api/v1/lark/image/k1" }, kind: "default", url: "https://example.com/x", action: undefined, disabled: false },
    ]);
    expect(cardIconName("thumbsup-like_outlined")).toBe("heart");
    expect(cardIconName("warning_filled")).toBe("warning");
    expect(cardIconName("zzz")).toBeUndefined();
  });
});

describe("mergeAround：按下卡片按钮后读回那条消息周围", () => {
  const m = (id: string): LarkMessage => ({ id, position: id });
  it("合进当前窗口；越过哪头就用新的那头游标", () => {
    const cur = { messages: [m("10"), m("11"), m("12")], has_older: true, older_cursor: "o10", has_newer: true, newer_cursor: "n12" };
    const got = { messages: [m("12"), m("11"), m("13"), m("14")], has_older: true, older_cursor: "o11", has_newer: false, newer_cursor: "n14" };
    const out = mergeAround(cur, got);
    expect(out.messages.map((x) => x.id)).toEqual(["10", "11", "12", "13", "14"]);
    expect([out.has_older, out.older_cursor, out.has_newer, out.newer_cursor]).toEqual([true, "o10", false, "n14"]);
    const inside = mergeAround(cur, { messages: [m("11")], has_older: false, has_newer: false });
    expect([inside.has_older, inside.older_cursor, inside.has_newer, inside.newer_cursor]).toEqual([true, "o10", true, "n12"]);
  });
});

describe("hub 请求", () => {
  afterEach(() => vi.unstubAllGlobals());
  const stub = (body: unknown) => {
    vi.stubGlobal("localStorage", { getItem: () => "tok" });
    const fetcher = vi.fn().mockResolvedValue({ ok: true, status: 200, text: async () => JSON.stringify(body), headers: new Headers() });
    vi.stubGlobal("fetch", fetcher);
    return fetcher;
  };

  it("按卡片按钮、读成员、读已读回执带上账号", async () => {
    const f = stub({ members: [{ user_id: "u1", name: "Tom" }, { user_id: "", name: "x" }], states: [{ message_id: "m1", read_count: 1, unread_count: 2 }] });
    await pressCardAction("m 1", "approve", 2);
    expect(f.mock.calls[0][0]).toBe("/api/v1/lark/messages/m%201/card-actions/approve?ol_account=2");
    expect(f.mock.calls[0][1].method).toBe("POST");
    expect(await chatMembers("f2", "汤", undefined)).toEqual([{ user_id: "u1", name: "Tom" }]);
    expect(f.mock.calls[1][0]).toBe(`/api/v1/lark/conversations/f2/members?q=${encodeURIComponent("汤")}&limit=30`);
    expect(await readStates("f2", ["m1"])).toEqual([{ message_id: "m1", read_count: 1, unread_count: 2 }]);
    expect(JSON.parse(f.mock.calls[2][1].body)).toEqual({ message_ids: ["m1"] });
  });
});

describe("@所有人", () => {
  const tom = { user_id: "u1", name: "Tom" };
  it("没打字或打的是「所有人」的一部分时放在最前", () => {
    expect(withEveryone([tom], "", "所有人")).toEqual([{ user_id: "all", name: "所有人" }, tom]);
    expect(withEveryone([], "所有", "所有人")[0].user_id).toBe("all");
    expect(withEveryone([tom], "To", "所有人")).toEqual([tom]);
  });
});

describe("已读回执", () => {
  it("私聊写已读 / 未读，群里是进度和全部已读，没有回执不显示", () => {
    expect(readMark({ message_id: "a", read_count: 1, unread_count: 0 }, false)).toEqual({ kind: "read" });
    expect(readMark({ message_id: "a", read_count: 0, unread_count: 1 }, false)).toEqual({ kind: "unread" });
    expect(readMark({ message_id: "a", read_count: 5, unread_count: 0 }, true)).toEqual({ kind: "all" });
    expect(readMark({ message_id: "a", read_count: 3, unread_count: 2 }, true)).toEqual({ kind: "progress", read: 3, total: 5 });
    expect(readMark(undefined, true)).toBeNull();
    expect(readMark({ message_id: "a", read_count: 0, unread_count: 0 }, true)).toBeNull();
  });

  it("只问自己发出去的；全部已读的不再问；三秒最多一次；旧 hub 不再问", () => {
    const r = new ReadReceipts();
    r.appeared(mine("m1"));
    r.appeared(mine("m2"));
    r.appeared({ id: "o1", position: "1" });
    r.appeared(mine("temp-x", { sending: true }));
    r.appeared(mine("m3", { recalled: true }));
    expect(r.wait(1000)).toBe(0);
    expect(r.take(1000)).toEqual(["m1", "m2"]);
    r.record([{ message_id: "m1", read_count: 1, unread_count: 0 }, { message_id: "m2", read_count: 1, unread_count: 1 }]);
    expect(r.wait(1000)).toBeNull();
    r.changed();
    expect(r.wait(2000)).toBe(2000);
    expect(r.take(4000)).toEqual(["m2"]);
    r.changed();
    r.failed(500);
    expect(r.wait(10_000)).toBe(0);
    r.failed(404);
    expect(r.wait(10_000)).toBeNull();
    r.changed();
    expect(r.wait(20_000)).toBeNull();
  });
});

describe("表情回应", () => {
  it("加上、再加一个、取消，最后一个取消就没了", () => {
    const one = toggledReactions([], "THUMBSUP", "我");
    expect(one).toEqual({ reactions: [{ key: "THUMBSUP", count: 1, mine: true, users: ["我"] }], remove: false });
    const two = toggledReactions([{ key: "OK", count: 1, users: ["Tom"] }], "ok", "我");
    expect(two.reactions).toEqual([{ key: "OK", count: 2, mine: true, users: ["Tom", "我"] }]);
    expect(toggledReactions(two.reactions, "OK", "我")).toEqual({ reactions: [{ key: "OK", count: 1, mine: false, users: ["Tom"] }], remove: true });
    expect(toggledReactions(one.reactions, "thumbsup", "我")).toEqual({ reactions: [], remove: true });
  });

  it("快捷表情去重、取前几个", () => {
    expect(quickBar(["A", "a", " ", "B", "C"], 2)).toEqual(["A", "B"]);
  });
});

describe("媒体", () => {
  it("会话里的图片按顺序收齐，撤回的不算", () => {
    const msgs: LarkMessage[] = [
      { id: "1", position: "1", parts: [{ kind: "image", url: "/api/v1/lark/media/a" }, { kind: "text", text: "x" }] },
      { id: "2", position: "2", recalled: true, parts: [{ kind: "image", url: "/api/v1/lark/media/b" }] },
      { id: "3", position: "3", parts: [{ kind: "image", url: "/api/v1/lark/media/c" }, { kind: "image" }] },
    ];
    expect(chatImages(msgs, (u) => u && `${u}?t`)).toEqual([{ id: "1#0", src: "/api/v1/lark/media/a?t" }, { id: "3#0", src: "/api/v1/lark/media/c?t" }]);
  });

  it("file sizes move to the next unit at the boundary (#8503)", () => {
    expect(fileSize(1048000)).toBe("1023 KB");
    expect(fileSize(1048575)).toBe("1.0 MB");
    expect(fileSize(1048500)).toBe("1.0 MB");
    expect(fileSize(1073689000)).toBe("1023.9 MB");
    expect(fileSize(1073741000)).toBe("1.0 GB");
    expect(fileSize(1073741823)).toBe("1.0 GB");
  });

  it("文件大小和时长", () => {
    expect(fileSize("245760")).toBe("240 KB");
    expect(fileSize(3 * 1024 * 1024)).toBe("3.0 MB");
    expect(fileSize(undefined)).toBe("");
    expect(duration(9000)).toBe("0:09");
    expect(duration(65_400)).toBe("1:05");
    expect(duration(0)).toBe("");
  });
});

describe("资料卡", () => {
  it("有值的字段才显示，用飞书的标签，只认 http 链接", () => {
    const rows = profileRows({ fields: [
      { key: "B-DEPARTMENT", value: "设计部" },
      { key: "B-LEADER", value: "李雷", url: "lark://client/profile?uid=u_ll" },
      { key: "email", label: "邮箱", value: "me@example.com" },
      { key: "unknown", value: "x" },
      { key: "B-TITLE", value: " " },
    ] }, { "B-DEPARTMENT": "部门", "B-LEADER": "上级", "B-TITLE": "职务" });
    expect(rows).toEqual([
      { label: "部门", value: "设计部", url: undefined },
      { label: "上级", value: "李雷", url: undefined },
      { label: "邮箱", value: "me@example.com", url: undefined },
    ]);
  });
});

describe("草稿", () => {
  const mem = () => {
    const m = new Map<string, string>();
    return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k), m };
  };

  it("按会话和账号存，空的就删掉", () => {
    const s = mem();
    const k = draftKey("f2", 2);
    expect(k).not.toBe(draftKey("f2"));
    saveDraft(k, { text: "@Tom 看下", replyTo: { id: "m1", sender_name: "Tom", text: "原文" }, mentions: [{ user_id: "u1", name: "Tom" }] }, s);
    expect(loadDraft(k, s)).toEqual({ text: "@Tom 看下", replyTo: { id: "m1", sender_name: "Tom", text: "原文" }, mentions: [{ user_id: "u1", name: "Tom" }] });
    saveDraft(k, { text: "  " }, s);
    expect(s.m.size).toBe(0);
    expect(loadDraft(k, s)).toEqual({ text: "" });
  });

  it("只有引用也留着；坏数据当没有", () => {
    const s = mem();
    saveDraft("k", { text: "", replyTo: { id: "m1" } }, s);
    expect(loadDraft("k", s).replyTo).toEqual({ id: "m1" });
    s.setItem("k", "{bad");
    expect(loadDraft("k", s)).toEqual({ text: "" });
  });
});

describe("飞书文档的 AI 摘要和目录", () => {
  it("摘要写好了才显示", async () => {
    const get = (r: Record<string, unknown>): Get => async () => r;
    expect(await readSummary("u", get({ state: "ready", summary: " 要点 " }))).toBe("要点");
    expect(await readSummary("u", get({ state: "generating", summary: "x" }))).toBeNull();
    expect(await readSummary("u", get({ state: "ready", summary: "" }))).toBeNull();
  });

  it("目录按最高一级算缩进，空标题不要，序号对应文档里的第几个标题", () => {
    expect(docOutline([{ level: 2, text: "背景" }, { level: 3, text: " 细节\n一 " }, { level: 2, text: " " }, { level: 6, text: "很深" }])).toEqual([
      { index: 0, text: "背景", depth: 0 },
      { index: 1, text: "细节 一", depth: 1 },
      { index: 3, text: "很深", depth: 3 },
    ]);
    expect(docOutline([])).toEqual([]);
  });
});
