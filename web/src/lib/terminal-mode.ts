// The terminal's two surfaces (docs/integrations/terminal.md "The full herdr view"): "Single pane", one pane streamed from herdr (the terminal
// as it has always been here), and "Full view", herdr's own client in a PTY on the computer with its sidebar, tabs and
// splits. Which one a terminal opens in, how the switch between them is staged, and the small protocol pieces of the
// full one that need no browser: the acknowledgements, the answers to a TUI's colour queries and how a view ends.
// Pure, so it is tested without a DOM.
import { t } from "../i18n";
import { paneHost } from "./agent-groups";
import { decodeBase64 } from "./terminal";

export type TerminalMode = "pane" | "full";

/** What Settings can choose: one of the two, or whichever was used last. */
export type ModePreference = TerminalMode | "last";

export const MODE_LABEL: Record<TerminalMode, string> = { pane: t("terminal-full.mode_pane"), full: t("terminal-full.mode_full") };
export const MODE_TITLE: Record<TerminalMode, string> = {
  pane: t("terminal-full.mode_pane_title"),
  full: t("terminal-full.mode_full_title"),
};

/** The last choice made with the switch, and the default in Settings; neither is written until it is chosen. */
export const LAST_MODE_KEY = "persona.terminal.mode";
export const PREFERENCE_KEY = "persona.terminal.defaultMode";
/** Set once the note about the shared layout has been shown. */
export const HINT_KEY = "persona.terminal.fullHint";

export function parseMode(raw: string | null | undefined): TerminalMode | null {
  return raw === "pane" || raw === "full" ? raw : null;
}

/** The default in Settings: "Full view" until another is chosen (2026-11-05, the user's decision: a terminal opens as herdr itself looks, as Moshi's does). */
export function parsePreference(raw: string | null | undefined): ModePreference {
  return raw === "pane" || raw === "last" ? raw : "full";
}

/** The mode a terminal opens in: what Settings says (or the last choice); "Single pane" where the hub cannot do "Full view" or it did not open on this machine. */
export function startMode(pref: ModePreference, last: TerminalMode | null, supported: boolean): TerminalMode {
  if (!supported) return "pane";
  if (pref === "last") return last ?? "full";
  return pref;
}

/**
 * The mode of a terminal: the one chosen on its own switch in this page's life; else "Single pane" where the hub has no full
 * client or "Full view" did not open on this machine (it is not tried again for every terminal); else Settings and the last choice.
 */
export function pickMode(a: { own?: TerminalMode; supported: boolean; noFull: boolean; pref: ModePreference; last: TerminalMode | null }): TerminalMode {
  if (!a.supported) return "pane";
  if (a.own) return a.own;
  if (a.noFull) return "pane";
  return startMode(a.pref, a.last, true);
}

/**
 * "Full view" is herdr's own client, so a terminal of another host (a botmux session, in botmux's own tmux) is never in it:
 * opening one would only show herdr as it happens to be. `id` is what the terminal opens by, `paneId` its session's pane.
 */
export function inHerdrClient(id: string, paneId: string | undefined): boolean {
  const host = paneHost(paneId || (id.startsWith("pane:") ? id.slice("pane:".length) : ""));
  return !host || host === "herdr";
}

export const otherMode = (m: TerminalMode): TerminalMode => (m === "pane" ? "full" : "pane");

// ── the switch between the surfaces ─────────────────────

/** How long one surface takes to give way to the other (a transition with the page's --ease-out-strong, cut short by the reduced-motion rule). */
export const SURFACE_MS = 220;

/**
 * Which surfaces are on the page: the one shown, the one on its way out (kept until it has faded) and the one the shown
 * one replaced (it comes in with a transition; null: it was there from the start, nothing moves).
 */
export interface Surfaces {
  shown: TerminalMode;
  leaving: TerminalMode | null;
  from: TerminalMode | null;
}

export type SurfaceAction = { type: "choose"; mode: TerminalMode } | { type: "done" } | { type: "adopt"; mode: TerminalMode };

/**
 * choose: the one shown goes out, the new one comes in (choosing the one shown again changes nothing); done: the one on
 * its way out has faded; adopt: a mode that was decided before anything was drawn (the hub's features arrived late).
 */
export function surfacesReducer(s: Surfaces, a: SurfaceAction): Surfaces {
  switch (a.type) {
    case "choose":
      return a.mode === s.shown ? s : { shown: a.mode, leaving: s.shown, from: s.shown };
    case "done":
      return s.leaving === null ? s : { ...s, leaving: null };
    case "adopt":
      return a.mode === s.shown && s.leaving === null ? s : { shown: a.mode, leaving: null, from: null };
  }
}

/** Which way the shown surface slides in from: "Full view" comes from the right, "Single pane" from the left. */
export const slideFrom = (m: TerminalMode): 1 | -1 => (m === "full" ? 1 : -1);

// ── acknowledgements (flow control) ─────────────────────

export interface AckScheduler {
  set(fn: () => void, ms: number): number;
  clear(id: number): void;
}

const timers: AckScheduler = { set: (fn, ms) => window.setTimeout(fn, ms), clear: (id) => window.clearTimeout(id) };

/**
 * Tells the hub how much of the terminal's output has been parsed, so the computer sends no more than a window ahead of
 * it: the total so far (cumulative), whenever `flushBytes` more have been parsed and at the latest `trailingMs` after
 * the first byte not yet told (Moshi's 64 KB and 250 ms). `noteParsed` is called from xterm's write callback, which is
 * what makes it a statement about what the screen has taken, not what arrived.
 */
export class AckTracker {
  private total = 0;
  private told = 0;
  private timer: number | null = null;

  constructor(
    private readonly tell: (bytes: number) => void,
    private readonly flushBytes = 64 << 10,
    private readonly trailingMs = 250,
    private readonly scheduler: AckScheduler = timers,
  ) {}

  noteParsed(n: number) {
    this.total += n;
    if (this.total - this.told >= this.flushBytes) this.flush();
    else if (this.timer === null) this.timer = this.scheduler.set(() => this.flush(), this.trailingMs);
  }

  flush() {
    if (this.timer !== null) {
      this.scheduler.clear(this.timer);
      this.timer = null;
    }
    if (this.total !== this.told) {
      this.told = this.total;
      this.tell(this.total);
    }
  }

  dispose() {
    if (this.timer !== null) this.scheduler.clear(this.timer);
    this.timer = null;
  }
}

// ── answers to the TUI's questions about the terminal it is in ──

/** `CSI ? 997 ; 1|2 n`: the colour scheme is dark (1) or light (2), what herdr asks for with `CSI ? 996 n` and DEC 2031. */
export const schemeReport = (dark: boolean) => `\x1b[?997;${dark ? 1 : 2}n`;

/** The answer to the mode query `CSI ? 2031 $ p`: 1 set, 2 reset. */
export const schemeModeReport = (on: boolean) => `\x1b[?2031;${on ? 1 : 2}$y`;

/** `OSC 10|11 ; rgb:rrrr/gggg/bbbb ST` for the foreground and background, as a terminal answers `OSC 10;?` / `OSC 11;?`; "" for a colour it cannot read. */
export function colourReports(foreground: string, background: string): string {
  const out: string[] = [];
  for (const [n, c] of [[10, foreground], [11, background]] as const) {
    const m = /^#([\da-f]{3}|[\da-f]{6})$/i.exec(c.trim());
    if (!m) return "";
    const hex = m[1]!.length === 3 ? [...m[1]!].map((x) => x + x).join("") : m[1]!;
    out.push(`\x1b]${n};rgb:${(hex.match(/../g) ?? []).map((x) => x + x).join("/")}\x1b\\`);
  }
  return out.join("");
}

// ── how a view ends ─────────────────────────────────────

export type FullStatus = "connecting" | "live" | "closed" | "error" | "unsupported" | "forbidden";

/** Machines where "Full view" did not open in this page's life (an old node, no herdr, Windows…): their terminals start as "Single pane" until the switch is used. */
export const noFullClient = new Set<string>();

export interface FullEnd {
  error?: string;
  code?: string;
  unsupported?: boolean;
}

/** What a herdr_end means for the view: a refusal that retrying will not change, or an end the user may reconnect from. */
export function endStatus(end: FullEnd): FullStatus {
  if (end.code === "forbidden") return "forbidden";
  if (end.unsupported) return "unsupported";
  return end.code === "too_many" || end.code === "bad_request" ? "error" : "closed";
}

/** The reason in words for the badge and for the line written dim into the terminal. */
export function endText(end: FullEnd): string {
  switch (endStatus(end)) {
    case "forbidden":
      return t("terminal-full.end_forbidden");
    case "unsupported":
      return t("terminal-full.end_unsupported");
    default:
      if (end.code === "too_many") return t("terminal-full.end_limit");
      if (end.code === "offline") return t("terminal-full.end_offline");
      return end.error?.trim() || t("terminal-full.end_closed");
  }
}

/** A line the terminal shows dim after its last output (a view that ended). */
export const dimLine = (text: string) => `\r\n\x1b[2m${text.replace(/[\x00-\x1f\x7f]/g, " ")}\x1b[0m\r\n`;

export const STATUS_LABEL: Record<FullStatus, string> = {
  connecting: t("terminal-full.status_connecting"),
  live: t("terminal-full.status_live"),
  closed: t("terminal-full.status_closed"),
  error: t("terminal-full.status_error"),
  unsupported: t("terminal-full.status_unsupported"),
  forbidden: t("terminal-full.status_forbidden"),
};

/** A view can be opened again from here (the badge offers "Reconnect"). */
export const canReconnect = (s: FullStatus) => s === "closed" || s === "error";

// ── the grid ────────────────────────────────────────────

/** The scrollback of the full surface's xterm: herdr's own client draws, but its panes scroll inside xterm's history. */
export const FULL_SCROLLBACK = 5000;

/** A grid worth opening a view at (a panel measured while it is still folded away is not). */
export function fullGrid(g: { cols: number; rows: number } | null | undefined): { cols: number; rows: number } | null {
  if (!g || !Number.isFinite(g.cols) || !Number.isFinite(g.rows)) return null;
  const cols = Math.floor(g.cols), rows = Math.floor(g.rows);
  return cols >= 20 && rows >= 5 && cols <= 500 && rows <= 300 ? { cols, rows } : null;
}

/** The bytes of a herdr_data message; null when its base64 is not. */
export const fullBytes = (data: unknown): Uint8Array | null => (typeof data === "string" ? decodeBase64(data) : null);
