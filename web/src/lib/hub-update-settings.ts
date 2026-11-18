// Settings-only presentation loads with the page; the first screen needs only Hub notices and update states.
import { t } from "../i18n";
import { updateWorking, type HubUpdateStatus } from "./hub-update";

export const DOCKER_COMMAND = "docker compose pull && docker compose up -d";

export function channelTitle(channel: string): string {
  return channel === "release" ? t("lib-hub-update.channel_release") : channel === "docker" ? "Docker" : t("lib-hub-update.channel_dev");
}

export function recordTitle(result: string): string {
  return t(result === "updated" ? "lib-hub-update.result_updated" : result === "rolled_back" ? "lib-hub-update.result_rolled_back" : result === "held" ? "lib-hub-update.result_held" : "lib-hub-update.result_failed");
}

export function recordTone(result: string): "green" | "orange" | "red" {
  return result === "updated" ? "green" : result === "rolled_back" || result === "held" ? "orange" : "red";
}

/** What the "Update" group shows: a Docker hub's instructions, why it doesn't update itself, a newer release, an update
 *  under way, or "Up to date". */
export type UpdateView =
  | { kind: "docker" }
  | { kind: "cannot"; why: string }
  | { kind: "newer"; working: boolean }
  | { kind: "working" }
  | { kind: "current" };

export function updateView(s: HubUpdateStatus): UpdateView {
  if (s.channel === "docker") return { kind: "docker" };
  if (!s.can_update) return { kind: "cannot", why: s.why_not ? t("lib-hub-update.cannot_update_why", { why: s.why_not }) : t("lib-hub-update.cannot_update") };
  const working = updateWorking(s);
  if (s.latest?.newer) return { kind: "newer", working };
  return working ? { kind: "working" } : { kind: "current" };
}

/** The warning under a newer release: it went back before, or auto-update holds it back. */
export function latestWarning(s: HubUpdateStatus): string | null {
  const l = s.latest;
  if (!l?.newer) return null;
  if (l.bad) return t("lib-hub-update.warning_bad");
  if (l.held && s.auto) return t("lib-hub-update.warning_held", { why: l.held });
  return null;
}

/** The line in place of "Up to date" when the update source looks stopped (`source_behind`, docs/operations/hub-update.md
 *  "Update source"): it names the source and where it came from, and hub.json's update.url, which sets another. */
export function sourceBehindWarning(s: HubUpdateStatus): string | null {
  if (!s.source_behind || !s.latest || !s.update_url) return null;
  const from = t(s.update_url_from === "hub.json" ? "lib-hub-update.source_from_config" : "lib-hub-update.source_from_build");
  return t("lib-hub-update.warning_source_behind", { url: s.update_url, from, version: s.latest.version });
}

/** The lines for a Docker hub: what to say and what to copy. */
export function dockerSteps(s: HubUpdateStatus): { headline?: string; steps: { text: string; copy: string }[] } {
  const l = s.latest;
  if (l?.newer) {
    if (s.image?.pinned) {
      return {
        headline: t("lib-hub-update.new_image", { version: l.version }),
        steps: [
          { text: t("lib-hub-update.docker_set_image"), copy: `${s.image.name}:${l.version}` },
          { text: t("lib-hub-update.docker_then_run"), copy: DOCKER_COMMAND },
        ],
      };
    }
    return { headline: t("lib-hub-update.new_image", { version: l.version }), steps: [{ text: t("lib-hub-update.docker_run"), copy: DOCKER_COMMAND }] };
  }
  return { steps: [{ text: t("lib-hub-update.docker_how"), copy: DOCKER_COMMAND }] };
}
