// The web pages a finished reply used (#9132; docs/web/web.md "Sources"): a quiet line under the reply, "Sources" and a
// chip per page, three of them, then +N for the rest. A chip names the page's site, as ChatGPT and Perplexity do (where
// it came from is what a reader checks first, and a site fits where a title is cut); its title and address are its
// tooltip and spoken name. Two pages of one site are told apart by their titles. What it
// reads is lib/turn-sources.ts. It is a chunk of its own, which SegmentView loads only under a finished exchange with a
// web call or folded work that names pages, so other turns and the first screen carry none of it. A chip is a link like any in a reply: a page the app opens itself (a Lark doc, another
// session) opens there, any other in a new tab. Shown in Persona conversations and Agent sessions, never while the turn
// runs (super rule 2: the calls stay in the folded work).
import { memo, useMemo, useState } from "react";
import { clickLink, linkHref } from "../../lib/links";
import { useLinkManager } from "../../lib/manager-choice";
import { turnSources } from "../../lib/turn-sources";
import type { Step } from "../../lib/turns";
import { Icon } from "../../ui/icons";
import { t } from "../../i18n";

/** Pages shown before +N (as the changed-files card shows three files). */
export const VISIBLE_SOURCES = 3;

/** The page's site, as people say it: the host in lower case, without "www.". The chip shows it and the same-site check
 *  compares it, so pages a reader sees as one site are always told apart by their titles. */
export function siteOf(url: string): string {
  let host: string;
  try {
    host = new URL(url).hostname;
  } catch {
    host = url;
  }
  return host.toLowerCase().replace(/^www\./, "");
}

const CHIP = "flex h-7 min-w-0 items-center gap-1.5 rounded-control bg-surface px-2 text-label text-ink-2 shadow-btn transition-colors hover:bg-hover hover:text-ink [@media(pointer:coarse)]:h-9";

export const TurnSources = memo(function TurnSources({ steps }: { steps: readonly Step[] }) {
  const sources = useMemo(() => turnSources(steps), [steps]);
  const manager = useLinkManager();
  const [expanded, setExpanded] = useState(false);
  if (!sources) return null;
  const visible = expanded ? sources : sources.slice(0, VISIBLE_SOURCES);
  const sites = sources.map((s) => siteOf(s.url));
  const hidden = sources.length - visible.length;
  return (
    <nav aria-label={t("chat-turnsources.label")} data-turn-sources={sources.length} className="flex min-w-0 flex-wrap items-center gap-1.5">
      {/* a heading of its own line on a narrow screen, where it would leave one chip beside it */}
      <span className="mr-0.5 basis-full text-label text-ink-3 sm:basis-auto">{t("chat-turnsources.label")}</span>
      {visible.map((s, i) => {
        const site = sites[i];
        const shared = sites.indexOf(site) !== sites.lastIndexOf(site);
        return (
          <a key={s.url} href={linkHref(s.url)} target="_blank" rel="noreferrer noopener" title={s.title ? `${s.title}\n${s.url}` : s.url}
            aria-label={s.title ? `${s.title}, ${site}` : site} onClick={(e) => clickLink(e, s.url, s.title || undefined, manager)}
            className={`${CHIP} max-w-[220px]`} data-source={s.url}>
            <Icon name="web" size={12} className="shrink-0 text-ink-3" />
            <span className="min-w-0 truncate">{shared && s.title ? s.title : site}</span>
          </a>
        );
      })}
      {hidden > 0 && (
        <button type="button" onClick={() => setExpanded(true)} aria-expanded={false} aria-label={t("chat-turnsources.show_more", { count: hidden })} className={`${CHIP} whitespace-nowrap tabular-nums`}>
          {t("chat-turnsources.more", { count: hidden })}
        </button>
      )}
      {expanded && sources.length > VISIBLE_SOURCES && (
        <button type="button" onClick={() => setExpanded(false)} aria-expanded className={`${CHIP} whitespace-nowrap`}>
          {t("chat-turnsources.show_less")}
        </button>
      )}
    </nav>
  );
});
