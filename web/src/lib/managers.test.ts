// Settings › Managers' words (docs/assistant/managers.md): where one runs, its status line, why Delete is off, and the hub's refusals.
import { describe, expect, it } from "vitest";
import { ApiError, errorText } from "./api";
import { deleteBlocked, deployText, managerError, minutesLeft, statusLine, unsupported, validName, whereText, type Manager } from "./managers";

const NOW = Date.parse("2026-11-05T12:00:00Z");
const m = (over: Partial<Manager>): Manager => ({ id: "m-1", name: "工作", online: false, ...over });

describe("助手", () => {
  it("运行在哪：机器 · 系统 · 怎么跑", () => {
    expect(whereText(m({ builtin: true, host: "server", os: "linux", deploy: "child" }))).toBe("server · Linux · hub 自己的");
    expect(whereText(m({ host: "MacBook", os: "darwin", deploy: "service" }))).toBe("MacBook · macOS · 服务");
    expect(whereText(m({ host: "NAS", os: "linux", deploy: "docker" }))).toBe("NAS · Linux · 容器");
    expect(whereText(m({ os: "freebsd", deploy: "nomad" }))).toBe("freebsd · nomad");
    expect(whereText(m({}))).toBe("");
    expect(deployText({ deploy: "external" })).toBe("单独运行");
    expect(deployText({ deploy: "launchd" })).toBe("服务");
    expect(deployText({ deploy: "container" })).toBe("容器");
  });

  it("状态：在线、离线多久前同步、安装中码还剩几分钟", () => {
    expect(statusLine(m({ online: true }), NOW)).toEqual({ text: "在线", tone: "green" });
    expect(statusLine(m({ last_seen: "2026-11-05T09:00:00Z" }), NOW)).toEqual({ text: "离线 · 最后同步于 3小时前", tone: "grey" });
    expect(statusLine(m({}), NOW).text).toBe("离线");
    expect(statusLine(m({ installing: "2026-11-05T12:06:30Z" }), NOW)).toEqual({ text: "安装中 · 码 7 分钟后过期", tone: "accent" });
    expect(statusLine(m({ installing: "2026-11-05T11:59:00Z" }), NOW).text).toBe("安装中 · 码已过期");
    expect(minutesLeft("2026-11-05T12:10:00.040Z", NOW)).toBe(10);
    expect(minutesLeft("2026-11-05T12:00:10Z", NOW)).toBe(1);
    expect(minutesLeft("not a time", NOW)).toBe(0);
  });

  it("hub 自己的、默认的不能删，说为什么", () => {
    expect(deleteBlocked(m({ builtin: true, default: true }))).toBe("hub 自己运行的助手不能删除");
    expect(deleteBlocked(m({ default: true }))).toBe("默认助手不能删除，先把别的设为默认");
    expect(deleteBlocked(m({}))).toBe("");
  });

  it("名字 1 到 40 个字", () => {
    expect(validName("  ")).toBe(false);
    expect(validName("工作")).toBe(true);
    expect(validName("字".repeat(40))).toBe(true);
    expect(validName("字".repeat(41))).toBe(false);
  });

  it("hub 的拒绝说成大白话；没有名册的旧 hub 单独认出来", () => {
    const err = (status: number, code?: string) => new ApiError(status, "raw", code ? { error: "raw", code } : { error: "raw" });
    expect(managerError(err(409, "default_manager"))).toBe("这是默认助手，先把别的设为默认再删除");
    expect(managerError(err(409, "builtin_manager"))).toBe("这是 hub 自己运行的助手，不能删除或换地方装");
    expect(managerError(err(404, "no_manager"))).toBe("这个助手已经不在了");
    expect(managerError(err(409, "installing"))).toBe("它还没装好，装好后才能改名或设为默认");
    expect(managerError(err(400))).toBe("名字要 1 到 40 个字");
    expect(managerError(err(403))).toBe("这台设备没有修改权限");
    // anything else is the shared explanation (#3849: a next step, the status and the hub's own words kept)
    expect(managerError(err(500))).toBe(errorText(err(500)));
    expect(managerError(err(500))).toContain("raw");
    expect(unsupported(err(404))).toBe(true);
    expect(unsupported(err(404, "no_manager"))).toBe(false);
  });
});
