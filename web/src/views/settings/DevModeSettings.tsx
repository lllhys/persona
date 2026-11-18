import { lazy, Suspense, useEffect, useState } from "react";
import { t } from "../../i18n";
import { devModeSince } from "../../lib/dev-probe";
import { bytes } from "../../lib/format";
import { Button, Dialog } from "../../ui/atoms";
import { Row, Switch } from "./SettingsParts";

// Export Diagnostics comes with its own chunk (the recorder's files, jszip, the Send To list), on the first open.
const DevExportDialog = lazy(() => import("./DevExportDialog").then((m) => ({ default: m.DevExportDialog })));

/** Settings › About Persona's Developing Persona rows (#9617, docs/operations/developing-persona.md): the switch, and
 *  while it is on, what is recorded and Export Diagnostics. Turning it off with something recorded asks first and offers
 *  to export. The recorder (lib/dev-mode.ts) loads only once the switch is on. */
export function DevModeRows() {
  const [since, setSince] = useState(devModeSince);
  const [size, setSize] = useState<number | null>(null);
  const [asking, setAsking] = useState(false);
  const [exporting, setExporting] = useState(false);

  useEffect(() => {
    if (!since) return;
    let live = true;
    void import("../../lib/dev-mode").then(async (m) => {
      const r = m.startDevMode();
      await r.flush();
      const n = await r.store.size();
      if (live) setSize(n);
    });
    return () => {
      live = false;
    };
  }, [since, exporting]);

  const turn = async (on: boolean) => {
    if (!on && (size ?? 0) > 0) {
      setAsking(true);
      return;
    }
    const m = await import("../../lib/dev-mode");
    await m.setDevMode(on);
    setSize(null);
    setSince(devModeSince());
  };
  const off = async () => {
    setAsking(false);
    const m = await import("../../lib/dev-mode");
    await m.setDevMode(false);
    setSize(null);
    setSince(null);
  };

  const when = since ? new Date(since).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) : "";
  return (
    <>
      <Row label={t("settings-devmode.title")} hint={t("settings-devmode.hint")} wrapHint anchor="developing-persona">
        <Switch on={Boolean(since)} onChange={(on) => void turn(on)} label={t("settings-devmode.title")} />
      </Row>
      {since && (
        <Row label={t("settings-devmode.recorded")} hint={size === null ? t("settings-devmode.since", { since: when }) : t("settings-devmode.since_size", { since: when, size: bytes(size) })} wrapHint>
          <Button icon="download" onClick={() => setExporting(true)} testId="dev-mode-export">
            {t("settings-devmode.export")}
          </Button>
        </Row>
      )}
      <Dialog
        open={asking}
        onClose={() => setAsking(false)}
        title={t("settings-devmode.confirm_title", { size: bytes(size ?? 0) })}
        footer={
          <div className="flex flex-wrap justify-end gap-2">
            <Button tone="quiet" onClick={() => setAsking(false)}>
              {t("settings-devmode.cancel")}
            </Button>
            <Button
              onClick={() => {
                setAsking(false);
                setExporting(true);
              }}
            >
              {t("settings-devmode.export_first")}
            </Button>
            <Button tone="danger" onClick={() => void off()} testId="dev-mode-off">
              {t("settings-devmode.turn_off")}
            </Button>
          </div>
        }
      >
        <p className="text-meta text-ink-2">{t("settings-devmode.confirm_body")}</p>
      </Dialog>
      {exporting && (
        <Suspense fallback={null}>
          <DevExportDialog since={since} size={size} onClose={() => setExporting(false)} />
        </Suspense>
      )}
    </>
  );
}
