// The mounted chat owns its transcript reads and socket subscription. A side panel subscribes to that owner's current
// object instead of opening a second hook with a different cache and history window (docs/protocol/protocol.md).
import { useSyncExternalStore } from "react";
import type { Transcript } from "./useTranscript";

type Entry = { owner: object; tr: Transcript };
const entries = new Map<string, Entry>();
const listeners = new Set<() => void>();
const keyOf = (mid: string, sid: string) => `${mid}/${sid}`;

export function publishTranscript(owner: object, mid: string, sid: string, tr: Transcript) {
  const key = keyOf(mid, sid);
  if (entries.get(key)?.tr === tr) return;
  entries.set(key, { owner, tr });
  for (const listener of listeners) listener();
}

export function unpublishTranscript(owner: object, mid: string, sid: string) {
  const key = keyOf(mid, sid);
  if (entries.get(key)?.owner !== owner) return;
  entries.delete(key);
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export function useSharedTranscript(mid: string, sid: string): Transcript | null {
  return useSyncExternalStore(subscribe, () => entries.get(keyOf(mid, sid))?.tr ?? null, () => null);
}
