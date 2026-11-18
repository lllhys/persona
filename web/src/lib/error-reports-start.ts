// Wires the page's error reporter (lib/error-reports.ts) to the hub: reports go to POST /api/v1/error-reports, and the
// hello's `features` say whether to send them at all. A frozen page (lib/freeze-watch.ts) uses the same gate.
import { api, ApiError } from "./api";
import { ERROR_REPORTS_FEATURE, reportFreeze, setFreezeDropListener, startErrorReports, type SendResult } from "./error-reports";
import { startFreezeWatch } from "./freeze-watch";
import { devModeSince, watchSockets } from "./dev-probe";
import type { ErrorReport } from "./extension-wire";
import { uuid } from "./format";
import { getState, subscribe } from "./store";

export async function sendErrorReports(reports: ErrorReport[]): Promise<SendResult> {
  try {
    const r = await api<{ enabled?: boolean }>("/error-reports", { method: "POST", json: { reports } });
    return { ok: true, enabled: r.enabled !== false };
  } catch (e) {
    return { ok: false, status: e instanceof ApiError ? e.status : 0 };
  }
}

startErrorReports({
  send: sendErrorReports,
  subscribe,
  features: () => {
    const s = getState();
    return { online: s.conn === "online", features: s.features };
  },
  uuid,
});

// After the reporter, so a hello applies the feature before the watch starts and a buffered freeze can be queued.
// A warning still in the queue when the feature turns off did not count: watch again if it comes back.
const watchFreezes = startFreezeWatch({
  subscribe,
  enabled: () => getState().features.includes(ERROR_REPORTS_FEATURE),
  report: reportFreeze,
});
setFreezeDropListener(() => watchFreezes.rearm());

// Developing Persona (docs/operations/developing-persona.md): with the switch on, the socket is wrapped before the app
// opens it, and the recorder's chunk loads. This module is not part of the first screen's budget.
if (devModeSince()) {
  watchSockets();
  void import("./dev-mode").then((m) => m.startDevMode());
}
