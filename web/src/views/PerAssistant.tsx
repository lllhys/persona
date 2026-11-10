// A page that reads the current assistant's things once when it opens (lib/persona-nav.ts reopensOnSwitch) opens again
// for another assistant made current, or for the default when a kept pick left the roster: what it shows and what it
// sends are the current one's, never the last one's.
import { Fragment, type ReactNode } from "react";
import { useChosenManager } from "../lib/manager-choice";
import { reopensOnSwitch } from "../lib/persona-nav";
import type { Route } from "../lib/router";

export function PerAssistant({ route, children }: { route: Route; children: ReactNode }) {
  const picked = useChosenManager();
  return reopensOnSwitch(route) ? <Fragment key={picked ?? ""}>{children}</Fragment> : <>{children}</>;
}
