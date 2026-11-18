// The fallback card's restore action, loaded with that card rather than with the eager recovery-state helpers.
import { api, ApiError, errorText, sessionPath } from "./api";
import { t } from "../i18n";

export interface RestoreModelResult {
  restore: string;
  restore_to?: string;
  /** An idle process was closed (false: none was running; the next start uses the configured model all the same). */
  closed: boolean;
}

/** POST …/restore-model, no body: whatever the page could say is not sent. The card waits for the session's next push. */
export function restoreModel(mid: string, sid: string): Promise<RestoreModelResult> {
  return api<RestoreModelResult>(`${sessionPath(mid, sid)}/restore-model`, { method: "POST" });
}

/** A refusal of the restore in the page's language, by its code; the node's own words for anything else. */
export function restoreErrorText(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case "restore_busy":
        return t("chat-refusal.err_busy");
      case "restore_unsupported":
        return t("chat-refusal.err_unsupported");
      case "restore_nothing":
        return t("chat-refusal.err_nothing");
      case "restore_unknown":
        return t("chat-refusal.err_unknown");
      case "node_outdated":
        return t("chat-refusal.err_outdated");
    }
  }
  return errorText(e);
}
