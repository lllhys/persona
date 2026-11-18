// "Session info" (iOS SessionInfoSheet), a chunk of its own: SessionDialogs.tsx loads it on the dialog's first open, so its
// fetch, its rows and the Persona conversation's standing permissions are not the first screen's, nor their words (the
// chat-sessioninfo namespace, which the first screen's catalog leaves to the rest) (#8261).
import { useEffect, useState } from "react";
import { api, errorText, sessionPath } from "../../lib/api";
import { ago, externalLink, resumeCommand, shortPath } from "../../lib/format";
import { openLink } from "../../lib/links";
import { effortTitle, listedModeOption, listedModesOf, modeTitle } from "../../lib/session-actions";
import { formatTokens } from "../../lib/tokens";
import type { Session } from "../../lib/types";
import { Button, copyText, cx, Dialog, Loading } from "../../ui/atoms";
import { t } from "../../i18n";
import { AlwaysPermissions } from "./AlwaysPermissions";
import { DelegatedHistory } from "./Delegated";

interface SessionInfo {
  session_id?: string;
  transcript_path?: string;
  cwd?: string;
  model?: string;
  launcher?: string;
  git_branch?: string;
  version?: string;
  permission_mode?: string;
  effort?: string;
  context_tokens?: number;
  output_tokens?: number;
  compactions?: number;
  turns?: number;
  /** Nobody gives its counts (a runtime's session with no transcript on the computer, #8261): the zeros beside it stand
   *  in for older apps and are not shown. */
  usage_unknown?: boolean;
  started_at?: string;
  last_activity?: string;
}

// Token counts as the Machines page and the apps write them (1.2K, 1.23M: lib/tokens.ts) with a localized unit; "" for a
// count nobody gave.
const tokens = (n?: number) => (n === undefined ? "" : t("chat-sessioninfo.tokens", { value: formatTokens(n) }));

// How it runs, how much it used, where it lives; ids and the resume command copy.
export function SessionInfoDialog({ session, machineName, open, onClose }: { session: Session; machineName: string; open: boolean; onClose: () => void }) {
  const [info, setInfo] = useState<SessionInfo | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!open) return;
    let live = true;
    setInfo(null);
    setError("");
    api<SessionInfo>(`${sessionPath(session.machine_id, session.session_id)}/info`)
      .then((r) => live && setInfo(r))
      .catch((e) => live && setError(errorText(e)));
    return () => {
      live = false;
    };
  }, [open, session.machine_id, session.session_id]);

  // counts nobody gives (#8261) are left out, not shown as zeros; a session that lists its own modes runs in one of them
  const counted = info && !info.usage_unknown ? info : undefined;
  const listed = listedModesOf(session);
  const mode = listed ? listedModeOption(info?.permission_mode && listed.includes(info.permission_mode) ? info.permission_mode : "default").title : modeTitle(info?.permission_mode);
  const groups: { title: string; rows: [string, string | undefined, boolean?][] }[] = info
    ? [
        { title: t("chat-sessioninfo.group_run"), rows: [[t("chat-sessioninfo.model"), info.model], [t("chat-sessioninfo.launcher"), info.launcher || session.launcher], [t("chat-sessiondialogs.permission_mode"), mode], [t("chat-sessioninfo.effort"), effortTitle(info.effort)], [t("chat-sessioninfo.version"), info.version, true]] },
        { title: t("chat-sessioninfo.group_usage"), rows: [[t("chat-sessioninfo.context"), counted?.context_tokens ? tokens(counted.context_tokens) : ""], [t("chat-sessioninfo.output"), tokens(counted?.output_tokens)], [t("chat-sessioninfo.turns"), counted?.turns !== undefined ? String(counted.turns) : ""], [t("chat-sessioninfo.compaction"), counted?.compactions ? t("chat-sessioninfo.compactions", { count: counted.compactions }) : ""], [t("chat-sessioninfo.started"), info.started_at && ago(info.started_at)], [t("chat-sessioninfo.last_activity"), info.last_activity && ago(info.last_activity)]] },
        { title: t("chat-sessioninfo.group_location"), rows: [[t("chat-sessioninfo.machine"), machineName], [t("chat-sessiondialogs.folder"), info.cwd ? shortPath(info.cwd) : undefined, true], [t("chat-sessioninfo.branch"), info.git_branch, true], [t("chat-sessioninfo.transcript"), info.transcript_path ? shortPath(info.transcript_path) : undefined, true]] },
      ]
    : [];

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t("chat-sessioninfo.info_title")}
      width={480}
      footer={
        <>
          <Button tone="quiet" icon="copy" onClick={() => copyText(session.session_id)}>
            {t("chat-sessioninfo.copy_session_id")}
          </Button>
          {externalLink(session) && (
            <Button tone="secondary" icon="external" onClick={() => openLink(externalLink(session)!)}>
              {t("chat-sessioninfo.open_in_lark")}
            </Button>
          )}
          {/* none for a runtime's session (resumeCommand): its runtime keeps it */}
          {!session.assistant && resumeCommand(session) && (
            <Button tone="secondary" icon="terminal" onClick={() => copyText(resumeCommand(session))}>
              {t("chat-sessioninfo.copy_resume_command")}
            </Button>
          )}
        </>
      }
    >
      {!info && !error && (
        <div className="flex justify-center py-6">
          <Loading />
        </div>
      )}
      {error && <p className="py-4 text-[13px] text-red">{error}</p>}
      <div className="flex flex-col gap-3">
        {open && session.assistant && <AlwaysPermissions key={session.session_id} session={session.session_id} />}
        {open && !session.assistant && <DelegatedHistory key={`${session.machine_id}/${session.session_id}`} session={session} />}
        {groups.map((g) => {
          const rows = g.rows.filter(([, v]) => v);
          if (!rows.length) return null;
          return (
            <section key={g.title}>
              <h3 className="mb-1 text-submeta font-medium text-ink-3">{g.title}</h3>
              <div className="divide-y divide-line overflow-hidden rounded-[10px] bg-inset shadow-hairline">
                {rows.map(([k, v, mono]) => (
                  <div key={k} className="flex items-center gap-3 px-3 py-1.5 text-[12.5px]">
                    <span className="w-16 shrink-0 text-ink-3">{k}</span>
                    <span title={v} className={cx("min-w-0 flex-1 truncate text-ink", mono && "font-mono text-[12px]")}>
                      {v}
                    </span>
                  </div>
                ))}
              </div>
            </section>
          );
        })}
      </div>
    </Dialog>
  );
}
