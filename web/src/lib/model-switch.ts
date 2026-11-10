// The model picker merged into the composer's "Thinking" chip (issue #2434, after Moshi's `model · effort` pill): one popover
// with the effort tabs on top and the model list below. The list is what Persona already asks the launcher's agent for
// (lib/launcher-models.ts); applying sends POST …/modes {model} and then {effort}, and the node types `/model <id>`
// into Claude Code through the same guards as a message (server/internal/node/model_switch.go). Pure helpers and the
// small event bus that lets the composer open the popover when `/model` is entered.
import { t } from "../i18n";
import { sessionCan, stateOf } from "./format";
import { composing, type KeyLike } from "./send-history";
import { effortChoices, type AgentModel, type LauncherModels } from "./launcher-models";
import type { Session } from "./types";

/** Explicit runtime capability takes precedence. Older sources retain the Claude Code terminal heuristic.
 * Persona conversations use their own model controls. */
export function canSwitchModel(s: Session | undefined): boolean {
  if (!s || s.assistant) return false;
  if (s.capabilities?.model !== undefined) return s.capabilities.model && sessionCan(s, "send") && stateOf(s) !== "offline";
  if (s.source_kind) return false;
  if (s.agent && s.agent !== "claude") return false;
  return sessionCan(s, "send") && stateOf(s) !== "offline";
}

/** Why the model cannot be switched right now, "" when it can (the node refuses the same cases). */
export function modelBusy(s: Session): string {
  const st = stateOf(s);
  return st === "working" || st === "needs_you" ? t("composer.busy") : "";
}

/** The launcher whose list the picker shows: the session's own, plain claude when it started without one. */
export const sessionLauncher = (s: Pick<Session, "launcher">) => s.launcher || "claude";

/** Whether `m` is the model the session runs now: `current` is what the transcript shows (a concrete model id), or
 *  the id the app switched to; the list has the alias (`opus`) and what it stands for (`resolved`). */
export function isCurrentModel(m: AgentModel, current: string | undefined): boolean {
  if (!current) return false;
  const c = current.toLowerCase();
  return [m.id, m.resolved].some((v) => v && v.toLowerCase() === c);
}

/** What the box is set to. `model`: index into the list; `effort`: index into the tabs, -1 none. */
export interface PickerState {
  model: number;
  effort: number;
}

export type PickerAction = "apply" | "cancel";

export interface PickerCounts {
  models: number;
  efforts: number;
}

/** ↑↓ move through the models, ←→ through the effort tabs (neither wraps: a tab strip has ends), Enter applies, Esc
 *  cancels. `handled` false: the key is not the popover's. */
export function pickerKey(state: PickerState, counts: PickerCounts, e: Pick<KeyLike, "key" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey" | "isComposing" | "keyCode">): { state: PickerState; action?: PickerAction; handled: boolean } {
  if (composing(e) || e.ctrlKey || e.metaKey || e.altKey) return { state, handled: false };
  switch (e.key) {
    case "ArrowDown":
      return { state: { ...state, model: counts.models ? Math.min(counts.models - 1, state.model + 1) : -1 }, handled: true };
    case "ArrowUp":
      return { state: { ...state, model: counts.models ? Math.max(0, state.model - 1) : -1 }, handled: true };
    case "ArrowRight":
      return { state: { ...state, effort: counts.efforts ? Math.min(counts.efforts - 1, state.effort + 1) : -1 }, handled: true };
    case "ArrowLeft":
      return { state: { ...state, effort: counts.efforts ? Math.max(0, state.effort - 1) : -1 }, handled: true };
    case "Enter":
      return { state, action: "apply", handled: true };
    case "Escape":
      return { state, action: "cancel", handled: true };
    default:
      return { state, handled: false };
  }
}

/** The tabs for the picked model: its own efforts (a model that takes none: no tab), all of them while the list has not
 *  loaded or lacks the model. */
export function effortTabs(list: LauncherModels | null | undefined, model: AgentModel | undefined, all: string[]): string[] {
  if (!list?.models.length || !model) return all;
  return effortChoices(list, model.id).filter((e) => all.includes(e));
}

/** What applying changes: only the model or effort that differs from the session's, so a pick that changes one sends
 *  one request. */
export function pickerPatch(o: { model?: AgentModel; current?: string; effort: string; currentEffort?: string }): { model?: string; effort?: string } {
  const patch: { model?: string; effort?: string } = {};
  if (o.model && !isCurrentModel(o.model, o.current)) patch.model = o.model.id;
  if (o.effort && o.effort !== o.currentEffort) patch.effort = o.effort;
  return patch;
}

/** `/model` alone (what the slash menu fills in, with or without the space) in the box. */
export const isModelCommand = (draft: string) => draft.trim() === "/model";

// ── opening the popover from the composer ────────────────────────────────────

type Opener = () => void;
const openers = new Map<string, Opener>();

/** The chip of the conversation `key` answers `/model` Enter with the popover; returns the unsubscribe. */
export function onModelPicker(key: string, open: Opener): () => void {
  openers.set(key, open);
  return () => {
    if (openers.get(key) === open) openers.delete(key);
  };
}

/** Opens the conversation's model popover; false when it has none (the agent has no model switch): the composer then
 *  sends the text as it always did. */
export function requestModelPicker(key: string): boolean {
  const open = openers.get(key);
  if (!open) return false;
  open();
  return true;
}
