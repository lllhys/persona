// The one quiet line about the hub at the top of the Agents list (#2897, docs/operations/hub-update.md "In the app", the app's
// HubNoticeRow): an update under way, one that went back, auto-update turned on (once), a newer version or Docker image.
// Clicking the line opens Settings › hub; its buttons each do their own thing. Follows the hub's `hub_update` pushes and
// reads again on a fresh hello (a hub that just restarted into a new version).
import { useCallback, useEffect, useState } from "react";
import { errorText } from "../../lib/api";
import {
  dismissedHubNotices, dismissHubNotice, hubNotice, hubNoticeKey, hubNoticeText, hubUpdate, setHubUpdateSettings, startHubUpdate,
  type HubNotice, type HubUpdateStatus,
} from "../../lib/hub-update";
import { isResumed } from "../../lib/reconnect";
import { navigate } from "../../lib/router";
import { onHubEvent } from "../../lib/store";
import { Spinner, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { t } from "../../i18n";

export function HubNoticeLine() {
  const [status, setStatus] = useState<HubUpdateStatus | null>(null);
  const [dismissed, setDismissed] = useState(() => dismissedHubNotices());

  useEffect(() => {
    let dead = false;
    // Only the newest read may land, and a hub_update push outdates every read still on its way: a GET that left
    // before the push would otherwise put the older status back (an "Update" button in the middle of an update).
    let seq = 0;
    // an older hub without /hub/update, or one that can't be read now: no line
    const read = () => {
      const mine = ++seq;
      void hubUpdate().then((s) => !dead && mine === seq && setStatus(s), () => {});
    };
    read();
    const offUpdate = onHubEvent("hub_update", (m) => {
      if (!m.update) return;
      seq++;
      setStatus(m.update as HubUpdateStatus);
    });
    // A resumed hello does not read: the hub replays the hub_update pushes it missed, which carry the update state. The
    // auto-update settings behind the auto_on and available lines are not pushed, so a change another device made while
    // the socket was down shows with the next push or fresh hello (Settings › hub works the same way).
    const offHello = onHubEvent("hello", (m) => !isResumed(m) && read());
    return () => {
      dead = true;
      offUpdate();
      offHello();
    };
  }, []);

  const run = useCallback(async (call: () => Promise<HubUpdateStatus>) => {
    try {
      setStatus(await call());
    } catch (e) {
      toast(errorText(e), "error");
    }
  }, []);

  const notice = hubNotice(status, dismissed);
  if (!notice) return null;
  return (
    <HubNoticeView
      notice={notice}
      onOpen={() => navigate("/settings?tab=hub")}
      onAutoOff={() => void run(() => setHubUpdateSettings({ auto: false }))}
      onSeen={() => void run(() => setHubUpdateSettings({ auto_notice_seen: true }))}
      onUpdate={() => void run(() => startHubUpdate(false))}
      onDismiss={(key) => setDismissed(dismissHubNotice(key))}
    />
  );
}

export function HubNoticeView({ notice, onOpen, onAutoOff, onSeen, onUpdate, onDismiss }: {
  notice: HubNotice;
  onOpen: () => void;
  onAutoOff: () => void;
  onSeen: () => void;
  onUpdate: () => void;
  onDismiss: (key: string) => void;
}) {
  const warn = notice.kind === "rolled_back";
  const key = hubNoticeKey(notice);
  const action = "touch-hit shrink-0 whitespace-nowrap rounded-control px-2 py-0.5 text-[12px] font-medium text-accent-ink hover:bg-hover";
  return (
    <div data-testid="hub-notice" data-kind={notice.kind} className="relative z-10 mt-1.5 flex items-start gap-1 rounded-[7px] px-2 py-1.5 hover:bg-hover [@media(pointer:coarse)]:min-h-11">
      <button type="button" onClick={onOpen} title={t("lib-hub-update.notice_open")} className="flex min-w-0 flex-1 items-start gap-2 text-left" aria-live="polite">
        <span className={`mt-[3px] flex size-3.5 shrink-0 items-center justify-center ${warn ? "text-orange" : "text-accent-ink"}`}>
          {notice.kind === "updating" ? <Spinner size={11} /> : <Icon name={warn ? "warning" : "retry"} size={13} stroke={2.2} />}
        </span>
        <span className="line-clamp-2 min-w-0 flex-1 text-[13px] leading-[1.45] text-ink-2">{hubNoticeText(notice)}</span>
      </button>
      {notice.kind === "auto_on" && (
        <>
          <button type="button" data-testid="hub-notice-auto-off" onClick={onAutoOff} className={action}>{t("lib-hub-update.notice_turn_off")}</button>
          <button type="button" data-testid="hub-notice-seen" onClick={onSeen} className={action}>{t("lib-hub-update.notice_got_it")}</button>
        </>
      )}
      {notice.kind === "available" && (
        <button type="button" data-testid="hub-notice-update" onClick={onUpdate} className={action}>{t("lib-hub-update.notice_update")}</button>
      )}
      {key && (
        <button type="button" data-testid="hub-notice-dismiss" aria-label={t("lib-hub-update.notice_dismiss")} title={t("lib-hub-update.notice_dismiss")} onClick={() => onDismiss(key)} className="touch-hit -mt-0.5 shrink-0 rounded-control p-1 text-ink-3 hover:bg-hover hover:text-ink-2">
          <Icon name="x" size={12} stroke={2} />
        </button>
      )}
    </div>
  );
}
