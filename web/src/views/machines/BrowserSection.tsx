// "Browser (ego lite)" on a Mac's page while the hub's extension ego is on (docs/plans/plan-ego-browser.md, iOS BrowserSection;
// G107): the switch that lets the assistant drive its ego lite, and how far that is: "Install ego lite" when it is not there;
// its first-run setup as steps (open it on the Mac or over the page's View screen, import a browser's data, finish, check again); "Ready" with
// what is left (its usage statistics) ticked or not. Once its setup is done, its sign-ins (BrowserLogins) follow. The
// hub is asked again when the page opens, and every 2 s while a job runs or the setup is under way and the tab is shown.
// Only the latest answer for the Mac shown now lands: one for a Mac opened before, or an older one, is dropped.
import { useEffect, useRef, useState } from "react";
import { api, enc, errorText, pastHub } from "../../lib/api";
import { browserView, browserWatching, type BrowserAction, type BrowserStep, type MachineBrowser } from "../../lib/browser";
import { documentPage, latestRequest, visiblePoll } from "../../lib/visible-poll";
import { Button, cx, EASE, Spinner, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Switch } from "../settings/SettingsParts";
import { BrowserLogins } from "./BrowserLogins";
import { t } from "../../i18n";

type Reads = ReturnType<typeof latestRequest<MachineBrowser>>;

export function BrowserSection({ mid, browser, online }: { mid: string; browser: MachineBrowser; online: boolean }) {
  // What the hub said since the page opened (newer than the machine list's).
  const [fresh, setFresh] = useState<MachineBrowser | null>(null);
  const [saving, setSaving] = useState(false);
  const [asking, setAsking] = useState(false);
  const [checking, setChecking] = useState(false);
  const shown = fresh ?? browser;
  const enabled = shown.enabled;
  const watching = browserWatching(shown, online);

  // One request line per Mac: disposed when another Mac is shown or the page goes, so their answers never land.
  // Declared before the effects that read through it, so it exists when they first run.
  const reads = useRef<Reads | null>(null);
  useEffect(() => {
    setFresh(null);
    // a failed read leaves the machine list's copy (or the last answer) on screen
    const line = latestRequest<MachineBrowser>(setFresh, () => {});
    reads.current = line;
    return () => {
      line.dispose();
      if (reads.current === line) reads.current = null;
    };
  }, [mid]);

  // the hub asks the Mac for up to 45 s (extmgr/browser.go)
  const refresh = () => reads.current?.run(() => api<MachineBrowser>(`/machines/${enc(mid)}/browser`, { timeout: pastHub(45_000) })) ?? Promise.resolve();

  // Runs a write and shows its answer as the latest one (an older read still in flight is dropped). If another Mac is
  // shown by the time it comes back, both its answer and its failure belong to the Mac left behind and are dropped.
  const write = async (send: () => Promise<MachineBrowser>) => {
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
    if (!watching) return;
    // visiblePoll asks right away when it starts; the page just asked (on open or with the action that started this)
    let started = false;
    const stop = visiblePoll(() => { if (started) void refresh(); }, 2000, documentPage);
    started = true;
    return stop;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mid, watching]);

  const set = async (on: boolean) => {
    setSaving(true);
    try {
      await write(() => api<MachineBrowser>(`/machines/${enc(mid)}/browser`, { method: "PUT", json: { enabled: on } }));
    } catch (e) {
      toast(t(on ? "machines-browsersection.turn_on_failed" : "machines-browsersection.turn_off_failed", { error: errorText(e) }), "error");
    } finally {
      setSaving(false);
    }
  };

  const run = async (what: BrowserAction) => {
    setAsking(true);
    try {
      await write(() => api<MachineBrowser>(`/machines/${enc(mid)}/browser/${what}`, { method: "POST" }));
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setAsking(false);
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

  const v = browserView(shown, online);
  const action = (title: string, what: BrowserAction, prominent = false) => (
    <Button tone={prominent ? "primary" : "secondary"} busy={asking} onClick={() => void run(what)}>
      {title}
    </Button>
  );
  const recheckButton = (
    <Button tone="secondary" busy={checking} onClick={() => void recheck()}>
      {t("machines-browsersection.recheck")}
    </Button>
  );

  return (
    <>
      <section data-testid="browser-section">
        <div className="mb-2.5 flex min-h-7 items-center gap-2 px-1">
          <h2 className="text-[13px] font-medium text-ink">{t("machines-browsersection.title")}</h2>
        </div>
        <div className="overflow-hidden rounded-card bg-surface shadow-card" style={{ animation: `fade-up 420ms ${EASE} both` }}>
          <div className="flex items-center gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <div className="truncate text-[13px] text-ink">{t("machines-browsersection.allow")}</div>
              <div className="mt-0.5 truncate text-[11.5px] text-ink-3">{t("machines-browsersection.allow_detail")}</div>
            </div>
            {saving && <Spinner />}
            <Switch on={enabled} onChange={(on) => void set(on)} disabled={saving || !online} label={t("machines-browsersection.allow")} />
          </div>
          {v.kind !== "off" && (
            <div className="flex flex-col items-start gap-2.5 border-t border-line px-4 py-3">
              {v.kind === "offline" && <Line text={t("machines-browsersection.offline")} />}
              {v.kind === "installing" && (
                <>
                  <p className="flex items-center gap-2 text-[12.5px] text-ink-2" role="status">
                    <Spinner />
                    {v.text}
                  </p>
                  {action(t("common.cancel"), "cancel")}
                </>
              )}
              {v.kind === "install" && (
                <>
                  <Line text={v.text} />
                  <p className="text-[11.5px] leading-relaxed text-ink-3">{t("machines-browsersection.install_detail")}</p>
                  {action(v.retry ? t("machines-browsersection.reinstall") : t("machines-browsersection.install"), "install", true)}
                </>
              )}
              {v.kind === "setup" && (
                <>
                  {v.running ? (
                    <p className="flex items-center gap-2 text-[12.5px] text-ink-2" role="status">
                      <Spinner />
                      {v.text}
                    </p>
                  ) : (
                    <Line text={v.text} />
                  )}
                  <Steps steps={v.steps} />
                  <div className="flex flex-wrap gap-2">
                    {action(t("machines-browsersection.open"), "open", true)}
                    {recheckButton}
                  </div>
                </>
              )}
              {v.kind === "unready" && (
                <>
                  <Line text={v.text} />
                  {recheckButton}
                </>
              )}
              {v.kind === "ready" && (
                <>
                  <Line text={v.text} ok />
                  <Steps steps={v.steps} />
                  {v.recheck && recheckButton}
                </>
              )}
            </div>
          )}
        </div>
      </section>
      {shown.set_up && <BrowserLogins mid={mid} online={online} />}
    </>
  );
}

/** Numbered steps; a done one is ticked and quieter (iOS GrantGuide). */
function Steps({ steps }: { steps: BrowserStep[] }) {
  let n = 0;
  return (
    <ol className="flex flex-col gap-1.5">
      {steps.map((step) => {
        if (!step.done) n++;
        return (
          <li key={step.id} data-step={step.id} className="flex items-start gap-2 text-[12px] leading-relaxed">
            <span
              aria-label={step.done ? t("machines-browsersection.step_done") : t("machines-browsersection.step_todo")}
              className={cx("mt-[2px] flex size-4 shrink-0 items-center justify-center rounded-full text-[10.5px] font-semibold tabular-nums", step.done ? "bg-green-tint text-green" : "bg-accent-tint text-accent-ink")}
            >
              {step.done ? <Icon name="check" size={9} stroke={2.8} /> : n}
            </span>
            <span className={cx("min-w-0 break-words", step.done ? "text-ink-3" : "text-ink-2")}>{step.text}</span>
          </li>
        );
      })}
    </ol>
  );
}

function Line({ text, ok }: { text: string; ok?: boolean }) {
  return (
    <p className="flex items-start gap-2 text-[12.5px]" role="status">
      <span className={cx("mt-[3px] flex size-3.5 shrink-0 items-center justify-center rounded-full", ok ? "bg-green-tint text-green" : "bg-orange-tint text-orange")}>
        <Icon name={ok ? "check" : "info"} size={9} stroke={2.8} />
      </span>
      <span className={cx("min-w-0 break-words", ok ? "text-ink" : "text-ink-2")}>{text}</span>
    </p>
  );
}
