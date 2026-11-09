// Interconnected hubs (docs/archive/operations/plan-hub-mesh.md 2.6): the hub serving this page lists every computer of its group as
// its own. Which hub a computer belongs to is only worth saying when the list spans more than one hub: with a single
// hub_id (or none, an older hub) the page looks exactly as before.
import { t } from "../i18n";
import type { Machine } from "./types";

/** How many hubs the machine list spans (machines without a hub_id count as this hub). */
export function hubCount(machines: Machine[]): number {
  return new Set(machines.map((m) => m.hub_id ?? "")).size;
}

/** The hub name to show next to a machine, or "" when the list has a single hub. */
export function hubLabel(machine: Machine | undefined, machines: Machine[]): string {
  if (!machine || hubCount(machines) < 2) return "";
  return machine.hub_name || machine.hub_id || "";
}

/** "Home · MacBook" when hubs are worth naming, else just the machine's name. */
export function machineWithHub(machineId: string, machines: Machine[]): string {
  const m = machines.find((x) => x.id === machineId);
  const name = m?.name ?? machineId;
  const hub = hubLabel(m, machines);
  return hub ? `${hub} · ${name}` : name;
}

const VIA: Record<string, string> = { local: t("lib-mesh.via_local"), ssh: "SSH", tailcat: "Tailcat", link: t("lib-mesh.via_link"), peer: t("lib-mesh.via_peer") };

/** How the hub reaches a computer: "via link · 1 hop · 24 ms" (only when hubs are worth naming). */
export function routeText(machine: Machine | undefined, machines: Machine[]): string {
  if (!machine?.route || hubCount(machines) < 2) return "";
  const r = machine.route;
  return [VIA[r.via] ?? r.via, r.hops > 0 ? t("lib-mesh.hops", { count: r.hops }) : "", r.rtt_ms !== undefined ? `${r.rtt_ms} ms` : ""].filter(Boolean).join(" · ");
}
