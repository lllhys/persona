import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Session } from "../../lib/types";
import type { UsageAccount } from "../../lib/usage-windows";
import { AgentStatusLine, sessionQuota, workingLocation } from "./AgentStatusLine";

const session: Session = { machine_id: "m", session_id: "s", title: "Example", cwd: "/work/topic/src", status: "idle", agent: "codex" };
const now = Date.parse("2026-11-16T16:00:00Z");
const quota: UsageAccount = {
  id: "codex", provider: "codex", name: "Codex", sampled_at: new Date(now).toISOString(),
  windows: [
    { label: "5h", window_minutes: 300, used_percent: 4 },
    { label: "7d", window_minutes: 10080, used_percent: 33, model: "example" },
  ],
};

describe("Agent status line", () => {
  it("uses the repository name and a folder relative to the linked worktree", () => {
    const linked = { ...session, repo_root: "/work/project", worktree: "/work/topic", branch: "topic" };
    expect(workingLocation(linked)).toEqual({ name: "project", subfolder: "src", branch: "topic", folder: "/work/topic/src" });
    expect(renderToStaticMarkup(<AgentStatusLine session={linked} />)).toContain("project");
    expect(workingLocation({ ...linked, cwd: "/work/topic-other/src" })?.subfolder).toBe("");
  });

  it("uses the physical directory where available and shows only the folder outside git", () => {
    expect(workingLocation({ ...session, cwd: "/alias", workspace_cwd: "/work/topic/src", repo_root: "/work/topic" })?.subfolder).toBe("src");
    expect(workingLocation(session)).toEqual({ name: "src", subfolder: "", branch: "", folder: "/work/topic/src" });
    expect(renderToStaticMarkup(<AgentStatusLine session={{ ...session, cwd: "" }} />)).toBe("");
  });

  it("uses a single unambiguous live subscription for the session's engine", () => {
    const claude = { ...quota, id: "claude", provider: "claude" };
    expect(sessionQuota([quota, claude], session, now)?.windows.map((window) => window.label)).toEqual(["5h"]);
    expect(sessionQuota([quota, { ...quota, id: "other" }], session, now)).toBeNull();
    expect(sessionQuota([claude], session, now)).toBeNull();
    expect(sessionQuota([{ ...quota, stale: true }], session, now)).toBeNull();
  });
});
