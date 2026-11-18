// "Browser sign-ins" on a Mac's page (#1304, docs/plans/plan-ego-browser.md "Importing cookies"; iOS BrowserLoginsSection): the sites
// whose sign-ins ego lite keeps (how many cookies, until when, never what), clearing some of them, and importing a
// cookie file (cookies.txt, or an extension's JSON). The file is read here, its sites ticked by the user (none at first),
// and only the ticked ones go to the hub, which passes them to the Mac and keeps nothing. The Mac must be online.
// A few sites are listed in place; more fold behind one summary row that opens the whole list here, with a search, an
// order and checkboxes (#5821, docs/design/DESIGN_SYSTEM.md ("Long lists on a detail page")).
import { useEffect, useMemo, useRef, useState } from "react";
import { api, enc, errorText, pastHub } from "../../lib/api";
import { parseCookies, picked, siteExpired, siteList, sitesOf, siteSummary, SITES_IN_PLACE, type CookieSite, type CookieSiteNow, type SiteOrder } from "../../lib/cookies";
import { Button, Collapse, cx, Dialog, IconButton, Loading, Menu, toast, useMenu } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { SearchField } from "../../ui/SearchField";
import { formatDate, t } from "../../i18n";

interface Answer {
  sites: CookieSiteNow[];
  imported?: number;
  skipped?: number;
  removed?: number;
}

const day = (at: string | number) => formatDate(typeof at === "number" ? at * 1000 : at, { year: "numeric", month: "numeric", day: "numeric" });
const text = (key: string, vars?: Record<string, string | number>) => t(`machines-browserlogins.${key}`, vars);

/** The search settles this long after the last key before the list is filtered again. */
const SEARCH_SETTLE_MS = 150;

function useSettled<T>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return settled;
}

export function BrowserLogins({ mid, online }: { mid: string; online: boolean }) {
  const [sites, setSites] = useState<CookieSiteNow[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [file, setFile] = useState<{ name: string; sites: CookieSite[]; bad: number } | null>(null);
  const [ticked, setTicked] = useState<Set<string>>(new Set());
  const [clearing, setClearing] = useState<string[] | null>(null); // the sites the confirmation asks about
  const [tries, setTries] = useState(0); // "Try again" reads the sites again
  const [open, setOpen] = useState(false); // the whole list, when it is folded
  const [query, setQuery] = useState("");
  const settled = useSettled(query, SEARCH_SETTLE_MS);
  const [order, setOrder] = useState<SiteOrder>("name");
  const [chosen, setChosen] = useState<Set<string>>(new Set());
  const [menuSite, setMenuSite] = useState("");
  const rowMenu = useMenu();
  const orderMenu = useMenu();
  const input = useRef<HTMLInputElement>(null);
  const orderAt = useRef<HTMLSpanElement>(null);
  const path = `/machines/${enc(mid)}/browser/cookies`;

  useEffect(() => {
    setSites(null);
    setError("");
    if (!online) return;
    let live = true;
    // the hub gives the Mac's browser 2 min 15 s (extmgr/browser_cookies.go)
    api<Answer>(path, { timeout: pastHub(135_000) })
      .then((a) => live && setSites(a.sites))
      .catch((e) => live && setError(errorText(e)));
    return () => {
      live = false;
    };
  }, [path, online, tries]);

  const folded = !!sites && sites.length > SITES_IN_PLACE;
  const shown = useMemo(() => (sites ? (folded ? siteList(sites, settled, order) : siteList(sites, "", "name")) : []), [sites, folded, settled, order]);
  const summary = useMemo(() => siteSummary(sites ?? []), [sites]);
  const chosenShown = shown.filter((s) => chosen.has(s.domain));

  const pick = async (f: File | undefined) => {
    if (!f) return;
    if (input.current) input.current.value = ""; // the same file can be picked again
    try {
      const parsed = parseCookies(await f.text());
      setFile({ name: f.name, sites: sitesOf(parsed.cookies), bad: parsed.bad });
      setTicked(new Set());
    } catch (e) {
      toast(errorText(e), "error");
    }
  };

  const importTicked = async () => {
    if (!file) return;
    const cookies = picked(file.sites, ticked);
    setBusy(true);
    try {
      const a = await api<Answer>(path, { method: "POST", json: { cookies } });
      setSites(a.sites);
      setFile(null); // the file's contents are gone from the page once sent
      toast(a.skipped ? text("imported_skipped", { count: a.imported ?? 0, skipped: a.skipped }) : text("imported", { count: a.imported ?? 0 }), "ok");
    } catch (e) {
      toast(text("import_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
    }
  };

  const clear = async (list: string[]) => {
    setBusy(true);
    try {
      const a = await api<Answer>(`${path}?${list.map((s) => `site=${enc(s)}`).join("&")}`, { method: "DELETE" });
      setSites(a.sites);
      setChosen((c) => new Set([...c].filter((d) => !list.includes(d) && a.sites.some((s) => s.domain === d))));
      toast(text("cleared", { count: a.removed ?? 0 }), "ok");
    } catch (e) {
      toast(text("clear_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
      setClearing(null);
    }
  };

  const choose = (domain: string) =>
    setChosen((c) => {
      const n = new Set(c);
      if (n.has(domain)) n.delete(domain);
      else n.add(domain);
      return n;
    });

  const row = (s: CookieSiteNow, checkable: boolean) => {
    const gone = siteExpired(s);
    const detail = [text("count", { count: s.count }), s.expires ? text("expires", { date: day(s.expires) }) : text("session_only")].join(" · ");
    const words = (
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate text-[13px] font-medium text-ink" data-browser-logins-site="">
            {s.domain}
          </span>
          {gone && <span className="inline-flex h-5 shrink-0 items-center whitespace-nowrap rounded-full bg-orange-tint px-2 text-[11px] font-medium text-orange">{text("expired")}</span>}
        </span>
        <span className="block truncate text-[11.5px] text-ink-3 tabular-nums">{detail}</span>
      </span>
    );
    return (
      <li key={s.domain} className="flex min-h-12 items-center gap-1 border-b border-line pl-4 pr-2 last:border-b-0">
        {checkable ? (
          <label className="flex min-h-12 min-w-0 flex-1 cursor-pointer items-center gap-3 py-2">
            <input type="checkbox" checked={chosen.has(s.domain)} onChange={() => choose(s.domain)} disabled={busy} className="size-4 shrink-0 accent-[var(--accent)]" />
            {words}
          </label>
        ) : (
          <span className="flex min-h-12 min-w-0 flex-1 items-center gap-3 py-2">
            <Icon name="lock" size={14} className="shrink-0 text-ink-3" />
            {words}
          </span>
        )}
        <IconButton
          icon="more"
          label={text("more", { site: s.domain })}
          size={32}
          disabled={busy}
          active={menuSite === s.domain && !!rowMenu.anchor}
          onClick={(e) => {
            setMenuSite(s.domain);
            rowMenu.open(e);
          }}
        />
      </li>
    );
  };

  const count = file ? picked(file.sites, ticked).length : 0;
  const allChosen = shown.length > 0 && chosenShown.length === shown.length;
  return (
    <section data-browser-logins="">
      <div className="mb-2.5 flex min-h-7 items-center gap-2 px-1">
        <h2 className="text-[13px] font-medium text-ink">{text("title")}</h2>
        {sites && <span className="text-[12px] text-ink-3 tabular-nums">{sites.length}</span>}
        <span className="flex-1" />
        <Button icon="arrowDown" onClick={() => input.current?.click()} disabled={busy || !online}>
          {text("import_file")}
        </Button>
        <input ref={input} type="file" accept=".txt,.json,text/plain,application/json" className="hidden" onChange={(e) => void pick(e.target.files?.[0])} />
      </div>
      <div className="overflow-hidden rounded-card bg-surface shadow-card">
        {!online ? (
          <p className="px-4 py-5 text-[12.5px] text-ink-3">{text("offline")}</p>
        ) : error ? (
          <div className="flex items-center gap-3 px-4 py-4">
            <p className="min-w-0 flex-1 text-[12.5px] text-red">{error}</p>
            <Button onClick={() => setTries((n) => n + 1)}>{text("try_again")}</Button>
          </div>
        ) : !sites ? (
          <div className="flex h-20 items-center justify-center">
            <Loading label={text("loading")} />
          </div>
        ) : sites.length === 0 ? (
          <p className="px-4 py-5 text-[12.5px] text-ink-3">{text("empty")}</p>
        ) : !folded ? (
          <ul>{shown.map((s) => row(s, false))}</ul>
        ) : (
          <>
            <button
              type="button"
              data-browser-logins-summary=""
              aria-expanded={open}
              aria-controls={`browser-logins-${mid}`}
              onClick={() => setOpen((v) => !v)}
              className="flex min-h-12 w-full items-center gap-3 px-4 py-2 text-left transition-colors hover:bg-hover"
            >
              <Icon name="lock" size={14} className="shrink-0 text-ink-3" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[13px] font-medium text-ink tabular-nums">{text("sites", { count: summary.count })}</span>
                {(summary.first !== undefined || summary.expired > 0) && (
                  <span className="block truncate text-[11.5px] text-ink-3 tabular-nums">
                    {summary.first !== undefined && text("first_expires", { date: day(summary.first / 1000) })}
                    {summary.first !== undefined && summary.expired > 0 && " · "}
                    {summary.expired > 0 && <span className="text-orange">{text("expired_count", { count: summary.expired })}</span>}
                  </span>
                )}
              </span>
              <Icon name={open ? "chevron" : "chevronRight"} size={14} className="shrink-0 text-ink-3" />
            </button>
            <Collapse open={open} lazy>
              <div id={`browser-logins-${mid}`} className="border-t border-line">
                <div className="flex flex-wrap items-center gap-2 px-4 py-2.5">
                  <SearchField value={query} onChange={setQuery} placeholder={text("search")} tone="inset" className="min-w-[160px] flex-1" />
                  <span ref={orderAt} className="inline-flex">
                    <Button tone="quiet" icon="sliders" title={text("order")} onClick={() => (orderMenu.anchor ? orderMenu.close() : orderMenu.openAt(orderAt.current))}>
                      {text(order === "name" ? "order_name" : "order_expiry")}
                    </Button>
                  </span>
                </div>
                <div className="flex min-h-10 items-center gap-3 border-t border-line px-4 py-1.5 text-[12px]">
                  <label className={cx("flex items-center gap-3", shown.length ? "cursor-pointer" : "opacity-50")}>
                    <input
                      type="checkbox"
                      checked={allChosen}
                      ref={(el) => {
                        if (el) el.indeterminate = chosenShown.length > 0 && !allChosen;
                      }}
                      disabled={busy || !shown.length}
                      onChange={() => setChosen(allChosen ? new Set() : new Set(shown.map((s) => s.domain)))}
                      className="size-4 accent-[var(--accent)]"
                    />
                    <span className="text-ink-2">{chosenShown.length ? text("chosen", { count: chosenShown.length }) : text("select_all")}</span>
                  </label>
                  <span className="flex-1" />
                  {chosenShown.length > 0 && (
                    <Button tone="danger" size="sm" testId="browser-logins-clear-selected" onClick={() => setClearing(chosenShown.map((s) => s.domain))} disabled={busy}>
                      {text("clear_count", { count: chosenShown.length })}
                    </Button>
                  )}
                </div>
                {shown.length ? (
                  <ul className="max-h-[60vh] overflow-y-auto border-t border-line">{shown.map((s) => row(s, true))}</ul>
                ) : (
                  <p className="border-t border-line px-4 py-5 text-[12.5px] text-ink-3">{text("no_match", { query: settled.trim() })}</p>
                )}
              </div>
            </Collapse>
          </>
        )}
      </div>
      <p className="mt-2 px-1 text-[11.5px] leading-relaxed text-ink-3">{text("privacy")}</p>

      {rowMenu.anchor && (
        <Menu
          anchor={rowMenu.anchor}
          onClose={rowMenu.close}
          align="right"
          items={[{ key: "clear", label: text("clear_site"), icon: "trash", danger: true, onSelect: () => setClearing([menuSite]) }]}
        />
      )}
      {orderMenu.anchor && (
        <Menu
          anchor={orderMenu.anchor}
          onClose={orderMenu.close}
          align="right"
          items={(["name", "expiry"] as const).map((o) => ({ key: o, label: text(o === "name" ? "order_name" : "order_expiry"), radio: true, checked: order === o, onSelect: () => setOrder(o) }))}
        />
      )}

      <Dialog
        open={!!file}
        onClose={() => !busy && setFile(null)}
        title={text("pick_title")}
        width={480}
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button tone="quiet" onClick={() => setFile(null)} disabled={busy}>
              {t("common.cancel")}
            </Button>
            <Button tone="primary" onClick={() => void importTicked()} disabled={busy || count === 0} busy={busy}>
              {count ? text("import_count", { count }) : text("import")}
            </Button>
          </div>
        }
      >
        {file && (
          <div className="flex flex-col gap-3">
            <p className="text-[12.5px] text-ink-3">
              {file.bad ? text("file_summary_bad", { name: file.name, count: file.sites.length, bad: file.bad }) : text("file_summary", { name: file.name, count: file.sites.length })}
            </p>
            <div className="flex items-center gap-3 text-[12px]">
              <button type="button" className="font-medium text-accent-ink" onClick={() => setTicked(new Set(file.sites.filter((s) => s.cookies.length).map((s) => s.site)))}>
                {text("select_all")}
              </button>
              <button type="button" className="text-ink-3 hover:text-ink" onClick={() => setTicked(new Set())}>
                {text("select_none")}
              </button>
            </div>
            <ul className="max-h-[46vh] overflow-y-auto rounded-[10px] shadow-hairline">
              {file.sites.map((s) => {
                const none = s.cookies.length === 0;
                const on = ticked.has(s.site);
                return (
                  <li key={s.site} className="border-b border-line last:border-b-0">
                    <label className={cx("flex min-h-11 items-center gap-3 px-3 py-2", none ? "opacity-50" : "cursor-pointer hover:bg-hover")}>
                      <input
                        type="checkbox"
                        checked={on}
                        disabled={none}
                        onChange={() => setTicked((t) => {
                          const n = new Set(t);
                          if (n.has(s.site)) n.delete(s.site);
                          else n.add(s.site);
                          return n;
                        })}
                        className="size-4 accent-[var(--accent)]"
                      />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[13px] text-ink">{s.site}</span>
                        <span className="block text-[11.5px] text-ink-3 tabular-nums">
                          {[text("count", { count: s.cookies.length }), s.expires && text("expires", { date: day(s.expires) }), s.expired && text("expired_skip", { count: s.expired })].filter(Boolean).join(" · ")}
                        </span>
                      </span>
                    </label>
                  </li>
                );
              })}
            </ul>
            <p className="text-[11.5px] leading-relaxed text-ink-3">{text("import_note")}</p>
          </div>
        )}
      </Dialog>

      <Dialog
        open={clearing !== null}
        onClose={() => !busy && setClearing(null)}
        title={clearing?.length === 1 ? text("clear_site_title", { site: clearing[0] }) : text("clear_sites_title", { count: clearing?.length ?? 0 })}
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button tone="quiet" onClick={() => setClearing(null)} disabled={busy}>
              {t("common.cancel")}
            </Button>
            <Button tone="danger" onClick={() => clearing && void clear(clearing)} disabled={busy} busy={busy}>
              {text("clear")}
            </Button>
          </div>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">{clearing?.length === 1 ? text("clear_site_body") : text("clear_sites_body")}</p>
      </Dialog>
    </section>
  );
}
