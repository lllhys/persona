// The sidebar's head (docs/assistant/managers.md "Clients", docs/web/web.md "Settings"): the current assistant's avatar, name and
// status. It opens the roster: every assistant with its status, where it runs and what waits there, the current one
// checked, the default one tagged; then New assistant and Manage assistants… (Settings › Assistants), and under them
// what the row held before (appearance, the other hubs, Settings, Disconnect). Picking an assistant makes it the
// current one for the whole app, kept in this browser (lib/manager-choice.ts). One assistant shows the same row: New
// assistant is always one click away. A hub without a roster (from before it) keeps the row it had.
import { memo, useState } from "react";
import { t } from "../i18n";
import { BREATHING, connStatus, phaseTone, type ConnPhase } from "../lib/connection";
import { chooseManager, pickedName, useChosenManager, useShownManager } from "../lib/manager-choice";
import { useManagerBadges } from "../lib/manager-attention";
import { loadRoster, managerError, managerOf, managerOrder, multiManager, renewCode, statusLine, switcherLine, useRoster, type InstallCode, type Manager } from "../lib/managers";
import { refreshReadiness } from "../lib/readiness";
import { navigate, type Route } from "../lib/router";
import { routeAfterSwitch } from "../lib/persona-nav";
import { AppMark } from "../ui/AppMark";
import { AssistantAvatar } from "../ui/AssistantAvatar";
import { cx, Menu, Spinner, toast, useMenu, useNow, type MenuItem } from "../ui/atoms";
import { Icon, type IconName } from "../ui/icons";
import { useDisconnect } from "./DisconnectBrowser";
import { hubMenuItems, useHubs } from "./Hubs";
import { lazyDialog } from "./LazyOverlay";
import type { ShownCode } from "./ManagerInstall";
import { setTheme, useTheme, type Theme } from "./theme";

// New assistant and the install code open only on a click: their chunk (shared with Settings › Assistants) loads on the
// first open, not with the first screen (src/startup-preload.ts FIRST_SCREEN_BUDGET).
type CodeProps = { open: boolean; shown: ShownCode | null; onClose: () => void; onCode: (c: InstallCode, id: string) => void };
const NewManager = lazyDialog(() => import("./ManagerInstall").then((m) => m.NewManager));
const CodeDialog = lazyDialog<CodeProps>(() => import("./ManagerInstall").then(({ CodeDialog: Code }) => ({ open: _open, ...rest }: CodeProps) => <Code {...rest} />));

// The install codes this page was given, by manager: a row still installing shows its code again while it lasts,
// rather than asking for a new one (which ends the old).
const codes = new Map<string, InstallCode>();

/** Makes `m` the current assistant and shows the page that is its own: a page about one thing of the assistant left
 *  behind goes back to that thing's list (lib/persona-nav.ts routeAfterSwitch). */
export function switchAssistant(m: Manager, route: Route) {
  chooseManager(m.id);
  const to = routeAfterSwitch(route);
  if (to) navigate(to);
  void refreshReadiness({ force: true });
  toast(t("sidebar.switched_to", { name: m.name }), "ok");
}

type SwitcherProps = { phase: ConnPhase; online: number; total: number; route: Route };
const NO_BADGES: Record<string, number> = {};

// The counts follow every sessions and cards push, and only several assistants have any: with one, the row reads none
// of that and stays as still as the row it replaced (SidebarShellMemo.test).
export const AssistantSwitcher = memo(function AssistantSwitcher(props: SwitcherProps) {
  return multiManager(useRoster()) ? <CountedSwitcher {...props} /> : <SwitcherRow {...props} badges={NO_BADGES} />;
});

function CountedSwitcher(props: SwitcherProps) {
  return <SwitcherRow {...props} badges={useManagerBadges()} />;
}

const SwitcherRow = memo(function SwitcherRow({ phase, online, total, route, badges }: SwitcherProps & { badges: Record<string, number> }) {
  const menu = useMenu();
  const theme = useTheme();
  const roster = useRoster();
  const current = useShownManager();
  const otherHubs = hubMenuItems(useHubs());
  const disconnect = useDisconnect();
  const [creating, setCreating] = useState(false);
  const [shown, setShown] = useState<ShownCode | null>(null);
  const [asking, setAsking] = useState(false);
  // the lines count minutes (an install code running out, "last synced"): once a minute while the list is open
  const now = useNow(30_000, Boolean(menu.anchor));
  const known = managerOf(roster, current);
  // before the roster is read (a reload), a kept pick shows the name the roster last gave it, not the hub's own row
  const picked = useChosenManager();
  const me: Pick<Manager, "id" | "name"> | null = known ?? (!roster && picked ? { id: picked, name: pickedName() ?? picked } : null);
  const linked = phase === "online";
  const othersWaiting = Object.entries(badges).some(([id, n]) => id !== current && n > 0);

  const themes: { k: Theme; label: string; icon: IconName }[] = [
    { k: "system", label: t("sidebar.theme_system"), icon: "monitor" },
    { k: "light", label: t("sidebar.theme_light"), icon: "sun" },
    { k: "dark", label: t("sidebar.theme_dark"), icon: "moon" },
  ];

  // a manager still installing: its install commands, with the code this page has while it lasts, else a new one
  const showCode = async (m: Manager) => {
    const kept = codes.get(m.id);
    if (kept && Date.parse(kept.expires_at) > Date.now()) return setShown({ id: m.id, name: m.name, code: kept, fresh: true });
    setAsking(true);
    try {
      const r = await renewCode(m.id, m.name, true);
      codes.set(r.id, r.code);
      setShown({ id: r.id, name: m.name, code: r.code, fresh: true });
      void loadRoster();
    } catch (e) {
      toast(managerError(e), "error");
    } finally {
      setAsking(false);
    }
  };

  const installed = roster ? managerOrder(roster) : [];
  const installing = roster ? roster.managers.filter((m) => m.installing) : [];
  const several = installed.length + installing.length > 1;
  const row = (m: Manager): MenuItem => ({
    key: `assistant:${m.id}`,
    label: m.name,
    tag: several && m.id === roster?.default ? t("settings-managers.default") : undefined,
    detail: switcherLine(m, now),
    lead: <AssistantAvatar id={m.id} name={m.name} isDefault={m.id === roster?.default} size={28} status={statusLine(m, now).tone} ring="ring-surface" />,
    // an offline one: the whole row faded (avatar, name and line), still chosen like the others
    dim: !m.online && !m.installing,
    ...(m.installing ? {} : { radio: true, checked: m.id === current, badge: m.id === current ? 0 : badges[m.id] ?? 0, badgeLabel: t("sidebar.switcher_count", { count: badges[m.id] ?? 0 }) }),
    onSelect: () => {
      if (m.installing) void showCode(m);
      else if (m.id !== current) switchAssistant(m, route);
    },
  });
  const items: MenuItem[] = [
    ...[...installed, ...installing].map(row),
    ...(roster
      ? [
          { key: "new-assistant", label: t("sidebar.new_assistant"), icon: "plus" as const, divider: true, onSelect: () => setCreating(true) },
          { key: "manage-assistants", label: t("sidebar.manage_assistants"), icon: "gear" as const, onSelect: () => navigate("/settings?tab=managers") },
        ]
      : []),
    // the three looks as rows of their own under a small title, as the row's menu had them: a submenu would open over
    // the rows under it on a narrow screen (a phone, a zoomed page) and hide Settings and Disconnect
    { key: "appearance", label: t("sidebar.appearance"), heading: true, divider: Boolean(roster), onSelect: () => {} },
    ...themes.map((x) => ({ key: x.k, label: x.label, icon: x.icon, checked: theme === x.k, onSelect: () => setTheme(x.k) })),
    // the other hubs in this browser's list (#1014); none, and the menu is as before
    ...otherHubs,
    { key: "settings", label: t("sidebar.nav_settings"), icon: "gear", divider: otherHubs.length > 0, onSelect: () => navigate({ name: "settings" }) },
    { key: "logout", label: t("sidebar.disconnect"), icon: "logout", danger: true, onSelect: disconnect.start },
  ];

  const name = me?.name ?? "Persona";
  const tone = known ? statusLine(known, now).tone : undefined;
  return (
    <>
      <button
        type="button"
        onClick={menu.open}
        aria-haspopup="menu"
        aria-expanded={Boolean(menu.anchor)}
        aria-label={me ? t(othersWaiting ? "sidebar.switcher_label_waiting" : "sidebar.switcher_label", { name }) : undefined}
        data-assistant-switcher={me?.id ?? ""}
        className={cx("flex w-full items-center gap-2.5 rounded-control p-1.5 text-left transition-[background-color,transform] duration-100 hover:bg-hover active:scale-[0.98]", menu.anchor && "bg-hover")}
      >
        {me ? <AssistantAvatar id={me.id} name={me.name} isDefault={me.id === roster?.default} size={32} status={linked ? tone : undefined} waiting={othersWaiting} /> : <AppMark size={32} radius={8} className="shadow-hairline" />}
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13px] leading-tight font-medium text-ink">{name}</span>
          <span className="flex items-center gap-1.5 truncate text-[11px] leading-tight text-ink-3">
            {/* the hub out of reach matters more than the assistant's own state: its line and dot take the place */}
            {known && linked ? (
              <span className="truncate">{switcherLine(known, now)}</span>
            ) : (
              <>
                <span aria-hidden data-conn-dot className={cx("size-1.5 shrink-0 rounded-full", phaseTone(phase).color)} style={phaseTone(phase).breathing ? BREATHING : undefined} />
                <span className="truncate">{connStatus(phase, online, total)}</span>
              </>
            )}
          </span>
        </span>
        {/* the menu is gone while the hub revokes this browser's token or makes an install code: the row says it is busy */}
        {disconnect.busy || asking ? <Spinner size={12} /> : <Icon name="chevron" size={12} stroke={2} className={cx("shrink-0 text-ink-3 transition-transform duration-200", menu.anchor && "rotate-180")} />}
      </button>
      {menu.anchor && <Menu anchor={menu.anchor} onClose={menu.close} width={264} header={roster ? t("sidebar.assistants") : undefined} items={items} />}
      {disconnect.dialog}
      <NewManager
        open={creating}
        onClose={() => setCreating(false)}
        onMade={(m, code) => {
          setCreating(false);
          codes.set(m.id, code);
          setShown({ id: m.id, name: m.name, code, fresh: true });
          void loadRoster();
        }}
      />
      <CodeDialog
        open={Boolean(shown)}
        shown={shown}
        onClose={() => setShown(null)}
        onCode={(code, id) => {
          codes.set(id, code);
          setShown((s) => (s ? { ...s, code, id } : s));
          void loadRoster();
        }}
      />
    </>
  );
});
