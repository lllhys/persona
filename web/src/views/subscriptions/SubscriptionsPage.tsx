// The source subscription pages (#2041, docs/extensions/subscriptions.md "Pages") at /subscriptions: the manager draws
// them as declarative UI, so this is only the page around them, as an extension's full page has (ExtensionNavPage).
// ?state= opens them on one extension's or one connector's subscriptions.
import { Suspense, useMemo, useState } from "react";
import { t } from "../../i18n";
import { SUBSCRIPTIONS_UI } from "../../lib/subscriptions-ui";
import { IconButton, Loading } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { ExtensionUI } from "../settings/ExtensionView";

export function SubscriptionsPage({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const [n, setN] = useState(0);
  const state = useMemo(() => new URLSearchParams(location.search).get("state") ?? undefined, []);
  const title = t("subscriptions.title");
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
        <div className="mx-auto flex max-w-[880px] flex-col px-4 py-6 sm:px-8">
          <Suspense fallback={<Loading />}>
            <ExtensionUI key={n} extension={SUBSCRIPTIONS_UI} ui="pages" title={title} bare state={state} />
          </Suspense>
        </div>
      </div>
    </div>
  );
}
