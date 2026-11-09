// Busy indicators under reduced motion. styles.css strips every animation to ~1ms × 1 iteration for people who asked the
// system for less motion; that also froze the spinners and the working dot, so "something is happening" vanished.
// An element that carries this class is exempt from the strip: it keeps going, at a slow 2.4s (the pace Moshi slows
// its loader to), while the movement, scale and slide animations stay off. Mark anything whose only job is to say
// "busy" or "trying": spinners, the working / connecting dots, the loader's shimmer and grid. busy-motion.test.ts
// fails for a looping busy animation in the source that is not marked.
import type { CSSProperties } from "react";

/** The class that exempts an element from the reduced-motion strip (styles.css, `.busy-motion`). */
export const BUSY_MOTION = "busy-motion";

/** How many turns the spinners' keyframes (styles.css `spin-long`) make in one cycle. A CSS animation that loops sends
 *  an `animationiteration` event every cycle, and React listens for those at the root, so each turn of each spinner
 *  cost the page a style recalc (8 marks turning: ~8% of an idle main thread). Many turns per cycle at the same speed
 *  look the same and send one event every minute or so. styles.css has the same number; under reduced motion the
 *  cycle is this many times 2.4s (`--busy-cycles`). */
export const SPIN_TURNS = 100;

/** The style of a spinner that turns once every `turnMs` (it also carries BUSY_MOTION). */
export function spinStyle(turnMs: number): CSSProperties {
  return { animation: `spin-long ${turnMs * SPIN_TURNS}ms linear infinite`, "--busy-cycles": SPIN_TURNS } as CSSProperties;
}
