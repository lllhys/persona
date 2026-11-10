// "Computer control" on a Mac's page (docs/extensions/computer-use.md, iOS ComputerSection): what the hub says the Mac has of Cua Driver
// (GET/PUT /api/v1/machines/{mid}/computer, POST …/computer/{install|grant|cancel}), and what the section shows for it.
import { BASE } from "./base";
import { formatList, t } from "../i18n";

export interface ComputerJob {
  /** install | grant */
  kind: string;
  /** running | done | failed | cancelled */
  state: string;
  step?: string;
  error?: string;
  started?: string;
}

export interface MachineComputer {
  enabled: boolean;
  installed?: boolean;
  version?: string;
  daemon?: boolean;
  /** granted | missing | unknown */
  accessibility?: string;
  screen_recording?: string;
  /** active | locked | away */
  desktop?: string;
  screen_sharing?: boolean;
  /** cua (Cua Driver) or lcu. Empty from a hub that only knows Cua Driver. */
  backend?: string;
  /** The official ChatGPT desktop app is installed (LCU's runtime). */
  chatgpt?: boolean;
  ready?: boolean;
  note?: string;
  checked?: string;
  job?: ComputerJob;
}

export type ComputerAction = "install" | "grant" | "cancel";

/** The macOS permissions it still lacks, by their names in System Settings. */
export function missingPermissions(c: MachineComputer): string[] {
  const out: string[] = [];
  if (c.accessibility === "missing") out.push(t("lib-computer.permission_accessibility"));
  if (c.screen_recording === "missing") out.push(t("lib-computer.permission_screen_recording"));
  return out;
}

/** macOS shows its permission dialogs on the Mac itself. */
export const GRANT_HINT = t("lib-computer.grant_hint");
export const NO_SCREEN_SHARING = t("lib-computer.no_screen_sharing");

/** What the section shows below the switch (nothing while it is off). The live screen is not part of it: see showsScreen. */
export type ComputerView =
  | { kind: "off" }
  | { kind: "offline" }
  | { kind: "running"; text: string; cancel: boolean }
  | { kind: "install"; text: string; retry: boolean }
  | { kind: "granted" } // the grant's dialogs are up on the Mac
  | { kind: "grant"; text: string }
  | { kind: "ready"; text: string }
  | { kind: "unknown"; text: string };

export function computerView(c: MachineComputer, online: boolean): ComputerView {
  if (!c.enabled) return { kind: "off" };
  if (!online) return { kind: "offline" };
  const job = c.job;
  if (job?.state === "running") {
    return { kind: "running", text: job.kind === "grant" ? GRANT_HINT : job.step || t("lib-computer.installing"), cancel: job.kind === "install" };
  }
  if (!c.installed) {
    const failed = job?.kind === "install" && job.state === "failed";
    const notInstalled = c.backend === "lcu" ? t("lib-computer.not_installed_lcu") : t("lib-computer.not_installed");
    return { kind: "install", text: failed ? t("lib-computer.install_failed", { error: job.error || t("lib-computer.installer_error") }) : notInstalled, retry: job?.state === "failed" };
  }
  const missing = missingPermissions(c);
  if (c.daemon && missing.length) {
    if (job?.kind === "grant" && job.state === "done") return { kind: "granted" };
    const failed = job?.kind === "grant" && job.state === "failed" ? job.error : undefined;
    return { kind: "grant", text: failed || t("lib-computer.missing_permissions", { permissions: formatList(missing, { type: "conjunction", style: "narrow" }) }) };
  }
  if (c.ready) {
    const readyVersion = c.backend === "lcu" ? "lib-computer.ready_version_lcu" : "lib-computer.ready_version";
    return { kind: "ready", text: c.version ? t(readyVersion, { version: c.version }) : t("lib-computer.ready") };
  }
  return { kind: "unknown", text: c.note || t("lib-computer.unknown") };
}

/** Asked again every few seconds: a job runs, or macOS's dialogs wait on the Mac after a grant. */
export function computerWatching(c: MachineComputer, online: boolean): boolean {
  if (!online || !c.enabled || !c.job) return false;
  return c.job.state === "running" || (c.job.kind === "grant" && c.job.state === "done" && missingPermissions(c).length > 0);
}

/** What a machine says about its live screen: the extension cua (computer) and ego (browser) each carry it. */
interface ScreenStatus {
  enabled: boolean;
  screen_sharing?: boolean;
}

/**
 * Whether the machine page offers the live screen (screenPath; docs/extensions/computer-use.md "Live view"). The screen is
 * macOS's Screen Sharing on port 5900 through the hub's relay, which the hub opens while cua or ego is on for the Mac
 * (extmgr portAllowed); Cua Driver plays no part, so its install, permissions and daemon do not matter. The same checks,
 * in the same order, as the screen page makes before it connects (ScreenView), plus online.
 */
export function showsScreen(m: { online: boolean; computer?: ScreenStatus; browser?: ScreenStatus }): boolean {
  if (!m.online) return false;
  const on = m.computer?.enabled ? m.computer : m.browser?.enabled ? m.browser : undefined;
  return !!on?.screen_sharing;
}

/** The web's live screen of the machine (web/src/screen): a signed-in browser opens it with its own token. */
export function screenPath(mid: string): string {
  return `${BASE}/screen/${encodeURIComponent(mid)}`;
}
