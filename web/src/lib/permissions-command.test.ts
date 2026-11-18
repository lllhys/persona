import { describe, expect, it, vi } from "vitest";
import { canPickPermissions, isPermissionsCommand, onPermissionPicker, permissionsCommand, PERMISSIONS_COMMAND, requestPermissionPicker } from "./permissions-command";
import { t } from "../i18n";
import type { Session } from "./types";

const session = (o: Partial<Session>): Session => ({ machine_id: "m", session_id: "s", cwd: "/x", status: "idle", state: "idle", pane_id: "p1", agent: "claude", ...o }) as Session;

// `/permissions` (#6476): the palette's row, the words that open the menu, where it is offered, and the picker it opens.
describe("/permissions", () => {
  it("is Persona's own row, in the user's language", () => {
    expect(permissionsCommand()).toEqual({ word: "/permissions", desc: t("chat-composer.cmd_permissions_desc"), text: "/permissions" });
  });

  it("opens the menu on the command alone, any case, with or without spaces", () => {
    for (const d of ["/permissions", " /permissions ", "/Permissions"]) expect(isPermissionsCommand(d)).toBe(true);
    for (const d of ["/permissions plan", "/permission", "permissions", ""]) expect(isPermissionsCommand(d)).toBe(false);
  });

  it("is offered where the permission picker is", () => {
    expect(canPickPermissions(session({}))).toBe(true);
    expect(canPickPermissions(session({ state: "working", status: "working" }))).toBe(true);
    expect(canPickPermissions(session({ assistant: true, state: "offline", status: "offline" }))).toBe(true);
    expect(canPickPermissions(session({ state: "offline", status: "offline" }), true)).toBe(true);
    expect(canPickPermissions(session({ agent: "codex" }))).toBe(false);
    expect(canPickPermissions(session({ state: "offline", status: "offline" }))).toBe(false);
    expect(canPickPermissions(undefined)).toBe(false);
  });

  it("opens the conversation's picker, and only that one's; none: the words go as text", () => {
    const a = vi.fn();
    const b = vi.fn();
    const offA = onPermissionPicker("m/a", a);
    const offB = onPermissionPicker("m/b", b);
    expect(requestPermissionPicker("m/a")).toBe(true);
    expect(a).toHaveBeenCalledTimes(1);
    expect(b).not.toHaveBeenCalled();
    offA();
    expect(requestPermissionPicker("m/a")).toBe(false);
    // an old unsubscribe does not drop the picker that took its place
    const b2 = vi.fn();
    const offB2 = onPermissionPicker("m/b", b2);
    offB();
    expect(requestPermissionPicker("m/b")).toBe(true);
    expect(b2).toHaveBeenCalledTimes(1);
    offB2();
    expect(PERMISSIONS_COMMAND).toBe("/permissions");
  });
});
