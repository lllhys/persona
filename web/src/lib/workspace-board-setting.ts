// Settings › General › Agent › Workflow board (#2475): the board is off unless turned on. The switch is the hub's
// (GET/PUT /workspace-board/settings), so every browser agrees; a hub that has never stored it answers whether the board
// was already in use. Off only hides the board: its columns and cards stay on the hub.
import { defaultKnown, fromDefaultManager, onDefaultManager } from "./manager-choice";
import { useSyncExternalStore } from "react";
import { api, ApiError } from "./api";
import { onHubEvent } from "./store";

export interface WorkspaceBoardSetting {
  /** null until the hub answered: nothing shows the board (or says it is off) before that. */
  enabled: boolean | null;
  canWrite: boolean;
}

let state: WorkspaceBoardSetting = { enabled: null, canWrite: false };
let loading: Promise<void> | null = null;
// a read was asked for while one was out: the hub may have changed after that one was answered
let again = false;
// Every read, write and push of the switch, numbered: an answer applies only while nothing newer came after its request
// (a read on its way when the user flips the switch, or a write overtaken by the next flip, would put an old value back).
let rev = 0;
const listeners = new Set<() => void>();

function set(next: WorkspaceBoardSetting) {
  if (next.enabled === state.enabled && next.canWrite === state.canWrite) return;
  state = next;
  listeners.forEach((l) => l());
}

export function getWorkspaceBoardSetting(): WorkspaceBoardSetting {
  return state;
}

/**
 * Reads the switch again (on every hello: another hub, or one that came back, may say otherwise). Asked for while a read
 * is out, it reads once more after that one (what changed while the socket was down is not lost), and the promise it
 * returns covers that read too.
 */
export function refreshWorkspaceBoardSetting(): Promise<void> {
  if (loading) {
    again = true;
    return loading;
  }
  loading = (async () => {
    try {
      do {
        again = false;
        await readSwitch();
      } while (again);
    } finally {
      loading = null;
    }
  })();
  return loading;
}

async function readSwitch(): Promise<void> {
  const mine = ++rev;
  try {
    const r = await api<{ enabled?: boolean; can_write?: boolean }>("/workspace-board/settings");
    if (mine === rev) set({ enabled: r.enabled === true, canWrite: r.can_write === true });
  } catch (e) {
    // a hub from before the switch showed the board to everyone and has nothing to turn it off with
    if (e instanceof ApiError && e.status === 404 && mine === rev) set({ enabled: true, canWrite: false });
    // anything else: keep what we had, the next hello asks again
  }
}

/** Turns the board on or off for this hub; shown at once, put back (and thrown) when the hub refuses. */
export async function setWorkspaceBoardEnabled(on: boolean): Promise<void> {
  const before = state;
  const mine = ++rev;
  set({ ...state, enabled: on });
  try {
    const r = await api<{ enabled?: boolean; can_write?: boolean }>("/workspace-board/settings", { method: "PUT", json: { enabled: on } });
    if (mine === rev) set({ enabled: r.enabled === true, canWrite: r.can_write !== false });
  } catch (e) {
    if (mine === rev) set(before);
    throw e;
  }
}

onHubEvent("hello", () => void refreshWorkspaceBoardSetting());
// another browser or device changed it
onHubEvent("workspace_board_settings", (e) => {
  // the switch this page reads and writes is the default manager's (no X-Persona-Manager): another manager's is not it.
  // Before the roster says which is the default none is taken: the switch is read again once it does.
  if (typeof e.enabled !== "boolean" || !defaultKnown() || !fromDefaultManager(e)) return;
  rev++;
  set({ ...state, enabled: e.enabled });
});
// the default became known or moved: its switch, which may have changed while its pushes were left out
onDefaultManager(() => void refreshWorkspaceBoardSetting());

const subscribe = (l: () => void) => {
  listeners.add(l);
  if (state.enabled === null) void refreshWorkspaceBoardSetting();
  return () => {
    listeners.delete(l);
  };
};

export function useWorkspaceBoardSetting(): WorkspaceBoardSetting {
  return useSyncExternalStore(subscribe, getWorkspaceBoardSetting, getWorkspaceBoardSetting);
}

/** Only for tests: back to "not answered yet". */
export function resetWorkspaceBoardSettingForTest() {
  state = { enabled: null, canWrite: false };
  loading = null;
  again = false;
  rev = 0;
}
