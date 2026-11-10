// Persona's profile (/assistant, the app's AssistantProfileView), opened from the Persona page and a conversation's menu:
// who it is and where it runs, what it will do on its own next, its persona, then what shapes it (Settings › Assistant's
// groups: "On its own", "About you", "Runs on") and what it can reach.
import { useEffect, useMemo, useState } from "react";
import { api } from "../../lib/api";
import { navigate } from "../../lib/router";
import { dayLabel, hm, untilLabel, upcoming, type Task } from "../../lib/routines";
import { useStore } from "../../lib/store";
import { EASE, IconButton, useNow } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { AssistantSettings } from "../settings/AssistantSettings";
import { Group, Pill } from "../settings/SettingsParts";
import { PersonaAvatar, PersonaPicker, usePersona } from "./Persona";
import { t } from "../../i18n";
import { stateName } from "../../lib/state-labels";
import { useScopedMachines } from "../../lib/current-assistant";

const REACH: { text: string; icon: IconName }[] = [
  { text: t("assistant-profile.reach_agents"), icon: "layers" },
  { text: t("assistant-profile.reach_lark"), icon: "chat" },
  { text: t("assistant-profile.reach_iphone"), icon: "monitor" },
  { text: t("assistant-profile.reach_mail"), icon: "mail" },
  { text: t("assistant-profile.reach_phone"), icon: "bell" },
];

export function Profile({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const persona = usePersona();
  const assistant = useStore((s) => s.assistant);
  const machines = useStore((s) => s.machines);
  // what it can work on: the current assistant's computers (docs/assistant/managers.md "The current assistant")
  const own = useScopedMachines();
  const working = useStore((s) => (s.assistant?.conversations ?? []).some((c) => c.state === "working"));
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const nowMs = useNow(30_000);
  const now = useMemo(() => new Date(nowMs), [nowMs]);

  useEffect(() => {
    api<{ tasks: Task[] }>("/assistant/tasks").then((r) => setTasks(r.tasks ?? []), () => setTasks([]));
  }, []);

  const machine = machines.find((m) => m.id === assistant?.machine_id);
  // "" (no computer yet) is not a name: "Runs on" then says there is none rather than leaving a gap
  const machineName = machine?.name || assistant?.machine_id || (assistant ? "" : t("assistant-profile.hub_machine"));
  const next = tasks ? upcoming(tasks, 3) : [];
  const reach = [{ text: own.length ? t("assistant-profile.reach_machines", { machines: own.map((m) => m.name).join(t("assistant-profile.list_separator")) }) : t("assistant-profile.reach_hub_computers"), icon: "machine" as IconName }, ...REACH];

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label={t("assistant-profile.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
        <h1 className="min-w-0 flex-1 truncate text-[14px] font-semibold text-ink">{persona.name}</h1>
        {assistant?.main && <IconButton icon="chat" label={t("assistant-profile.main_chat")} onClick={() => navigate({ name: "persona", sid: assistant.main })} />}
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-[760px] px-5 py-8 sm:px-8" style={{ animation: `fade-up 320ms ${EASE} both` }}>
          <div className="mb-8 flex flex-col items-center gap-2 text-center">
            <PersonaAvatar size={88} className="shadow-raised" />
            <h2 className="mt-2 text-[24px] font-semibold tracking-tight text-ink">{persona.name}</h2>
            <p className="max-w-full truncate text-[13px] text-ink-2">{machineName ? t("assistant-profile.subtitle", { machine: machineName }) : t("assistant-profile.subtitle_no_machine")}</p>
            <div className="mt-1 flex flex-wrap justify-center gap-1.5">
              {machine && <Pill tone={machine.online ? "green" : undefined}>{machine.online ? t("assistant-profile.online") : t("assistant-profile.offline")}</Pill>}
              {working && <Pill tone="accent">{stateName("working")}</Pill>}
              {next[0] && <Pill>{t("assistant-profile.next_up", { day: dayLabel(next[0].at, now), time: hm(next[0].at) })}</Pill>}
            </div>
          </div>

          {next.length > 0 && (
            <Group title={t("assistant-profile.upcoming")}>
              {next.map((u) => (
                <button key={u.id} type="button" onClick={() => navigate({ name: "routines" })} className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-hover">
                  <span className="w-12 shrink-0">
                    <span className="block text-[14px] font-semibold text-ink tabular-nums">{hm(u.at)}</span>
                    <span className="block text-[11px] whitespace-nowrap text-ink-3">{dayLabel(u.at, now)}</span>
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-1.5">
                      <Icon name={u.repeating ? "loop" : "bell"} size={12} className="shrink-0 text-accent-ink" />
                      <span className="truncate text-[13px] font-medium text-ink">{u.title}</span>
                    </span>
                    <span className="block truncate text-[11.5px] text-ink-3">{u.detail} · {untilLabel(u.at, now)}</span>
                  </span>
                </button>
              ))}
            </Group>
          )}

          <Group title={t("assistant-profile.avatar")} footer={t("assistant-profile.avatar_footer")}>
            <PersonaPicker />
          </Group>

          <AssistantSettings bare />

          <Group title={t("assistant-profile.access")} footer={t("assistant-profile.access_footer")}>
            {reach.map((r) => (
              <div key={r.text} className="flex items-center gap-3 px-4 py-2.5">
                <span className="flex size-7 shrink-0 items-center justify-center rounded-[8px] bg-field text-ink-3">
                  <Icon name={r.icon} size={14} />
                </span>
                <span className="min-w-0 flex-1 text-[13px] leading-snug text-ink">{r.text}</span>
              </div>
            ))}
          </Group>
        </div>
      </div>
    </div>
  );
}
