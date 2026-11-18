// The closeout card's Command evidence row (#7665, docs/web/web.md "Closeout"): for each command its Verified text names,
// only the facts recorded in this closeout's own exchange (lib/closeout-commands.ts), never a badge. Its own chunk, which
// CloseoutCard loads when a card with a Verified claim draws (#8118): the matching code and its strings stay off the first
// screen. It reads the exchange's folded steps itself, while the card is open; a failed or capped read stays unavailable
// with Retry.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { t } from "../../i18n";
import { closeoutCommandClaims, closeoutCommandEvidence, type CloseoutCommandEvidence } from "../../lib/closeout-commands";
import type { Segment } from "../../lib/turns";
import type { Item, Session } from "../../lib/types";
import { Button } from "../../ui/atoms";

type ExpandFold = (fold: Item) => Promise<boolean | void>;
type ReadState = { scope: string; foldsKey: string; folds: Item[]; status: "loading" | "done" | "failed" };

/** The row (a label and its facts, in the card's two-column list), or nothing when the Verified text names no command. */
export function CloseoutEvidence({ session, verified, open, segment, expandFold }: { session: Session; verified: string; open: boolean; segment?: Segment; expandFold?: ExpandFold }) {
  const claims = useMemo(() => closeoutCommandClaims(verified), [verified]);
  const scope = `${session.machine_id}/${session.session_id}:${segment?.id ?? "unknown"}:${claims.join(" || ")}`;
  const foldsKey = segment?.folds.map((fold) => fold.id).join(" || ") ?? "";
  const [read, setRead] = useState<ReadState | null>(null);
  const request = useRef(0);
  useEffect(() => () => { request.current++; }, []);
  const load = useCallback(async (folds: Item[]) => {
    const ticket = ++request.current;
    const next = { scope, foldsKey: folds.map((fold) => fold.id).join(" || "), folds, status: "loading" as const };
    setRead(next);
    let status: ReadState["status"] = "done";
    try {
      if (!expandFold) throw new Error("fold reader unavailable");
      for (const fold of folds) if (await expandFold(fold) === false) throw new Error("fold read incomplete");
    } catch {
      status = "failed";
    }
    if (request.current === ticket) setRead({ ...next, status });
  }, [expandFold, scope]);
  useEffect(() => {
    if (!open || !claims.length || !segment?.folds.length) return;
    if (read?.scope === scope && (read.status === "failed" || read.foldsKey === foldsKey)) return;
    void load(segment.folds);
  }, [open, claims.length, segment, foldsKey, scope, read, load]);

  if (!claims.length) return null;
  const failed = read?.scope === scope && read.status === "failed";
  const loading = read?.scope === scope && read.status === "loading";
  const unavailable = failed || loading || !segment || Boolean(segment?.folds.length);
  const evidence = closeoutCommandEvidence(verified, segment).map((item) => unavailable ? { ...item, state: "unavailable" as const } : item);
  const retry = () => {
    const folds = failed && read ? read.folds : segment?.folds;
    if (folds?.length) void load(folds);
  };
  return (
    <>
      <dt className="whitespace-nowrap text-ink-3">{t("chat-closeoutevidence.title")}</dt>
      <dd className="min-w-0 text-ink-2 [overflow-wrap:anywhere]">
        <CommandEvidence
          items={evidence}
          loading={loading}
          retry={(failed || Boolean(segment?.folds.length)) && expandFold ? retry : undefined}
        />
      </dd>
    </>
  );
}

function CommandEvidence({ items, loading, retry }: { items: CloseoutCommandEvidence[]; loading: boolean; retry?: () => void }) {
  return (
    <div className="flex min-w-0 flex-col gap-2" data-closeout-evidence>
      {loading && <span role="status" className="text-ink-3">{t("chat-closeoutevidence.loading")}</span>}
      <ul className="flex min-w-0 flex-col gap-2">
        {items.map((item) => <CommandEvidenceLine key={item.claim} item={item} />)}
      </ul>
      {retry && <Button size="sm" tone="quiet" icon="retry" busy={loading} onClick={retry}>{t("chat-chatview.retry")}</Button>}
    </div>
  );
}

function CommandEvidenceLine({ item }: { item: CloseoutCommandEvidence }) {
  const facts: string[] = [];
  if (item.exitCode !== undefined) facts.push(t("chat-closeoutevidence.exit", { code: item.exitCode }));
  if (item.isError !== undefined) facts.push(t("chat-closeoutevidence.is_error", { value: String(item.isError) }));
  if (item.state === "running") facts.push(t("chat-closeoutevidence.running"));
  else if (item.background) facts.push(t("chat-closeoutevidence.background"));
  if (item.state === "not_found") facts.push(t("chat-closeoutevidence.not_found"));
  else if (item.state === "unavailable") {
    facts.push(t(item.reason === "no_result" ? "chat-closeoutevidence.no_result" : item.reason === "truncated" ? "chat-closeoutevidence.truncated" : "chat-closeoutevidence.unavailable"));
  } else if (item.exitCode === undefined && !item.background) facts.push(t("chat-closeoutevidence.no_exit"));
  return (
    <li data-closeout-command-evidence={item.state} className="flex min-w-0 flex-col gap-0.5">
      <span className="flex min-w-0 items-baseline gap-1.5">
        <span className="shrink-0 text-ink-3">{t("chat-closeoutevidence.claimed")}</span>
        <code className="min-w-0 font-mono text-label whitespace-pre-wrap text-ink-2 [overflow-wrap:anywhere]">{item.claim}</code>
      </span>
      {item.command && (
        <span className="flex min-w-0 items-baseline gap-1.5">
          <span className="shrink-0 text-ink-3">{t("chat-closeoutevidence.recorded")}</span>
          <code className="min-w-0 font-mono text-label whitespace-pre-wrap text-ink [overflow-wrap:anywhere]">{item.command}</code>
        </span>
      )}
      <span className={item.isError ? "text-orange" : "text-ink-3"}>{facts.join(" · ")}</span>
    </li>
  );
}
