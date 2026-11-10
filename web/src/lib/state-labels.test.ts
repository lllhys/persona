// @vitest-environment node
// One set of state names on the web (#3415): the words live in lib-state-labels.json, and no other namespace defines a
// state name again or brings back one of the old variants (Waiting for you / In progress / To review / Mark all as read…).
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { beforeAll, describe, expect, it } from "vitest";
import { initI18n } from "../i18n";
import { markAllSeenLabel, STATE_LABEL, stateCount, stateName, type StateName } from "./state-labels";

const ZH = join(__dirname, "../i18n/locales/zh-Hans");

/** The names only an agent's or a task's state has (lib-state-labels.json). Done, Unknown, Interrupted… also name other
 *  things (a tool call, a project, a CI check), so they are not checked. */
const NAMES = ["需要你", "工作中", "完成未看", "已看", "未运行", "待唤醒", "待你回复", "待跟进", "全部标为已看"];

/** Names a page used before #3415 for one of those states. */
const OLD = ["等你", "在做", "待查看", "做完", "做完了", "做完了，还没看", "停着", "等你处理", "需要你处理", "正在工作", "待回答", "全部标为已读"];

/** The same word meaning something else: a file marked reviewed, an inbox item's tier. */
const OTHER_MEANING = new Set(["extensions-review.viewed", "lib-inbox.tier_action"]);

function others(): [string, string][] {
  const out: [string, string][] = [];
  for (const f of readdirSync(ZH).filter((f) => f.endsWith(".json") && f !== "lib-state-labels.json")) {
    const ns = f.replace(/\.json$/, "");
    for (const [k, v] of Object.entries(JSON.parse(readFileSync(join(ZH, f), "utf8")) as Record<string, string>)) out.push([`${ns}.${k}`, v]);
  }
  return out;
}

describe("state names", () => {
  beforeAll(() => initI18n("zh-Hans"));

  it("are the docs/protocol/i18n.md glossary's", () => {
    expect(STATE_LABEL).toEqual({ needs_you: "需要你", working: "工作中", unread: "完成未看", idle: "已看", offline: "未运行", unknown: "未知" });
    const names: Record<StateName, string> = {
      needs_you: "需要你", working: "工作中", unread: "完成未看", idle: "已看", offline: "未运行", unknown: "未知",
      waiting: "待唤醒", bg_waiting: "后台等待中", awaits_reply: "待你回复", done: "已完成", interrupted: "中断了", follow_up: "待跟进", task_waiting: "等待中", not_started: "未开始", failed: "没开起来",
    };
    for (const [s, name] of Object.entries(names)) expect(stateName(s as StateName)).toBe(name);
    expect(stateCount("needs_you", 2)).toBe("2 个需要你");
    expect(stateCount("unread", 1)).toBe("1 个完成未看");
    expect(stateCount("bg_waiting", 2)).toBe("2 个后台等待中");
    expect(markAllSeenLabel()).toBe("全部标为已看");
    expect(markAllSeenLabel(3)).toBe("全部标为已看（3）");
  });

  it("are defined once: no other text is just a state name, old or new", () => {
    const words = new Set([...NAMES, ...OLD]);
    // the counts too: "3 need you"
    for (const n of NAMES) words.add(`{count} 个${n}`);
    const again = others().filter(([key, v]) => words.has(v) && !OTHER_MEANING.has(key)).map(([key, v]) => `${key}: ${v}`);
    expect(again, "take these from lib/state-labels.ts").toEqual([]);
  });

  it("say 已看 in a sentence about marking seen, never 已读", () => {
    const read = others().filter(([, v]) => /标为已读/.test(v)).map(([key]) => key);
    expect(read).toEqual([]);
  });
});
