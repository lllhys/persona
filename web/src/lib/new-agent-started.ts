// New agent's onStarted (#5933; lib/new-agent.ts NewAgentPreset): kept out of the launch code, which the first screen
// loads, and loaded with the New agent dialog.
import { getState, subscribe } from "./store";
import type { Session } from "./types";

/** Tells `onStarted` about the launch `id` once it is ready (started, its first message sent), once. A launch dropped before
 *  that tells nothing; a reload forgets the wait, and the launch goes on without it. */
export function whenStarted(id: string, onStarted: (session: Session) => void) {
  const check = () => {
    const launch = getState().launches[id];
    if (launch?.phase === "ready" && launch.session) {
      off();
      onStarted(launch.session);
    } else if (!launch) off();
  };
  const off = subscribe(check);
  check();
}
