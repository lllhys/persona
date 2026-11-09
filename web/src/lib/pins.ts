// Pinned spaces and agents (GET / PUT /api/v1/pins, the same shape as /mutes; iOS PinsStore): the hub keeps them, so
// the phone and every browser see the same. Keys are the hub's: "machine/space name" (herdr workspace ids change when
// herdr restarts, names do not) and "machine/session id". The newest pin comes first.
import type { Session } from "./types";

export interface AgentPins {
  spaces: string[];
  sessions: string[];
}

export const NO_PINS: AgentPins = { spaces: [], sessions: [] };

export const agentKey = (s: Pick<Session, "machine_id" | "session_id">) => `${s.machine_id}/${s.session_id}`;
export const spaceKey = (machineId: string, spaceName: string) => `${machineId}/${spaceName}`;

/** The pins as the hub sent them (an older hub, or a missing list, is no pins). */
export function pinsOf(p: Partial<AgentPins> | null | undefined): AgentPins {
  return { spaces: p?.spaces ?? [], sessions: p?.sessions ?? [] };
}

export function isPinned(s: Pick<Session, "machine_id" | "session_id">, p: AgentPins): boolean {
  return p.sessions.includes(agentKey(s));
}

export function isSpacePinned(machineId: string, spaceName: string | undefined, p: AgentPins): boolean {
  return Boolean(spaceName && p.spaces.includes(spaceKey(machineId, spaceName)));
}

/** A key taken out of the list, or put first (iOS PinsStore.toggle). */
export function toggled(list: string[], key: string): string[] {
  return list.includes(key) ? list.filter((k) => k !== key) : [key, ...list];
}

export function togglePin(s: Pick<Session, "machine_id" | "session_id">, p: AgentPins): AgentPins {
  return { ...p, sessions: toggled(p.sessions, agentKey(s)) };
}

export function toggleSpacePin(machineId: string, spaceName: string, p: AgentPins): AgentPins {
  return { ...p, spaces: toggled(p.spaces, spaceKey(machineId, spaceName)) };
}

/** The pinned agents among `sessions`, in pin order (the newest pin first). */
export function pinnedSessions(sessions: Session[], p: AgentPins): Session[] {
  if (!p.sessions.length) return [];
  const order = new Map(p.sessions.map((k, i) => [k, i]));
  return sessions.filter((s) => order.has(agentKey(s))).sort((a, b) => order.get(agentKey(a))! - order.get(agentKey(b))!);
}
