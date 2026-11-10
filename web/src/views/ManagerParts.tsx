// What the pages show of the managers once there is more than one (docs/assistant/managers.md "Clients"): the assistant page's
// title, a manager's heading where a list still names whose a thing is, and what an offline manager's assistant page
// says. With one manager none of it renders, so those pages stay as they were. The switcher itself is the sidebar's
// head (views/AssistantSwitcher.tsx).
import { useRef, useState } from "react";
import { t } from "../i18n";
import { lastSyncedText, managerOrder, multiManager, useRoster, type Manager } from "../lib/managers";
import { pickedName, useChosenManager, useShownManager } from "../lib/manager-choice";
import { ago } from "../lib/format";
import { api, enc, errorText } from "../lib/api";
import { Button, cx, Menu, toast, useMenu } from "../ui/atoms";
import { AssistantAvatar } from "../ui/AssistantAvatar";
import { MAIN_TITLE } from "./assistant/conversations";

/** The status dot of a manager: green online, grey offline. */
function Dot({ online }: { online: boolean }) {
  return <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", online ? "bg-green" : "bg-line-strong")} />;
}

/** The assistant page's title: the current assistant's name once there are several (the sidebar's switcher picks it,
 *  views/AssistantSwitcher.tsx), else Persona as before. */
export function useAssistantTitle(): string {
  const roster = useRoster();
  const shown = useShownManager();
  const picked = useChosenManager();
  // before the roster is read (a reload): a kept pick's name as the roster last gave it
  if (!roster && picked) return pickedName() || MAIN_TITLE;
  return (multiManager(roster) && roster.managers.find((m) => m.id === shown)?.name) || MAIN_TITLE;
}

/** The heading of one manager's group in a list: its name, and when an offline one last synced. */
export function ManagerHeading({ manager, count, className }: { manager: Manager; count?: number; className?: string }) {
  const synced = lastSyncedText(manager);
  return (
    <div className={cx("flex min-w-0 items-center gap-2 whitespace-nowrap", className)}>
      <Dot online={manager.online} />
      <span className={cx("truncate text-[12.5px] font-semibold", manager.online ? "text-ink-2" : "text-ink-3")}>{manager.name}</span>
      {count !== undefined && <span className="text-[11.5px] text-ink-3 tabular-nums">{count}</span>}
      {synced && <span className="ml-auto truncate text-[11.5px] text-ink-3">{synced}</span>}
    </div>
  );
}

/** What a composer says for a conversation of manager id while it is offline (several managers only): "<name> · last
 *  synced …"; undefined when it is online, unknown, or the hub has one manager. Such a conversation can't be sent to,
 *  wherever it was opened from (the inbox, a notification, the Agents list). */
export function useManagerOffline(id: string | undefined): string | undefined {
  const roster = useRoster();
  if (!id || !multiManager(roster)) return undefined;
  const m = roster.managers.find((x) => x.id === id);
  if (!m || m.online || m.installing) return undefined;
  const when = ago(m.last_seen);
  return when ? t("settings-managers.offline_composer", { name: m.name, time: when }) : t("settings-managers.offline_composer_never", { name: m.name });
}

/** The current assistant while it is offline (several assistants only), and what the pages say of it. */
export interface ShownOffline {
  manager: Manager;
  /** Whether it is the roster's default (its avatar's color). */
  isDefault: boolean;
  /** The assistant page's composer: "<name> · last synced …". */
  composer: string;
  /** The assistant page's starter: it can read the history and not send. */
  body: string;
  /** A list's page (OfflineState): "<name> is offline", and when it last synced and that the page updates once it's back. */
  title: string;
  note: string;
}

/** The shown manager when it is offline (only with more than one): the assistant page then can't send, and the lists
 *  the hub merges say it is offline (OfflineState). */
export function useShownOffline(): ShownOffline | null {
  const roster = useRoster();
  const shown = useShownManager();
  if (!multiManager(roster)) return null;
  const m = roster.managers.find((x) => x.id === shown);
  if (!m || m.online || m.installing) return null;
  const when = ago(m.last_seen);
  return {
    manager: m,
    isDefault: m.id === roster.default,
    composer: when ? t("settings-managers.offline_composer", { name: m.name, time: when }) : t("settings-managers.offline_composer_never", { name: m.name }),
    body: t("settings-managers.offline_body", { name: m.name }),
    title: t("settings-managers.offline_page_title", { name: m.name }),
    note: when ? t("settings-managers.offline_page_body", { time: when }) : t("settings-managers.offline_page_body_never"),
  };
}

/** In place of a list's empty state (or its first-run page) while the current assistant is offline (useShownOffline):
 *  the hub's merged lists (the inbox, to-dos) leave out a manager that doesn't answer (docs/protocol/protocol.md
 *  "Per-manager routing"), so its list would read as empty. Its avatar with the offline mark, as in the switcher. */
export function OfflineState({ offline, ring = "ring-page", className }: { offline: ShownOffline; /** The surface it sits on, which the mark is cut out of. */ ring?: string; className?: string }) {
  const { manager } = offline;
  return (
    <div role="status" data-assistant-offline={manager.id} className={cx("flex flex-col items-center gap-2 px-6 text-center", className)}>
      <AssistantAvatar id={manager.id} name={manager.name} isDefault={offline.isDefault} size={40} status="grey" ring={ring} className="mb-1 opacity-60" />
      <p className="text-[13.5px] font-medium text-ink">{offline.title}</p>
      <p className="max-w-80 text-[12.5px] leading-relaxed text-ink-3">{offline.note}</p>
    </div>
  );
}

/** Move to… on a computer's page: gives it to another manager (`POST /managers/{id}/machines`, docs/assistant/managers.md "Moving a
 *  machine or a source"); its sessions and cards follow, the machines push shows where it went. Only with more than
 *  one manager, and not for a linked hub's computer (not movable). */
export function MoveToManager({ machineId, manager, movable = true }: { machineId: string; manager?: string; movable?: boolean }) {
  const roster = useRoster();
  const menu = useMenu();
  const [busy, setBusy] = useState(false);
  const at = useRef<HTMLSpanElement>(null);
  if (!multiManager(roster) || !movable) return null;
  const owner = manager || roster.default;
  const move = async (to: Manager) => {
    setBusy(true);
    try {
      await api(`/managers/${enc(to.id)}/machines`, { method: "POST", json: { machine_id: machineId } });
      toast(t("settings-managers.moved", { name: to.name }), "ok");
    } catch (e) {
      toast(t("settings-managers.move_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <span ref={at} className="contents">
        <Button tone="secondary" icon="arrowRight" busy={busy} onClick={() => (menu.anchor ? menu.close() : menu.openAt(at.current?.firstElementChild as HTMLElement | null))}>
          {t("settings-managers.move_to")}
        </Button>
      </span>
      {menu.anchor && (
        <Menu
          anchor={menu.anchor}
          onClose={menu.close}
          align="right"
          header={t("settings-managers.move_title")}
          items={managerOrder(roster).map((m) => ({
            key: m.id,
            label: m.name,
            checked: m.id === owner,
            hint: m.online ? undefined : t("settings-managers.move_offline"),
            disabled: m.id === owner,
            onSelect: () => void move(m),
          }))}
        />
      )}
    </>
  );
}
