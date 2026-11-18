// "Devices": every device credential the hub issued (phones, watches, browsers, persona-links), revoking one, and a pairing
// QR code for a new phone. On a member hub the center issues and revokes them all (409 member_hub): this page says so.
import { t } from "../../i18n";
import { tRich } from "../../i18n/rich";
import { useEffect, useState } from "react";
import { ago } from "../../lib/format";
import { Button, Dialog, toast, useNow } from "../../ui/atoms";
import { adminApi, AdminError, errorText } from "../api";
import { AdminIcon, type AdminIconName } from "../icons";
import { activeDevices, centerName, countdown, dateTime, deviceKindLabel, roleLabel } from "../logic";
import type { Device, PhonePair } from "../types";
import { devDialog } from "../router";
import { useShared } from "../store";
import { Card, ConfirmButton, CopyValue, EmptyState, ErrorBox, Loader, PageHeader, Qr, Tag, useLoad } from "../parts";
import { PasswordLoginSettings } from "./PasswordLogin";

const KIND_ICON: Record<string, AdminIconName> = { phone: "phone", watch: "pulse", link: "link", web: "web", other: "web" };

export function Devices() {
  const { data, error, loading, reload, set } = useLoad(async () => activeDevices((await adminApi<{ devices: Device[] }>("/devices")).devices));
  const [pairing, setPairing] = useState(() => devDialog() === "pair");
  const { mesh } = useShared();
  const refused = error instanceof AdminError && error.code === "member_hub";
  const member = refused || mesh?.role === "member";
  return (
    <>
      <PageHeader
        title={t("admin-nav.devices")}
        subtitle={t("admin-pages-devices.subtitle")}
        right={
          !member && (
            <Button tone="primary" onClick={() => setPairing(true)}>
              <AdminIcon name="qr" size={13} />
              {t("admin-pages-devices.pair_phone")}
            </Button>
          )
        }
      />
      {!member && <PasswordLoginSettings />}
      {member && (refused || !data) ? (
        <Card>
          <EmptyState icon="mesh" title={mesh?.role === "member" ? t("admin-pages-devices.managed_by_named", { name: centerName(mesh) }) : t("admin-pages-devices.managed_by_primary")}>
            {t("admin-pages-devices.member_detail")}
          </EmptyState>
        </Card>
      ) : !data ? (
        loading ? <Loader /> : <ErrorBox error={error} onRetry={reload} />
      ) : (
        <Card title={t("admin-pages-devices.credentials", { count: data.length })} footer={t("admin-pages-devices.footer")}>
          {data.length === 0 ? (
            <EmptyState icon="phone" title={t("admin-pages-devices.empty_title")} action={<Button onClick={() => setPairing(true)}>{t("admin-pages-devices.pair_phone")}</Button>}>
              {t("admin-pages-devices.empty_detail")}
            </EmptyState>
          ) : (
            data.map((d) => <DeviceRow key={d.id} d={d} onRevoked={() => set((cur) => cur?.filter((x) => x.id !== d.id))} />)
          )}
        </Card>
      )}
      <PairPhone open={pairing} onClose={() => (setPairing(false), reload())} />
    </>
  );
}

function DeviceRow({ d, onRevoked }: { d: Device; onRevoked: () => void }) {
  const [busy, setBusy] = useState(false);
  const revoke = async () => {
    setBusy(true);
    try {
      await adminApi(`/devices/${encodeURIComponent(d.id)}`, "DELETE");
      toast(t("admin-pages-devices.revoked", { name: d.name }), "ok");
      onRevoked();
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-[10px] bg-inset text-ink-2 shadow-hairline">
        <AdminIcon name={KIND_ICON[d.kind] ?? "key"} size={16} />
      </span>
      <div className="min-w-[180px] flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-[13.5px] font-medium text-ink">{d.name}</span>
          <Tag tone={d.role === "full" ? "accent" : "gray"}>{roleLabel(d.role)}</Tag>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-ink-3">
          <span className="whitespace-nowrap">{deviceKindLabel(d.kind)}</span>
          {d.machine_id && (
            <>
              <span>·</span>
              <span className="whitespace-nowrap">{tRich("admin-pages-devices.bound_to", { machine: <span className="font-mono text-ink-2">{d.machine_id}</span> })}</span>
            </>
          )}
          <span>·</span>
          <span className="whitespace-nowrap">{d.last_seen ? t("admin-pages-devices.used_ago", { ago: ago(d.last_seen) }) : t("admin-pages-devices.never_used")}</span>
          {d.created_at && (
            <>
              <span className="hidden sm:inline">·</span>
              <span className="hidden whitespace-nowrap sm:inline">{t("admin-pages-devices.paired_at", { time: dateTime(d.created_at) })}</span>
            </>
          )}
        </div>
      </div>
      <ConfirmButton label={t("admin-pages-devices.revoke")} confirm={t("admin-pages-devices.revoke")} busy={busy} onConfirm={revoke} />
    </div>
  );
}

function PairPhone({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [pair, setPair] = useState<PhonePair | null>(null);
  const [error, setError] = useState("");
  const [round, setRound] = useState(0);
  const now = useNow(1000, Boolean(pair));
  useEffect(() => {
    if (!open) return;
    let live = true;
    setPair(null);
    setError("");
    adminApi<PhonePair>("/devices/pair", "POST").then(
      (p) => live && setPair(p),
      (e) => live && setError(errorText(e)),
    );
    return () => {
      live = false;
    };
  }, [open, round]);
  const left = pair ? countdown(pair.expires_at, now) : "";
  return (
    <Dialog open={open} onClose={onClose} title={t("admin-pages-devices.pair_phone")} width={460}>
      {!pair && !error && <div className="py-10 text-center text-[12.5px] text-ink-3">{t("admin-pages-devices.generating")}</div>}
      {error && <p className="py-3 text-[12.5px] text-red">{error}</p>}
      {pair && (
        <div className="flex flex-col items-center">
          <div className="relative">
            <Qr rows={pair.qr.rows} label={t("admin-pages-devices.qr_label")} size={208} />
            {!left && (
              <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 rounded-[10px] bg-surface/90 backdrop-blur-[2px]">
                <span className="text-[12.5px] text-ink-2">{t("admin-pages-devices.qr_expired")}</span>
                <Button icon="retry" onClick={() => setRound((r) => r + 1)}>
                  {t("admin-pages-devices.regenerate")}
                </Button>
              </div>
            )}
          </div>
          <p className="mt-3 text-center text-[12.5px] leading-relaxed text-ink-2">
            {left ? tRich("admin-pages-devices.scan_left", { left: <span className="font-mono text-ink">{left}</span> }) : t("admin-pages-devices.scan_expired")}
          </p>
          <div className="mt-4 w-full divide-y divide-line overflow-hidden rounded-control bg-inset shadow-hairline">
            <div className="flex items-center gap-3 px-3 py-2">
              <span className="min-w-14 shrink-0 whitespace-nowrap text-[11.5px] text-ink-3">{t("admin-pages-devices.address")}</span>
              <CopyValue value={pair.url} className="min-w-0 flex-1" />
            </div>
            {pair.fp && (
              <div className="flex items-center gap-3 px-3 py-2">
                <span className="min-w-14 shrink-0 whitespace-nowrap text-[11.5px] text-ink-3">{t("admin-pages-devices.fingerprint")}</span>
                <CopyValue value={pair.fp} className="min-w-0 flex-1" />
              </div>
            )}
            <div className="flex items-center gap-3 px-3 py-2">
              <span className="min-w-14 shrink-0 whitespace-nowrap text-[11.5px] text-ink-3">{t("admin-pages-devices.link")}</span>
              <CopyValue value={pair.pair_url} label={t("admin-pages-devices.copy_pair_link")} className="min-w-0 flex-1" />
            </div>
          </div>
          <p className="mt-3 text-center text-[11.5px] leading-relaxed text-ink-3">{t("admin-pages-devices.qr_warning")}</p>
        </div>
      )}
    </Dialog>
  );
}
