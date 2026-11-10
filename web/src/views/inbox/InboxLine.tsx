// "Trail" (#2128, docs/assistant/inbox.md "Relationships: parents and children"): under an item's summary, what it grew out of (its parent's chain, up to a todo or
// an agent session) and what grew out of it (the items right under it). Each row opens where it lives. Nothing is
// drawn when the item has neither.
import { useEffect, useState } from "react";
import { fetchInboxTree, itemRoute, parentRoute, type InboxItem, type InboxParent, type InboxTree } from "../../lib/inbox";
import { openFromInbox } from "../../lib/inbox-nav";
import type { Route } from "../../lib/router";
import { t } from "../../i18n";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

export type LineRow = { key: string; label: string; title: string; route?: Route; up: boolean };

const kindLabel: Record<InboxParent["kind"], string> = { inbox: t("inbox-inboxline.kind_inbox"), todo: t("inbox-inboxline.kind_todo"), session: t("inbox-inboxline.kind_session") };

/** The rows of a line: the ancestors farthest first (the chain reads top down), then the children oldest first.
 *  `manager`: whose line it is (its items are that manager's: docs/assistant/managers.md "Clients"). */
export function lineRows(tree: InboxTree, manager?: string): LineRow[] {
  const ups = [...tree.ancestors].reverse().map((a): LineRow => ({
    key: `up:${a.kind}:${a.id}`,
    label: kindLabel[a.kind],
    title: a.item?.title || (a.kind === "inbox" ? t("inbox-inboxline.gone") : a.id),
    route: a.kind === "inbox" && !a.item ? undefined : parentRoute(a, manager),
    up: true,
  }));
  const downs = tree.children.map((c): LineRow => ({ key: `down:${c.id}`, label: t("inbox-inboxline.next"), title: c.title, route: itemRoute({ id: c.id, manager }), up: false }));
  return [...ups, ...downs];
}

export function LineList({ rows }: { rows: LineRow[] }) {
  if (!rows.length) return null;
  return (
    <section className="mt-3" data-inbox-line>
      <p className="mb-1 text-[11.5px] font-medium text-ink-3">{t("inbox-inboxline.title")}</p>
      <ul className="flex flex-col gap-0.5">
        {rows.map((r) => (
          <li key={r.key}>
            <button
              type="button"
              disabled={!r.route}
              onClick={() => r.route && openFromInbox(r.route)}
              className={cx(
                "flex w-full min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-left text-[12.5px] whitespace-nowrap",
                r.route ? "text-ink hover:bg-field" : "cursor-default text-ink-3",
              )}
            >
              <Icon name={r.up ? "up" : "chevron"} size={12} />
              <span className="shrink-0 text-ink-3">{r.label}</span>
              <span className="truncate">{r.title}</span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

export function InboxLine({ item }: { item: InboxItem }) {
  const [rows, setRows] = useState<LineRow[]>([]);
  const parentKey = item.parent ? `${item.parent.kind}:${item.parent.id}` : "";
  useEffect(() => {
    let live = true;
    setRows([]);
    // the item's manager's line (its tree is in that manager's inbox)
    fetchInboxTree("inbox", item.id, item.manager)
      .then((tree) => live && setRows(lineRows(tree, item.manager)))
      .catch(() => {}); // an older hub without the route: no line
    return () => {
      live = false;
    };
  }, [item.id, item.manager, parentKey]);
  return <LineList rows={rows} />;
}
