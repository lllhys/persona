// "iPhone control" on a computer's page while the hub's extension iphone is on (#8217; docs/extensions/iphone.md, the
// iPhone app's guided setup #8199): the switch that lets the assistant operate iPhones through this computer, and what
// each iPhone needs next, read from the hub's codes (lib/iphone.ts). What is missing comes with what to do here: Install
// the iPhone helper, Pair an iPhone on the cable, Prepare it (with what that does and doesn't do), Cancel while one of
// those runs, Forget in a paired iPhone's menu. Developer Mode and the iPhone's own consent are given on the iPhone, so
// for those the section says where. The hub is asked again when the page opens, every 2 s while a job runs and every
// 5 s while the next step is at the iPhone, only while the tab is shown.
import { useEffect, useRef, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { iphonePollEvery, iphoneView, newerIPhone, type IPhoneAction, type IPhoneRow, type IPhoneTone, type MachineIPhone } from "../../lib/iphone";
import { stateName } from "../../lib/state-labels";
import { documentPage, latestRequest, visiblePoll } from "../../lib/visible-poll";
import { Button, cx, EASE, IconButton, Menu, Spinner, toast, useMenu } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Switch } from "../settings/SettingsParts";
import { t } from "../../i18n";

export function IPhoneSection({ mid, iphone, online, mac }: { mid: string; iphone: MachineIPhone; online: boolean; mac: boolean }) {
  // What the hub said since the page opened; the machine list's copy wins once it is newer (it follows the node too).
  const [fresh, setFresh] = useState<MachineIPhone | null>(null);
  const [saving, setSaving] = useState(false);
  // The action whose request is on the way: its row shows it at once, before the hub answers (principle 7).
  const [asked, setAsked] = useState<{ action: IPhoneAction; device?: string } | null>(null);
  const [checking, setChecking] = useState(false);
  const menu = useMenu();
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const shown = newerIPhone(fresh, iphone);
  const enabled = shown.enabled;
  const every = iphonePollEvery(shown, online, mac);

  // One request line per computer, as ComputerSection: only the latest read or write is shown (#5455).
  const reads = useRef<ReturnType<typeof latestRequest<MachineIPhone>> | null>(null);
  useEffect(() => {
    setFresh(null);
    const line = latestRequest<MachineIPhone>(setFresh, () => {});
    reads.current = line;
    return () => {
      line.dispose();
      if (reads.current === line) reads.current = null;
    };
  }, [mid]);

  const refresh = () => reads.current?.run(() => api<MachineIPhone>(`/machines/${enc(mid)}/iphone`)) ?? Promise.resolve();

  const write = async (send: () => Promise<MachineIPhone>) => {
    const line = reads.current;
    try {
      const value = await send();
      void line?.run(() => Promise.resolve(value));
    } catch (e) {
      if (line && line === reads.current) throw e;
    }
  };

  useEffect(() => {
    if (online && enabled) void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mid, online, enabled]);

  useEffect(() => {
    if (!every) return;
    let started = false; // the page just asked (on open or with the action that started this)
    const stop = visiblePoll(() => { if (started) void refresh(); }, every, documentPage);
    started = true;
    return stop;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mid, every]);

  const set = async (on: boolean) => {
    setSaving(true);
    try {
      await write(() => api<MachineIPhone>(`/machines/${enc(mid)}/iphone`, { method: "PUT", json: { enabled: on } }));
    } catch (e) {
      toast(t(on ? "machines-iphonesection.turn_on_failed" : "machines-iphonesection.turn_off_failed", { error: errorText(e) }), "error");
    } finally {
      setSaving(false);
    }
  };

  const run = async (action: IPhoneAction, device?: string) => {
    setAsked({ action, device });
    try {
      await write(() => api<MachineIPhone>(`/machines/${enc(mid)}/iphone/${action}`, { method: "POST", json: device ? { device } : {} }));
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setAsked(null);
    }
  };

  const recheck = async () => {
    setChecking(true);
    try {
      await refresh();
    } finally {
      setChecking(false);
    }
  };

  const v = iphoneView(shown, online, mac, asked ?? undefined);
  const busy = asked !== null;
  const forgetting = shown.devices?.find((d) => d.id === menuFor);

  return (
    <section data-testid="iphone-section">
      <div className="mb-2.5 flex min-h-7 items-center gap-2 px-1">
        <h2 className="text-meta font-medium text-ink">{t("machines-iphonesection.title")}</h2>
      </div>
      <div className="overflow-hidden rounded-card bg-surface shadow-card" style={{ animation: `fade-up 420ms ${EASE} both` }}>
        <div className="flex items-center gap-3 px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="truncate text-meta text-ink">{t("machines-iphonesection.allow")}</div>
            <div className="mt-0.5 truncate text-submeta text-ink-3">{t("machines-iphonesection.allow_detail")}</div>
          </div>
          {saving && <Spinner />}
          <Switch on={enabled} onChange={(on) => void set(on)} disabled={saving || !online} label={t("machines-iphonesection.allow")} />
        </div>
        {v.kind !== "off" && (
          <div className="flex flex-col items-start gap-2.5 border-t border-line px-4 py-3">
            {v.kind === "offline" && <Line text={t("lib-iphone.offline")} tone="plain" />}
            {v.kind === "installing" && (
              <>
                <Line text={v.text} tone="work" />
                <Button busy={asked?.action === "cancel"} disabled={busy} onClick={() => void run("cancel")}>{t("common.cancel")}</Button>
              </>
            )}
            {v.kind === "notMac" && (
              <>
                <Line text={t("lib-iphone.not_mac")} tone="need" />
                <Detail text={t("lib-iphone.not_mac_detail")} />
              </>
            )}
            {v.kind === "install" && (
              <>
                <Line text={v.text} tone="need" />
                <Detail text={t("lib-iphone.install_detail")} />
                <Button tone="primary" testId="iphone-install" disabled={busy} onClick={() => void run("install")}>
                  {t(v.retry ? "machines-iphonesection.retry" : "machines-iphonesection.install")}
                </Button>
              </>
            )}
            {v.kind === "devices" && (
              <>
                {v.noXcode && (
                  <>
                    <Line text={t("lib-iphone.no_xcode")} tone="need" />
                    <Detail text={t("lib-iphone.no_xcode_detail")} />
                    <Button busy={checking} onClick={() => void recheck()}>{t("machines-iphonesection.check_again")}</Button>
                  </>
                )}
                {v.note && <Line text={v.note} tone="plain" />}
                {v.failed && <Line text={v.failed} tone="need" />}
                {v.rows.length === 0 ? (
                  <>
                    <Line text={t("lib-iphone.none")} tone="need" />
                    <Detail text={t("lib-iphone.none_detail")} />
                  </>
                ) : (
                  <ul className="w-full divide-y divide-line">
                    {v.rows.map((r) => (
                      <Row
                        key={r.device.id}
                        r={r}
                        busy={busy}
                        cancelling={asked?.action === "cancel"}
                        run={run}
                        menuOpen={menuFor === r.device.id && !!menu.anchor}
                        openMenu={(e) => {
                          setMenuFor(r.device.id);
                          menu.open(e);
                        }}
                      />
                    ))}
                  </ul>
                )}
                {v.consentHint && <Detail text={t("lib-iphone.consent_hint")} />}
              </>
            )}
          </div>
        )}
      </div>
      {menu.anchor && forgetting && (
        <Menu
          anchor={menu.anchor}
          onClose={menu.close}
          align="right"
          items={[{
            key: "forget",
            label: t("machines-iphonesection.forget"),
            icon: "trash",
            danger: true,
            confirm: t("machines-iphonesection.forget_confirm"),
            confirmDetail: t("machines-iphonesection.forget_detail"),
            onSelect: () => run("forget", forgetting.id),
          }]}
        />
      )}
    </section>
  );
}

function Row({ r, busy, cancelling, run, menuOpen, openMenu }: {
  r: IPhoneRow;
  busy: boolean;
  cancelling: boolean;
  run: (action: IPhoneAction, device?: string) => Promise<void>;
  menuOpen: boolean;
  openMenu: (e: React.MouseEvent<HTMLElement>) => void;
}) {
  const d = r.device;
  const name = d.name || t("lib-iphone.iphone");
  const [more, setMore] = useState(false);
  return (
    <li className="flex items-start gap-3 py-2.5" data-testid={`iphone-device-${d.id}`}>
      <Icon name="phone" size={16} className={cx("mt-0.5 shrink-0", r.tone === "ok" ? "text-green" : "text-ink-3")} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-meta text-ink">
          {name}
          {d.ios && <span className="text-submeta text-ink-3"> · {t("lib-iphone.ios", { version: d.ios })}</span>}
        </div>
        <p className="mt-0.5 flex items-start gap-1.5 text-submeta leading-relaxed text-ink-3" role="status">
          {r.tone === "work" && <Spinner size={10} />}
          <span className="min-w-0 break-words">{r.text}</span>
        </p>
        {r.prepare && (
          <>
            <p className="mt-1.5 text-submeta leading-relaxed text-ink-2">{t("lib-iphone.prepare_detail")}</p>
            <button type="button" className="mt-1 flex items-center gap-1 text-submeta text-ink-3 hover:text-ink" aria-expanded={more} onClick={() => setMore((m) => !m)}>
              <Icon name="chevronRight" size={10} className={cx("transition-transform", more && "rotate-90")} />
              {t("machines-iphonesection.details")}
            </button>
            {more && <p className="mt-1 text-submeta leading-relaxed text-ink-3">{t("lib-iphone.prepare_more")}</p>}
          </>
        )}
      </div>
      {r.action === "pair" && <Button tone="primary" testId={`iphone-pair-${d.id}`} disabled={busy} onClick={() => void run("pair", d.id)}>{t("machines-iphonesection.pair")}</Button>}
      {r.action === "prepare" && <Button tone="primary" testId={`iphone-prepare-${d.id}`} disabled={busy} onClick={() => void run("prepare", d.id)}>{t("machines-iphonesection.prepare")}</Button>}
      {r.action === "cancel" && <Button testId={`iphone-cancel-${d.id}`} busy={cancelling} disabled={busy && !cancelling} onClick={() => void run("cancel")}>{t("common.cancel")}</Button>}
      {!r.action && <Chip tone={r.tone} />}
      {r.forget && <IconButton icon="more" size={28} label={t("machines-iphonesection.more", { name })} active={menuOpen} disabled={busy} onClick={openMenu} />}
    </li>
  );
}

function Chip({ tone }: { tone: IPhoneTone }) {
  if (tone === "ok") return <span className="shrink-0 rounded-chip bg-green-tint px-1.5 text-submeta text-green">{t("lib-iphone.chip_ready")}</span>;
  if (tone === "need") return <span className="shrink-0 rounded-chip bg-orange-tint px-1.5 text-submeta text-orange">{stateName("needs_you")}</span>;
  return null;
}

function Detail({ text }: { text: string }) {
  return <p className="text-submeta leading-relaxed text-ink-3">{text}</p>;
}

function Line({ text, tone }: { text: string; tone: IPhoneTone }) {
  return (
    <p className="flex items-start gap-2 text-label" role="status">
      {tone === "work" ? (
        <span className="mt-[3px] shrink-0"><Spinner /></span>
      ) : (
        <span className={cx("mt-[3px] flex size-3.5 shrink-0 items-center justify-center rounded-full", tone === "ok" ? "bg-green-tint text-green" : "bg-orange-tint text-orange")}>
          <Icon name={tone === "ok" ? "check" : "info"} size={9} stroke={2.8} />
        </span>
      )}
      <span className={cx("min-w-0 break-words", tone === "ok" ? "text-ink" : "text-ink-2")}>{text}</span>
    </p>
  );
}
