import { useRef, useState } from "react";
import { larkInboxCard } from "../../lib/lark-inbox-card";
import { openLink } from "../../lib/links";
import { t } from "../../i18n";
import { Button } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Markdown } from "../../ui/Markdown";

function Description({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const ref = useRef<HTMLButtonElement>(null);
  const long = text.length > 240 || text.split("\n").length > 5;
  const collapse = () => {
    setExpanded(false);
    ref.current?.focus({ preventScroll: true });
    ref.current?.scrollIntoView({ block: "nearest" });
  };
  return <div>
    {long && !expanded && <div className="line-clamp-3"><Markdown text={text} small askImages /></div>}
    {long && <button type="button" ref={ref} className="touch-hit mt-1 inline-flex min-h-8 items-center whitespace-nowrap rounded-control text-xs text-accent-ink focus-visible:outline-2 focus-visible:outline-accent" onClick={() => expanded ? collapse() : setExpanded(true)} aria-expanded={expanded}>
      {t(expanded ? "inbox-connectorthread.collapse_description" : "inbox-connectorthread.expand_description")}
    </button>}
    {(!long || expanded) && <Markdown text={text} small askImages />}
    {long && expanded && <Button tone="quiet" className="mt-2" onClick={collapse}>{t("inbox-connectorthread.collapse_description")}</Button>}
  </div>;
}

export function LarkInboxContent({ text, markdown }: { text: string; markdown?: boolean }) {
  const card = larkInboxCard(text);
  if (!card) {
    if (text.includes("<card")) return <pre className="whitespace-pre-wrap break-words font-sans select-text">{text}</pre>;
    return markdown ? <Description text={text} /> : <p className="whitespace-pre-wrap select-text">{text}</p>;
  }
  return <article className="min-w-0 overflow-clip rounded-card bg-surface shadow-card">
    <div className="flex items-center gap-2 border-b border-line px-3 py-2 text-xs font-medium">
      <Icon name="branch" size={14} className={card.title === "Merge Request Merged" ? "text-green" : "text-ink-3"} />
      <span className="min-w-0 break-words">{card.title || t("inbox-connectorthread.message_card")}</span>
    </div>
    <div className="px-3 py-3">
      {card.request && <a href={card.request.url} onClick={(e) => { e.preventDefault(); openLink(card.request!.url, card.request!.title); }} className="mb-2 block break-words text-sm font-semibold text-accent-ink">{card.request.title}</a>}
      <Description text={card.description ?? card.body} />
    </div>
    <details className="border-t border-line text-xs text-ink-3">
      <summary className="touch-hit flex cursor-pointer list-none items-center gap-2 px-3 py-2 focus-visible:outline-2 focus-visible:outline-accent [&::-webkit-details-marker]:hidden">
        <Icon name="folder" size={13} />
        <span className="min-w-0 flex-1 truncate">{card.repository?.title || t("inbox-connectorthread.message_card")}</span>
        <span className="shrink-0">{t("inbox-connectorthread.message_details")}</span><Icon name="chevron" size={12} />
      </summary>
      <div className="border-t border-line bg-inset px-3 py-3">
        {card.author && <p className="mb-2 text-ink-2">{t("inbox-connectorthread.author", { name: card.author })}</p>}
        {card.repository && <a className="block break-words text-accent-ink" href={card.repository.url} onClick={(e) => { e.preventDefault(); openLink(card.repository!.url, card.repository!.title); }}>{card.repository.title}</a>}
        <details className="mt-2"><summary className="touch-hit cursor-pointer text-accent-ink">{t("inbox-connectorthread.original_message")}</summary><pre className="mt-2 whitespace-pre-wrap break-words text-ink-2 select-text">{text}</pre></details>
      </div>
    </details>
  </article>;
}
