// Each online machine's terminal panes (`GET /machines/{mid}/panes`): read when the consumer mounts, every few seconds
// while the page is in front, and on demand. The "Terminal" page and the sidebar's terminal tree (when turned on) read them.
import { useCallback, useEffect, useRef, useState } from "react";
import { api, enc, errorText } from "./api";
import type { PanesResponse } from "./terminals";
import type { Machine } from "./types";

/** How often the layout is read again while the page is in front (agents' states are live already, from the sessions). */
export const REFRESH_MS = 8000;

export interface Listing {
  data?: PanesResponse;
  error?: string;
}

const sameLayout = (a: PanesResponse | undefined, b: PanesResponse) => a !== undefined && JSON.stringify(a) === JSON.stringify(b);

/** `enabled` false reads nothing (the sidebar's tree, off). */
export function usePanes(machines: Machine[], enabled = true) {
  const [listing, setListing] = useState<Record<string, Listing>>({});
  const [busy, setBusy] = useState(false);
  const online = enabled ? machines.filter((m) => m.online).map((m) => m.id) : [];
  const key = online.join("\n");

  // Per machine: the number of the last read started, and of the last reply shown. Reads overlap (the timer, refresh,
  // reload), so a reply older than the one shown is dropped instead of putting an out-of-date list (or error) back.
  const started = useRef(new Map<string, number>());
  const shown = useRef(new Map<string, number>());
  // The busy read that clears `busy`: only the last one started does, and a new key or unmount drops it.
  const busyToken = useRef(0);

  const load = useCallback(async (ids: string[]) => {
    await Promise.all(
      ids.map((mid) => {
        const n = (started.current.get(mid) ?? 0) + 1;
        started.current.set(mid, n);
        const fresh = () => {
          if (n < (shown.current.get(mid) ?? 0)) return false;
          shown.current.set(mid, n);
          return true;
        };
        // A reply that repeats what is shown keeps the object (and so the trees built from it and every row): the
        // layout rarely changes between two reads, and each machine's reply would re-render the page or the sidebar.
        return api<PanesResponse>(`/machines/${enc(mid)}/panes`).then(
          (data) => fresh() && setListing((l) => (l[mid] && l[mid].error === undefined && sameLayout(l[mid].data, data) ? l : { ...l, [mid]: { data } })),
          // a failed read keeps what was seen: the list does not blink out
          (e) => {
            if (!fresh()) return;
            const error = errorText(e);
            setListing((l) => (l[mid]?.error === error ? l : { ...l, [mid]: { data: l[mid]?.data, error } }));
          },
        );
      }),
    );
  }, []);

  const loadBusy = useCallback(
    async (ids: string[]) => {
      const token = ++busyToken.current;
      setBusy(true);
      try {
        await load(ids);
      } finally {
        if (busyToken.current === token) setBusy(false);
      }
    },
    [load],
  );

  useEffect(() => {
    const ids = key ? key.split("\n") : [];
    if (!ids.length) return;
    void loadBusy(ids);
    const t = setInterval(() => document.visibilityState === "visible" && void load(ids), REFRESH_MS);
    return () => {
      clearInterval(t);
      // the read still out belongs to the old key: it no longer holds `busy` (with no machine left, nothing would clear it)
      busyToken.current++;
      setBusy(false);
    };
  }, [key, load, loadBusy]);

  const refresh = useCallback(() => loadBusy(key ? key.split("\n") : []), [key, loadBusy]);
  return { listing, busy, refresh, reload: load };
}
