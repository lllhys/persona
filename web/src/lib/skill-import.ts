import { t } from "../i18n";
import { api, ApiError } from "./api";
import type { Skill } from "./hub-update";
import { MANAGER_HEADER } from "./manager-choice";

export const MAX_SKILL_ZIP = 4 << 20;

// The same read-only inventory API. Keep this upload editor's abort/auth fencing in the
// lazy Settings module, not hub-update's shared first-screen module.
export const skillImportList = (manager: string, signal: AbortSignal, current: () => boolean) =>
  api<{ machine_id: string; machine?: string; skills: Skill[] | null }>("/skills", {
    headers: { [MANAGER_HEADER]: manager }, signal, isAuthContextCurrent: current,
  });

export interface SkillImportResult {
  name: string;
  description: string;
  destination: string;
  machine: string;
  files: number;
  bytes: number;
  reference: string;
  environment: string;
  expires_at: string;
  recovery?: string;
}

export interface SkillImportRequest {
  op: "preview" | "install" | "cancel" | "undo";
  archive?: string;
  reference?: string;
  environment?: string;
}

export const importSkill = (manager: string, body: SkillImportRequest, signal?: AbortSignal, current?: () => boolean) =>
  api<SkillImportResult>("/skills/import", {
    method: "POST", json: body, headers: { [MANAGER_HEADER]: manager }, signal, isAuthContextCurrent: current,
  });

/** No browser ZIP parser: only bounded bytes go to the execution computer's validator. */
export function readSkillZIP(file: File, signal: AbortSignal): Promise<string> {
  if (!file.size || file.size > MAX_SKILL_ZIP) return Promise.reject(new Error("skill_zip_limits"));
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    const abort = () => { reader.abort(); reject(new DOMException("Aborted", "AbortError")); };
    reader.onload = () => {
      signal.removeEventListener("abort", abort);
      resolve(String(reader.result).split(",")[1]);
    };
    reader.onerror = () => { signal.removeEventListener("abort", abort); reject(new Error("skill_import_io")); };
    reader.onabort = () => { signal.removeEventListener("abort", abort); reject(new DOMException("Aborted", "AbortError")); };
    if (signal.aborted) return abort();
    signal.addEventListener("abort", abort, { once: true });
    reader.readAsDataURL(file);
  });
}

/** Never render arbitrary server/filesystem details or untrusted manifest text as HTML. */
export function skillImportError(error: unknown): string {
  const code = error instanceof ApiError ? error.code : error instanceof Error ? error.message : "";
  const keys: Record<string, string> = {
    skill_zip_limits: "error_limits", skill_zip_invalid: "error_zip", skill_zip_path: "error_path",
    skill_zip_type: "error_type", skill_manifest_layout: "error_layout", skill_manifest_utf8: "error_utf8",
    skill_manifest_yaml: "error_yaml", skill_manifest_name: "error_name", skill_manifest_description: "error_description",
    skill_name_unsafe: "error_unsafe_name", skill_conflict: "error_conflict", skill_import_expired: "error_expired",
    skill_import_changed: "error_changed", skill_environment_changed: "error_environment", skill_undo_changed: "error_undo",
    skill_import_busy: "error_busy", skill_import_io: "error_io", skill_import_request: "error_request",
    no_machine: "error_machine", machine_pending: "error_machine", node_unreachable: "error_machine",
    node_outdated: "error_outdated", forbidden: "error_forbidden", timeout: "error_timeout",
  };
  return t(`settings-skills.${keys[code ?? ""] ?? "error_unknown"}`);
}
