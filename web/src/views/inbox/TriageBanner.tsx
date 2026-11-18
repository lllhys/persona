// The banner over the inbox list while new items wait for triage (#1773, docs/assistant/inbox.md "Waiting-for-triage banner"): how many, from
// where, and why — the hub's reason, else the state's words (lib/inbox.ts triageBanner). "Set up" opens the model
// settings of "Triage" when no model is set up; the other states only say why. Not dismissable: it goes when triage is
// ok again (the list's next pull). The same words, place and tone as iOS and Android.
import { navigate } from "../../lib/router";
import { triageBanner, type InboxTriage } from "../../lib/inbox";
import { TRIAGE_SETTINGS } from "../../lib/inbox-page";
import { t } from "../../i18n";
import { Button } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

/** `canConfigure`: this device may change settings (a web page always pairs as a full device). */
export function TriageBanner({ triage, canConfigure = true }: { triage: InboxTriage | null | undefined; canConfigure?: boolean }) {
  const b = triageBanner(triage);
  if (!b) return null;
  return (
    <div role="status" data-triage={triage?.state} className="mt-3 flex items-center gap-2.5 rounded-card bg-orange-tint px-3 py-2.5 text-orange">
      <Icon name="clock" size={14} stroke={2.2} className="shrink-0" />
      <p className="min-w-0 flex-1 text-[12.5px] leading-[1.5] font-medium text-pretty break-words">{b.text}</p>
      {b.configure && canConfigure && (
        <Button tone="secondary" className="h-7 px-2.5 text-[12px]" onClick={() => navigate(TRIAGE_SETTINGS)}>
          {t("inbox-triagebanner.configure")}
        </Button>
      )}
    </div>
  );
}
