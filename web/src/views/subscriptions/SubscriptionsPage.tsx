// The source subscription pages (#2041, docs/extensions/subscriptions.md "Pages") at /subscriptions: the manager draws
// them as declarative UI, so this is only the page around them, as an extension's full page has (ExtensionNavPage).
// ?state= opens them on one extension's or one connector's subscriptions, or one of their pages. A manager with
// inbox_sources draws Inbox Sources (#5278), every source of the Inbox, and the page says so.
import { Suspense, useMemo, useState } from "react";
import { t } from "../../i18n";
import { useInboxSourcesShown } from "../../lib/subscriptions";
import { cx, IconButton, Loading } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { SourcesUI } from "./SourcesUI";

export function SubscriptionsPage({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const [n, setN] = useState(0);
  const [graph, setGraph] = useState(false); // the relationship view takes a wider page (#5741)
  const state = useMemo(() => new URLSearchParams(location.search).get("state") ?? undefined, []);
  const sources = useInboxSourcesShown();
  // every page of them is Inbox Sources' with a manager that draws them (#5278), whichever one opens first
  const title = sources ? t("subscriptions.title_sources") : t("subscriptions.title");
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label={t("subscriptions.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
        <span className="flex size-7 shrink-0 items-center justify-center rounded-[8px] bg-field text-ink-2">
          <Icon name="inbox" size={14} />
        </span>
        <h1 className="min-w-0 truncate text-[14px] font-semibold text-ink">{title}</h1>
        <span className="flex-1" />
        <IconButton icon="retry" label={t("subscriptions.redraw")} onClick={() => setN((x) => x + 1)} />
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className={cx("mx-auto flex flex-col px-4 py-6 sm:px-8", graph ? "max-w-[1120px]" : "max-w-[880px]")}>
          <Suspense fallback={<Loading />}>
            <SourcesUI key={n} title={title} state={state} onGraph={setGraph} />
          </Suspense>
        </div>
      </div>
    </div>
  );
}
