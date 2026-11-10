// "Browser" (ego lite) on a Mac's page (docs/plans/plan-ego-browser.md, iOS BrowserSection; G107): what the hub says the Mac has
// of ego lite (GET/PUT /api/v1/machines/{mid}/browser, POST …/browser/{install|open|cancel}) and what the section
// shows for it.
import { t } from "../i18n";
import type { ComputerJob } from "./computer";

export interface MachineBrowser {
  enabled: boolean;
  installed?: boolean;
  version?: string;
  /** Its first-run setup is done; `imported`, that setup took another browser's data. */
  set_up?: boolean;
  imported?: boolean;
  /** Chromium's usage statistics: on | off | unknown. */
  analytics?: string;
  ready?: boolean;
  note?: string;
  screen_sharing?: boolean;
  job?: ComputerJob;
}

export type BrowserAction = "install" | "open" | "cancel";

export interface BrowserStep {
  id: string;
  done: boolean;
  text: string;
}

/** What the section shows below the switch (nothing while it is off). The live screen is the page's (showsScreen). */
export type BrowserView =
  | { kind: "off" }
  | { kind: "offline" }
  | { kind: "installing"; text: string }
  | { kind: "install"; text: string; retry: boolean }
  | { kind: "setup"; text: string; running: boolean; steps: BrowserStep[] }
  | { kind: "unready"; text: string }
  | { kind: "ready"; text: string; steps: BrowserStep[]; recheck: boolean };

/** ego lite's first-run setup, done on the Mac (checked on 0.6.2, 2026-11-04). */
export function setUpSteps(): BrowserStep[] {
  return [
    { id: "open", done: false, text: t("lib-browser.step_open") },
    { id: "import", done: false, text: t("lib-browser.step_import") },
    { id: "finish", done: false, text: t("lib-browser.step_finish") },
    { id: "recheck", done: false, text: t("lib-browser.step_recheck") },
  ];
}

/** What is left once it is ready: the browser data imported (or not), its usage statistics. */
export function readySteps(b: MachineBrowser): BrowserStep[] {
  const out: BrowserStep[] = [
    { id: "setup", done: true, text: t("lib-browser.ready_setup") },
    { id: "import", done: true, text: b.imported ? t("lib-browser.ready_imported") : t("lib-browser.ready_not_imported") },
  ];
  if (b.analytics === "on") out.push({ id: "analytics", done: false, text: t("lib-browser.ready_analytics_on") });
  else if (b.analytics === "off") out.push({ id: "analytics", done: true, text: t("lib-browser.ready_analytics_off") });
  return out;
}

export function browserView(b: MachineBrowser, online: boolean): BrowserView {
  if (!b.enabled) return { kind: "off" };
  if (!online) return { kind: "offline" };
  const job = b.job;
  if (job?.state === "running" && job.kind === "install") return { kind: "installing", text: job.step || t("lib-browser.installing") };
  if (!b.installed) {
    const failed = job?.kind === "install" && job.state === "failed";
    return { kind: "install", text: failed ? t("lib-browser.install_failed", { error: job.error || t("lib-browser.installer_error") }) : t("lib-browser.not_installed"), retry: job?.state === "failed" };
  }
  if (!b.set_up) {
    return { kind: "setup", text: job?.kind === "open" ? t("lib-browser.setup_opened") : t("lib-browser.setup_needed"), running: job?.state === "running", steps: setUpSteps() };
  }
  if (!b.ready) return { kind: "unready", text: b.note || t("lib-browser.not_ready") };
  return { kind: "ready", text: b.version ? t("lib-browser.ready_version", { version: b.version }) : t("lib-browser.ready"), steps: readySteps(b), recheck: b.analytics === "on" };
}

/** Asked again every 2 s: a job runs, or the setup was opened on the Mac and is not done yet. */
export function browserWatching(b: MachineBrowser, online: boolean): boolean {
  if (!online || !b.enabled) return false;
  if (b.job?.state === "running") return true;
  return !!b.installed && !b.set_up && b.job?.kind === "open";
}
