// "iPhone control" on a computer's page (#8217; docs/extensions/iphone.md "Interfaces" and "Codes for apps"): what the hub
// says the computer has of the iPhone helper and the iPhones it reaches (GET/PUT /api/v1/machines/{mid}/iphone,
// POST …/iphone/{pair|forget|cancel|install|prepare}), and what the section shows for it. Every state is read from the
// codes, in the words of the iPhone app's guided setup (#8199); a note from an older node that sends no code is shown
// as it is. The iPhone's own consent is given only on that iPhone, so the section only says where.
import { t } from "../i18n";

export interface IPhoneJob {
  /** pair | forget | install | prepare */
  kind: string;
  /** running | done | failed | cancelled */
  state: string;
  step?: string;
  error?: string;
  /** While a pairing runs, what it waits for (trust_pending, locked); once failed, why (docs/extensions/iphone.md). */
  code?: string;
  /** The iPhone a pairing or a preparing is for. */
  device?: string;
  started?: string;
}

export interface IPhoneDevice {
  id: string;
  name?: string;
  product_type?: string;
  ios?: string;
  paired?: boolean;
  /** On this computer's cable now. */
  usb?: boolean;
  state?: string;
  /** usb | network */
  transport?: string;
  connected?: boolean;
  locked?: boolean;
  ready?: boolean;
  note?: string;
  code?: string;
  developer_mode?: boolean;
  /** Its owner prepared it once, so the computer prepares it again by itself after a restart. */
  auto_prepare?: boolean;
  /** On for this computer and that iPhone consented. */
  allowed?: boolean;
}

export interface MachineIPhone {
  enabled: boolean;
  installed?: boolean;
  version?: string;
  devices?: IPhoneDevice[];
  note?: string;
  /** bridge_missing | none_paired */
  code?: string;
  /** The computer has the developer disk image Prepare mounts (Xcode's on a Mac); missing from an older node. */
  ddi_present?: boolean;
  checked?: string;
  job?: IPhoneJob;
}

export type IPhoneAction = "pair" | "forget" | "cancel" | "install" | "prepare";

/** How a line reads: waits for the person (orange), ready (green), the computer working (a spinner), or just words. */
export type IPhoneTone = "need" | "ok" | "work" | "plain";

/** One iPhone in the list: its state in words, and the one thing to do about it right there. */
export interface IPhoneRow {
  device: IPhoneDevice;
  text: string;
  tone: IPhoneTone;
  action?: "pair" | "prepare" | "cancel";
  /** Prepare is offered: what it does (nothing reinstalled or erased) goes under the row. */
  prepare?: boolean;
  /** Paired: Forget is in the row's menu. */
  forget: boolean;
}

/** What the section shows below the switch (nothing while it is off). */
export type IPhoneView =
  | { kind: "off" }
  | { kind: "offline" }
  | { kind: "installing"; text: string }
  | { kind: "notMac" }
  | { kind: "install"; text: string; retry: boolean }
  | { kind: "devices"; noXcode: boolean; note?: string; failed?: string; rows: IPhoneRow[]; consentHint: boolean };

/** A job running, or the one just asked for while its request is on the way (shown at once, principle 7). */
function running(p: MachineIPhone, asked?: { action: IPhoneAction; device?: string }): IPhoneJob | undefined {
  if (p.job?.state === "running") return p.job;
  if (asked && asked.action !== "cancel" && asked.action !== "forget") return { kind: asked.action, state: "running", device: asked.device };
  return undefined;
}

export function iphoneView(p: MachineIPhone, online: boolean, mac: boolean, asked?: { action: IPhoneAction; device?: string }): IPhoneView {
  if (!p.enabled) return { kind: "off" };
  if (!online) return { kind: "offline" };
  const job = running(p, asked);
  if (job?.kind === "install") return { kind: "installing", text: job.step || t("lib-iphone.installing") };
  // Linux has no developer disk image unless one was put there (PERSONA_IPHONE_DDI_DIR): nothing to install for yet.
  if (!mac && p.ddi_present !== true) return { kind: "notMac" };
  if (p.installed === false) {
    const failed = p.job?.kind === "install" && p.job.state === "failed" ? p.job : undefined;
    return { kind: "install", text: failed ? installFailed(failed) : t("lib-iphone.not_installed"), retry: !!failed };
  }
  const noXcode = mac && p.ddi_present === false;
  const rows = (p.devices ?? []).map((d) => iphoneRow(d, job, noXcode));
  const last = p.job?.state === "failed" ? p.job : undefined;
  const failed = last?.kind === "pair" ? pairFailed(last) : last?.kind === "prepare" ? prepareFailed(last) : undefined;
  const note = p.note && !(p.code === "bridge_missing" || p.code === "none_paired") ? p.note : undefined;
  return { kind: "devices", noXcode, note, failed, rows, consentHint: rows.some((r) => r.device.paired && r.device.ready && !r.device.allowed) };
}

export function iphoneRow(d: IPhoneDevice, job: IPhoneJob | undefined, noXcode: boolean): IPhoneRow {
  const forget = !!d.paired;
  const mine = job && (job.device ? job.device === d.id : job.kind === "pair" && !d.paired && !!d.usb);
  if (mine && job.kind === "pair") return { device: d, text: pairing(job), tone: "work", action: "cancel", forget: false };
  if (mine && job.kind === "prepare") return { device: d, text: t("lib-iphone.preparing"), tone: "work", action: "cancel", forget };
  if (!d.paired) {
    if (d.usb) return { device: d, text: t("lib-iphone.on_cable"), tone: "need", action: job ? undefined : "pair", forget };
    return { device: d, text: d.note || t("lib-iphone.not_connected"), tone: "plain", forget };
  }
  if (d.code === "developer_mode_off" || (d.developer_mode === false && !d.ready)) return { device: d, text: t("lib-iphone.developer_mode_off"), tone: "need", forget };
  if (d.code === "ddi_not_mounted") {
    if (d.auto_prepare) return { device: d, text: t("lib-iphone.preparing_again"), tone: "work", forget };
    if (noXcode) return { device: d, text: t("lib-iphone.needs_xcode"), tone: "need", forget };
    return { device: d, text: t("lib-iphone.not_ready"), tone: "need", action: job ? undefined : "prepare", prepare: true, forget };
  }
  if (d.locked || d.code === "locked") return { device: d, text: t("lib-iphone.locked"), tone: "need", forget };
  if (!d.connected) return { device: d, text: d.note || t("lib-iphone.not_connected"), tone: "plain", forget };
  if (!d.ready) return { device: d, text: d.note || t("lib-iphone.not_ready"), tone: "plain", forget };
  if (!d.allowed) return { device: d, text: t("lib-iphone.waiting_consent"), tone: "need", forget };
  const how = d.transport === "network" ? "lib-iphone.ready_wifi" : d.transport === "usb" ? "lib-iphone.ready_cable" : "lib-iphone.ready";
  return { device: d, text: t(how), tone: "ok", forget };
}

/** A pairing in progress, by what it waits for from the person at the phone. */
function pairing(job: IPhoneJob): string {
  if (job.code === "trust_pending") return t("lib-iphone.pair_trust");
  if (job.code === "locked") return t("lib-iphone.pair_unlock");
  return job.step || t("lib-iphone.pairing");
}

function pairFailed(job: IPhoneJob): string {
  if (job.code === "timeout") return t("lib-iphone.pair_timeout");
  return t("lib-iphone.pair_failed", { error: job.error || job.code || t("lib-iphone.unknown_error") });
}

function prepareFailed(job: IPhoneJob): string {
  if (job.code === "ddi_missing") return t("lib-iphone.prepare_failed_ddi");
  if (job.code === "tss_failed") return t("lib-iphone.prepare_failed_tss");
  if (job.code === "developer_mode_off") return t("lib-iphone.developer_mode_off");
  if (job.code === "locked") return t("lib-iphone.locked");
  return t("lib-iphone.prepare_failed", { error: job.error || job.code || t("lib-iphone.unknown_error") });
}

function installFailed(job: IPhoneJob): string {
  if (job.code === "download_failed") return t("lib-iphone.install_failed_download");
  if (job.code === "checksum_mismatch" || job.code === "bad_helper") return t("lib-iphone.install_failed_check");
  return t("lib-iphone.install_failed", { error: job.error || job.code || t("lib-iphone.unknown_error") });
}

/**
 * How often to ask again while the tab is shown, in ms (0: not at all): every 2 s while a job runs, every 5 s while
 * the state can change only at the iPhone (plugged in, Developer Mode, unlocked, its consent), so the page follows
 * the person there without a reload.
 */
export function iphonePollEvery(p: MachineIPhone, online: boolean, mac: boolean): number {
  if (!online || !p.enabled) return 0;
  if (p.job?.state === "running") return 2000;
  if (p.installed === false || (!mac && p.ddi_present !== true)) return 0;
  const devices = p.devices ?? [];
  if (!devices.length) return 5000;
  return devices.some((d) => d.paired && !(d.ready && d.allowed)) ? 5000 : 0;
}

/** The one of the two answers the hub gave last (by `checked`): the machine list's push or the page's own read. */
export function newerIPhone(fresh: MachineIPhone | null, listed: MachineIPhone): MachineIPhone {
  if (!fresh) return listed;
  const a = Date.parse(fresh.checked ?? ""), b = Date.parse(listed.checked ?? "");
  return Number.isFinite(a) && Number.isFinite(b) && b > a ? listed : fresh;
}
