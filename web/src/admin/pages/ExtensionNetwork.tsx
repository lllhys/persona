// "Network and isolation" in an installed extension's details (#5696): whether it runs in a sandbox, which the owner
// switches here (turning it off is confirmed; turning it on back to the default asks nothing), and what went through the
// egress proxy. The web's settings page links here (`?extension=<id>#network`) when an address was blocked. The owner
// allows a blocked host, or one typed in, with no question (a toast offers Undo) and revokes one (#5697).
import { t } from "../../i18n";
import { useEffect, useRef, useState } from "react";
import { Button, cx, Dialog, toast } from "../../ui/atoms";
import { Switch } from "../../views/settings/SettingsParts";
import { ago } from "../../lib/format";
import { allowExtensionHost, errorText, revokeExtensionHost, setExtensionSandbox } from "../api";
import { scrollBehavior } from "../../ui/stream/motion";
import { AdminIcon } from "../icons";
import type { AdminExtension } from "../types";

/** The extension the page was opened for (`/admin/extensions?extension=<id>`), "" none. */
export function linkedExtension(): string {
  return new URLSearchParams(location.search).get("extension") ?? "";
}

export function NetworkBlock({ p, onChanged, linked }: { p: AdminExtension; onChanged: (p: AdminExtension) => void; linked?: boolean }) {
  const s = p.sandbox;
  const box = useRef<HTMLDivElement>(null);
  const [lit, setLit] = useState(false);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!linked || !box.current) return;
    // The markets above load after the list and push it down: keep the block in view while the page settles, until the
    // user scrolls themselves.
    const show = () => box.current?.scrollIntoView({ block: "center", behavior: scrollBehavior() });
    let moved = false;
    const mine = () => (moved = true);
    addEventListener("wheel", mine, { once: true, passive: true });
    addEventListener("touchmove", mine, { once: true, passive: true });
    const settle = [0, 400, 1000, 1800].map((ms) => setTimeout(() => !moved && show(), ms));
    setLit(true);
    const off = setTimeout(() => setLit(false), 2400);
    return () => {
      settle.forEach(clearTimeout);
      clearTimeout(off);
      removeEventListener("wheel", mine);
      removeEventListener("touchmove", mine);
    };
  }, [linked]);
  if (!s || p.source !== "installed") return null;
  // Back to the default when the new value is the default: the switch has no separate "default" position.
  const set = async (on: boolean) => {
    setBusy(true);
    try {
      onChanged(await setExtensionSandbox(p.id, on === (s.default ?? true) ? null : on));
      toast(t(on ? "admin-pages-extensions.sandbox_turned_on" : "admin-pages-extensions.sandbox_turned_off", { name: p.name }), "ok");
      setAsking(false);
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy(false);
    }
  };
  const note = s.enabled && !s.active ? (s.why ?? "") : t(
    s.chosen
      ? s.enabled ? "admin-pages-extensions.sandbox_you_on" : "admin-pages-extensions.sandbox_you_off"
      : s.enabled ? "admin-pages-extensions.sandbox_default_on" : "admin-pages-extensions.sandbox_default_off",
  );
  const e = s.egress;
  const blocked = e?.denied ?? 0;
  return (
    <div id="network" ref={box} data-extension-network={p.id}>
      <p className="mb-1.5 text-[12px] font-medium text-ink-2">{t("admin-pages-extensions.network_title")}</p>
      {/* the highlight is an inset ring: the details' collapse clips anything drawn outside the block */}
      <div className={cx("divide-y divide-line overflow-hidden rounded-control transition-shadow duration-[400ms]", lit ? "shadow-[inset_0_0_0_2px_var(--accent)]" : "shadow-hairline")}>
        <div className="flex items-center gap-3 px-3 py-2.5">
          <AdminIcon name="shield" size={14} className={cx("shrink-0", !s.enabled ? "text-ink-3" : s.active ? "text-green" : "text-orange")} />
          <div className="min-w-0 flex-1">
            <div className="text-[13px] text-ink">{t("admin-pages-extensions.sandbox_label")}</div>
            {note && <div className="mt-0.5 text-[11.5px] leading-snug break-words text-ink-3">{note}</div>}
            {s.enabled && !s.active && <div className="mt-0.5 text-[11.5px] leading-snug text-ink-3">{t("admin-pages-extensions.sandbox_proxy_still")}</div>}
          </div>
          <Switch on={s.enabled} disabled={busy} onChange={(on) => (on ? void set(true) : setAsking(true))} label={t("admin-pages-extensions.sandbox_switch_label", { name: p.name })} />
        </div>
        {!s.enabled && (
          <div className="px-3 py-2.5">
            <p className="flex items-start gap-2 rounded-control bg-orange-tint px-3 py-2 text-[12.5px] leading-relaxed text-ink">
              <AdminIcon name="alert" size={13} className="mt-0.5 shrink-0 text-orange" />
              {t("admin-pages-extensions.sandbox_off_note", { name: p.name })}
            </p>
          </div>
        )}
        {s.enabled && e && (
          <div className="flex items-center gap-3 px-3 py-2.5">
            <span className="min-w-0 flex-1 text-[13px] text-ink">{t("admin-pages-extensions.network_egress")}</span>
            <span className="shrink-0 text-[12.5px] whitespace-nowrap tabular-nums">
              <span className="text-green">{t("admin-pages-extensions.network_allowed", { count: e.allowed ?? 0 })}</span>
              <span className="text-ink-3"> · </span>
              <span className={blocked ? "text-red" : "text-ink-3"}>{t("admin-pages-extensions.network_blocked", { count: blocked })}</span>
            </span>
          </div>
        )}
      </div>
      {s.enabled && <OwnerHosts p={p} onChanged={onChanged} />}
      <Dialog
        open={asking}
        onClose={() => !busy && setAsking(false)}
        title={t("admin-pages-extensions.sandbox_off_title", { name: p.name })}
        footer={<>
          <Button tone="quiet" onClick={() => setAsking(false)} disabled={busy}>{t("admin-parts.cancel")}</Button>
          <Button tone="danger" busy={busy} onClick={() => void set(false)}>{t("admin-pages-extensions.sandbox_off_confirm")}</Button>
        </>}
      >
        <p className="text-[12.5px] leading-relaxed text-ink-2">{t("admin-pages-extensions.sandbox_off_body")} {t("admin-pages-extensions.sandbox_off_one_host")}</p>
      </Dialog>
    </div>
  );
}

/** How a host the owner allowed reads: host, host:port, [v6]:port. */
export function hostText(h: { host: string; port?: string }): string {
  const host = h.host.includes(":") ? `[${h.host}]` : h.host;
  return h.port ? `${host}:${h.port}` : host;
}

/** The blocked hosts (with Allow) and the allowed ones (with Revoke), and a field to allow one by hand (#5697). */
function OwnerHosts({ p, onChanged }: { p: AdminExtension; onChanged: (p: AdminExtension) => void }) {
  const s = p.sandbox!;
  const allowedHosts = s.owner_allowed ?? [];
  const blocked = (s.egress?.recent_denied ?? []).filter((d) => !allowedHosts.some((a) => a.host === d.target));
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const allow = async (host: string, typed: boolean) => {
    setBusy(host);
    setError("");
    try {
      onChanged(await allowExtensionHost(p.id, host));
      if (typed) setDraft("");
      toast(t("admin-pages-extensions.host_allowed", { host }), "ok", { label: t("admin-pages-extensions.host_undo"), run: () => void revoke(host, true) });
    } catch (e) {
      if (typed) setError(errorText(e));
      else toast(errorText(e), "error");
    } finally {
      setBusy("");
    }
  };
  const revoke = async (host: string, undo = false) => {
    setBusy(host);
    try {
      onChanged(await revokeExtensionHost(p.id, host));
      if (!undo) toast(t("admin-pages-extensions.host_revoked", { host }), "ok");
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy("");
    }
  };
  return (
    <>
      {blocked.length > 0 && (
        <>
          <p className="mt-3 mb-1.5 text-[12px] font-medium text-ink-2">{t("admin-pages-extensions.hosts_blocked")}</p>
          <div className="divide-y divide-line overflow-hidden rounded-control shadow-hairline" data-hosts-blocked>
            {blocked.map((d) => (
              <div key={d.target} className="flex items-center gap-3 px-3 py-2">
                <div className="min-w-0 flex-1">
                  <div className="truncate font-mono text-[12px] text-ink select-text">{d.target}</div>
                  <div className="text-[11.5px] text-ink-3 tabular-nums">{t("admin-pages-extensions.host_blocked_times", { count: d.count, when: ago(d.at) })}</div>
                </div>
                <Button tone="secondary" className="h-7 shrink-0 px-2.5" busy={busy === d.target} disabled={!!busy} onClick={() => void allow(d.target, false)}>
                  {t("admin-pages-extensions.host_allow")}
                </Button>
              </div>
            ))}
          </div>
        </>
      )}
      <p className="mt-3 mb-1.5 text-[12px] font-medium text-ink-2">{t("admin-pages-extensions.hosts_allowed")}</p>
      <div className="divide-y divide-line overflow-hidden rounded-control shadow-hairline" data-hosts-allowed>
        {allowedHosts.map((a) => {
          const host = hostText(a);
          return (
            <div key={host} className="flex items-center gap-3 px-3 py-2">
              <div className="min-w-0 flex-1">
                <div className="truncate font-mono text-[12px] text-ink select-text">{host}</div>
                <div className="text-[11.5px] text-ink-3">{t(a.port ? "admin-pages-extensions.host_this_port" : "admin-pages-extensions.host_any_port")}{a.at ? ` · ${ago(a.at)}` : ""}</div>
              </div>
              <Button tone="quiet" className="h-7 shrink-0 px-2" busy={busy === host} disabled={!!busy} onClick={() => void revoke(host)}>
                {t("admin-pages-extensions.host_revoke")}
              </Button>
            </div>
          );
        })}
        <form
          className="px-3 py-2"
          onSubmit={(ev) => {
            ev.preventDefault();
            if (draft.trim()) void allow(draft.trim(), true);
          }}
        >
          <div className="flex items-center gap-2">
            <input
              value={draft}
              onChange={(ev) => (setDraft(ev.target.value), setError(""))}
              placeholder={t("admin-pages-extensions.host_placeholder")}
              aria-label={t("admin-pages-extensions.host_field_label", { name: p.name })}
              aria-invalid={error ? true : undefined}
              spellCheck={false}
              autoCapitalize="off"
              className={cx(
                "h-8 w-full min-w-0 rounded-control bg-inset px-2.5 font-mono text-[12.5px] text-ink shadow-hairline outline-none transition-shadow placeholder:font-sans placeholder:text-ink-3 focus:shadow-[0_0_0_1px_var(--line-strong),0_0_0_3px_var(--accent-tint)]",
                error && "shadow-[0_0_0_1px_var(--red)]",
              )}
            />
            <Button tone="secondary" type="submit" className="h-8 shrink-0 px-3" busy={busy === draft.trim() && !!draft.trim()} disabled={!draft.trim() || !!busy}>
              {t("admin-pages-extensions.host_allow")}
            </Button>
          </div>
          {error && <p className="mt-1 text-[11.5px] leading-relaxed text-red">{error}</p>}
        </form>
      </div>
    </>
  );
}
