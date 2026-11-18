// Settings › Notifications › "Show at the MacBook notch" (#7669): the desktop app on macOS draws the Agents' counts around
// the notch, the iPhone's Dynamic Island on a MacBook (lib/desktop-island.ts sends what it shows). Off by default; the
// desktop app keeps the choice for this computer, as it keeps launch at login. Hidden in a browser, on Windows and in a
// desktop app before it. The desktop's answer goes to the island's rows too (setIslandOn), which list every agent only
// while it is on.
import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { errorText } from "../../lib/api";
import { setIslandOn } from "../../lib/desktop-island";
import { toast } from "../../ui/atoms";
import { Group, Row, Switch } from "./SettingsParts";
import type { DesktopIsland } from "../../lib/desktop-types";

export function DesktopIslandSettings() {
  const desktop = typeof window === "undefined" ? undefined : window.personaDesktop;
  const [island, setIsland] = useState<DesktopIsland | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!desktop?.getIsland) return;
    let live = true;
    desktop.getIsland().then((v: DesktopIsland) => {
      if (!live) return;
      setIsland(v);
      setIslandOn(v.enabled);
    }, () => {});
    return () => { live = false; };
  }, [desktop]);

  if (!island?.supported || !desktop?.setIslandEnabled) return null;

  const toggle = async (on: boolean) => {
    setBusy(true);
    try {
      const next = await desktop.setIslandEnabled!(on);
      setIsland(next);
      setIslandOn(next.enabled);
    } catch (e) {
      toast(t("settings-notifications.island_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Group footer={t("settings-notifications.island_footer")}>
      <Row icon="machine" anchor="notify-island" label={t("settings-notifications.island")} hint={t("settings-notifications.island_hint")}>
        <Switch on={island.enabled} onChange={(on) => void toggle(on)} disabled={busy} label={t("settings-notifications.island")} />
      </Row>
    </Group>
  );
}
