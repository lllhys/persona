// "Search in chat" (the chat's ⋯ menu, Android ChatSearchScreen, parity N26): searches what is loaded of the chat (words,
// quoted contexts, thoughts, tool names, tool input and output, system events; the work the hub folded too, fetched while
// the panel is open), the hits by category with the match marked; picking one closes the panel and goes to it (ChatView).
// "Load earlier and keep searching" pages back through the history, up to 20 pages a go.
import { useEffect, useMemo, useRef, useState } from "react";
import type { ChatSearchHit, ChatSearchResults, SearchCategory } from "../../lib/chat-search";
import { groupHits, searchChat, snippetParts } from "../../lib/chat-search-match";
import type { Item } from "../../lib/types";
import { t } from "../../i18n";
import { Button, Dialog, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Empty } from "../assistant/parts";
import { composing } from "../../lib/send-history";

/** The wait after the last key before searching (Android's 200 ms). */
const SETTLE_MS = 200;

const CATEGORY: Record<SearchCategory, string> = {
  text: "chat-chatsearch.category_text",
  quoted: "chat-chatsearch.category_quoted",
  thinking: "chat-chatsearch.category_thinking",
  tool_name: "chat-chatsearch.category_tool_name",
  tool_input: "chat-chatsearch.category_tool_input",
  tool_output: "chat-chatsearch.category_tool_output",
  event: "chat-chatsearch.category_event",
};

const NO_RESULTS: ChatSearchResults = { hits: [], truncated: false };

/** How a run of "Load earlier" ended: null reached the start; a string says why it stopped. */
export type LoadOutcome = string | null;

export function ChatSearch({
  open,
  query,
  onQuery,
  items,
  foldsLoading = false,
  foldsPartial = false,
  rounds,
  hasMore,
  onLoadEarlier,
  onPick,
  onClose,
}: {
  open: boolean;
  query: string;
  onQuery: (q: string) => void;
  items: Item[];
  /** The folded work is being fetched for the search (useFoldSearch). */
  foldsLoading?: boolean;
  /** Some folded work is not searched (its fetch failed, or past the budget). */
  foldsPartial?: boolean;
  /** Exchanges loaded so far. */
  rounds: number;
  hasMore: boolean;
  /** Pages back (up to 20) until the start, an error or `stop()`; `onPage` counts the pages in. */
  onLoadEarlier: (onPage: (n: number) => void, stopped: () => boolean) => Promise<LoadOutcome>;
  onPick: (hit: ChatSearchHit) => void;
  onClose: () => void;
}) {
  const [applied, setApplied] = useState(query);
  useEffect(() => {
    const id = window.setTimeout(() => setApplied(query), SETTLE_MS);
    return () => clearTimeout(id);
  }, [query]);
  const searching = applied !== query;
  // only while the panel shows: the query stays after a pick, and the chat keeps changing under it
  const results = useMemo(() => (open ? searchChat(items, applied) : NO_RESULTS), [open, items, applied]);
  const groups = useMemo(() => groupHits(results.hits), [results.hits]);

  const [loading, setLoading] = useState(false);
  const [pages, setPages] = useState(0);
  const [notice, setNotice] = useState<string | null>(null);
  const stop = useRef(false);
  // the panel closing stops a run going
  useEffect(() => {
    if (!open) stop.current = true;
  }, [open]);
  const loadEarlier = async () => {
    stop.current = false;
    setLoading(true);
    setPages(0);
    setNotice(null);
    try {
      setNotice(await onLoadEarlier(setPages, () => stop.current));
    } catch {
      setNotice(t("chat-chatview.search_load_failed"));
    } finally {
      setLoading(false);
    }
  };

  const count = `${results.hits.length}${results.truncated ? "+" : ""}`;
  const footer = (
    <div className="flex w-full min-w-0 items-center gap-3">
      <span role="status" className="min-w-0 flex-1 truncate text-[12px] text-ink-3">
        {loading ? t("chat-chatsearch.loading_pages", { count: pages }) : notice ?? (foldsLoading ? t("chat-chatsearch.searching_folds") : foldsPartial ? t("chat-chatsearch.folds_partial") : hasMore ? "" : t("chat-chatsearch.at_start"))}
      </span>
      {loading ? (
        <Button tone="quiet" onClick={() => (stop.current = true)}>
          {t("chat-chatsearch.stop_loading")}
        </Button>
      ) : (
        hasMore && (
          <Button onClick={() => void loadEarlier()}>
            {t("chat-chatsearch.load_earlier")}
          </Button>
        )
      )}
    </div>
  );

  return (
    <Dialog open={open} onClose={onClose} title={t("chat-chatsearch.title")} width={640} footer={footer}>
      <div className="flex flex-col gap-2" data-chat-search>
        <label className="flex min-w-0 items-center gap-2 rounded-control bg-field px-3 py-2">
          <Icon name="search" size={14} className="shrink-0 text-ink-3" />
          <input
            autoFocus
            value={query}
            onChange={(e) => onQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key !== "Enter" || composing(e.nativeEvent)) return;
              const first = results.hits[0];
              if (first && !searching) {
                e.preventDefault();
                onPick(first);
              }
            }}
            placeholder={t("chat-chatsearch.placeholder")}
            aria-label={t("chat-chatsearch.placeholder")}
            className="min-w-0 flex-1 bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3"
          />
          {searching && <Spinner size={12} />}
        </label>
        <div className="flex flex-col gap-0.5 px-1 text-[12px] text-ink-3">
          <span>{t("chat-chatsearch.summary", { hits: count, rounds })}</span>
          {results.truncated && <span>{t("chat-chatsearch.truncated")}</span>}
        </div>
        <div className="-mx-1 max-h-[52vh] overflow-y-auto px-1 max-sm:max-h-none" role="list">
          {!searching && results.hits.length === 0 ? (
            <Empty icon="search" title={applied.trim() ? t("chat-chatsearch.no_hits") : t("chat-chatsearch.enter_keyword")}>
              {t("chat-chatsearch.scope")}
            </Empty>
          ) : (
            groups.map(([category, hits]) => (
              <section key={category} className="flex flex-col gap-1 pb-3">
                <h3 className="px-1 pt-1 text-[11.5px] font-semibold text-ink-3">{t(CATEGORY[category])}</h3>
                {hits.map((hit, i) => (
                  <button
                    key={`${hit.itemId}/${i}`}
                    type="button"
                    role="listitem"
                    onClick={() => onPick(hit)}
                    className="flex w-full min-w-0 items-start gap-2 rounded-control px-2 py-2 text-left transition-colors hover:bg-hover"
                    data-search-result={category}
                  >
                    <Icon name="search" size={13} className="mt-[3px] shrink-0 text-ink-3" />
                    <span className="line-clamp-3 min-w-0 text-[13px] leading-relaxed text-ink [overflow-wrap:anywhere]">
                      {snippetParts(hit).map(([text, match], k) => (match ? <mark key={k} className="rounded-[3px] bg-accent-tint text-ink">{text}</mark> : <span key={k}>{text}</span>))}
                    </span>
                  </button>
                ))}
              </section>
            ))
          )}
          <p className="px-1 py-2 text-[11.5px] leading-relaxed text-ink-3">{t("chat-chatsearch.footnote")}</p>
        </div>
      </div>
    </Dialog>
  );
}
