// The composer's permission picker (#5324, cindy's PermissionSelector, at the owner's request): a bare chip after the +,
// the mode's symbol, its name and a chevron, framed only on hover or while its menu is open; Full access tints its words
// red. Its menu is the header's (`modeMenuItems`: into Full access asks again, leaving it asks where it can't come back,
// Full access shown disabled with its reason where the agent can't take it) without Plan mode, which is the + panel's
// check row (`usePlanMode`); each mode's description is its row's tooltip. A Persona conversation keeps all its modes
// (#2416) in the same words. A session that lists its own modes (#8261, Paseo's Grok, OpenCode, Codex) has those two
// rows alone, "Launcher default" and Full access, a switch both ways.
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { bypassNote, leavePlanTo, modeConfirmation, modeMenuItems, pickerIcon } from "../../lib/header-mode";
import { onPermissionPicker } from "../../lib/permissions-command";
import { BYPASS_MODE, BYPASS_WARNING, listedModeNow, listedModeOption, listedModesOf, modeTitle, PERMISSION_MODES } from "../../lib/session-actions";
import type { Session } from "../../lib/types";
import { cx, Menu, useMenu } from "../../ui/atoms";
import type { MenuItem } from "../../ui/menu-model";
import { Icon, type IconName } from "../../ui/icons";
import { t } from "../../i18n";
import { applyModes, useModeSet } from "./SessionDialogs";

const PLAN = "plan";

/** A listed session's own approval (#8261): the plain shield, not Claude Code's raised hand of "asks first". */
const LISTED_DEFAULT_ICON: IconName = "shield";

/** The words a mode's chip and row show on hover: what the mode does (Full access's warning when it is on); none while
 *  the mode is not known. */
function modeTip(mode: string | undefined): string {
  if (mode === BYPASS_MODE) return BYPASS_WARNING;
  return PERMISSION_MODES.find((m) => m.id === mode)?.detail ?? "";
}

/** The picker's rows: the header's, Plan mode left to the + panel, each with its symbol and its description on hover. A
 *  disabled row keeps its reason under its name (a disabled row shows no tooltip), and so does a row asking again. */
/** `all` (the menu `/permissions` opens, #6476): Plan mode stays in it. Full access keeps its words under its name where
 *  it does not take effect at once (`bypassNote`: a restart, the next message). */
export function pickerItems(s: Session, pick: (mode: string) => void, assistant?: boolean, all?: boolean): MenuItem[] {
  const later = bypassNote(s, assistant);
  const listed = !assistant && listedModesOf(s);
  return modeMenuItems(s, pick, { assistant })
    .filter((r) => all || r.key !== PLAN)
    .map((r) => ({
      ...r,
      // a session's own approval (#8261) is no "Ask": the plain shield
      icon: listed && r.key === "default" ? LISTED_DEFAULT_ICON : pickerIcon(r.key),
      ...(r.disabled || (r.key === BYPASS_MODE && later) ? {} : { detail: undefined, tip: r.detail }),
      danger: r.danger || (r.key === BYPASS_MODE && (r.checked || s.bypass_pending)),
    }));
}

export const PermissionPicker = memo(function PermissionPicker({ session, assistant }: { session: Session; assistant?: boolean }) {
  const menu = useMenu();
  const { set } = useModeSet(session);
  // a session that lists its own modes (#8261) always runs in one of them: no mode reported is its own approval
  const listed = assistant ? undefined : listedModesOf(session);
  const mode = listed ? listedModeNow(session, listed) : session.permission_mode;
  const danger = mode === BYPASS_MODE;
  // `/permissions` opens the menu with every mode, Plan mode included (#6476); the chip's own click without it
  const [all, setAll] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const openAt = useRef(menu.openAt);
  openAt.current = menu.openAt;
  const key = `${session.machine_id}/${session.session_id}`;
  useEffect(() => onPermissionPicker(key, () => {
    setAll(true);
    openAt.current(button.current);
  }), [key]);
  // A mode not known yet (Claude Code's before its first prompt, which can still be changed) is named by no value: just
  // "Permission mode", as on iOS (#5756, #5596), never a mode the agent may not have.
  const own = listed ? listedModeOption(mode ?? "default") : undefined;
  const label = own ? own.title : mode ? modeTitle(mode) : t("chat-sessiondialogs.permission_mode");
  // a restart into Full access waits for the turn to end: the chip says so on hover, the mode shown is the one it runs in
  const tip = session.bypass_pending ? bypassNote(session, assistant) : own && mode !== BYPASS_MODE ? own.detail : modeTip(mode);
  return (
    <>
      <button
        type="button"
        data-testid="permission-picker"
        data-mode={mode || undefined}
        aria-haspopup="menu"
        aria-expanded={Boolean(menu.anchor)}
        aria-label={mode ? t("chat-sessiondialogs.header_mode", { mode: label }) : label}
        title={tip || undefined}
        // the caret stays in the message box (Tab still reaches the chip)
        onMouseDown={(e) => e.preventDefault()}
        ref={button}
        data-pending={session.bypass_pending ? BYPASS_MODE : undefined}
        onClick={(e) => {
          setAll(false);
          menu.open(e);
        }}
        className={cx(
          // as the model pill: no frame at rest, the capsule on hover and while the menu is open
          "touch-hit flex h-[30px] min-w-0 max-w-full shrink items-center gap-1 rounded-full border border-transparent px-2.5 text-[13px] leading-none whitespace-nowrap transition-colors duration-150",
          "hover:border-line-strong hover:bg-hover aria-expanded:border-line-strong aria-expanded:bg-hover focus-visible:rounded-full!",
          // a very narrow box keeps the symbol (the words stay its accessible name and tooltip), as cindy's icon-only chip
          "@max-[300px]/composer:px-2",
          danger ? "text-red" : mode === "auto" ? "text-mode-acts" : "text-ink",
        )}
      >
        <Icon name={own && mode === "default" ? LISTED_DEFAULT_ICON : pickerIcon(mode)} size={14} stroke={2} className="shrink-0" />
        <span className="min-w-0 truncate @max-[300px]/composer:hidden">{label}</span>
        <Icon name="chevron" size={14} stroke={2} className="shrink-0 @max-[300px]/composer:hidden" />
      </button>
      {menu.anchor && <Menu anchor={menu.anchor} onClose={menu.close} side="top" width={300} items={pickerItems(session, (m) => void set({ permission_mode: m }), assistant, all)} />}
    </>
  );
});

/** The + panel's Plan mode row (#5324): checked while the session plans; unchecking goes back to the mode it had before, as
 *  this page saw it (`leavePlanTo`). Undefined where the session has no modes to switch. */
export interface PlanMode {
  on: boolean;
  toggle: () => void;
}

// the mode each chat had before Plan mode, as this page saw it
const beforePlan = new Map<string, string>();

export function usePlanMode(session: Session | undefined, assistant?: boolean): PlanMode | undefined {
  const latest = useRef(session);
  latest.current = session;
  const key = session ? `${session.machine_id}/${session.session_id}` : "";
  const mode = session?.permission_mode;
  useEffect(() => {
    if (key && mode && mode !== PLAN) beforePlan.set(key, mode);
  }, [key, mode]);
  const toggle = useCallback(() => {
    const s = latest.current;
    if (!s) return;
    const to = s.permission_mode === PLAN ? leavePlanTo(s, beforePlan.get(`${s.machine_id}/${s.session_id}`), assistant) : PLAN;
    // into Plan mode from a Full access the agent can't come back to asks first, as the picker's row does
    if (modeConfirmation(s, to, assistant) === "leave" && !confirm(`${t("chat-sessiondialogs.leave_bypass_confirm", { mode: modeTitle(to) })}\n\n${t("chat-sessiondialogs.leave_bypass_detail")}`)) return;
    void applyModes(() => latest.current ?? s, { permission_mode: to });
  }, [assistant]);
  const on = mode === PLAN;
  return useMemo(() => (key ? { on, toggle } : undefined), [key, on, toggle]);
}
