// The entry row to the source subscription pages (#2041) on an extension's details and on a connector's page: shown only
// when its connector can be subscribed (or already is), with how many are on and failing.
import { t } from "../../i18n";
import { navigate } from "../../lib/router";
import { countsOf, subscriptionsHref, useSubscriptionSummary, type SubscriptionCounts, type SubscriptionSummaryLine, type SubscriptionsScope } from "../../lib/subscriptions";
import { Group, Row } from "../settings/SettingsParts";

/** An entry row's line: how many are on and failing, or that some can be subscribed. */
export function countsText(c: SubscriptionCounts): string {
  if (c.failing > 0) return t("subscriptions.counts_failing", { on: c.on, failing: c.failing });
  return c.on > 0 ? t("subscriptions.counts_on", { on: c.on }) : t("subscriptions.counts_none");
}

export function SubscriptionsEntry({ pick, scope }: { pick: (l: SubscriptionSummaryLine) => boolean; scope: SubscriptionsScope }) {
  const counts = countsOf(useSubscriptionSummary(), pick);
  if (!counts) return null;
  return (
    <Group>
      <Row icon="loop" label={t("subscriptions.row")} hint={countsText(counts)} onClick={() => navigate(subscriptionsHref(scope))} />
    </Group>
  );
}
