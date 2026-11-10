// A Persona conversation's menu in the sidebar (the iOS sidebar's long press, Android's drawer): "Pin" / "Unpin",
// "Rename…", "Archive", "Copy session ID", then "End…" while it runs. The main conversation is never pinned, renamed or archived.
// Pins are this browser's (lib/conversation-pins.ts); the rest is the hub's (the same calls as "All chats").
import { api, enc, errorText, sessionPath } from "../lib/api";
import { togglePinnedConversation, unpinAllConversations } from "../lib/conversation-pins";
import { sessionTitle } from "../lib/format";
import { refreshAssistant } from "../lib/store";
import type { Session } from "../lib/types";
import { copyText, Menu, toast, type MenuItem, type ToastAction } from "../ui/atoms";
import type { MenuAt } from "../ui/menu-model";
import { t } from "../i18n";

/** What the hub does for the menu; tests swap it. */
export interface ConversationMenuDeps {
  rename: (s: Session, title: string) => Promise<unknown>;
  archive: (s: Session) => Promise<unknown>;
  unarchive: (s: Session) => Promise<unknown>;
  end: (s: Session) => Promise<unknown>;
  refresh: () => Promise<unknown>;
  copy: (text: string) => unknown;
  notify: (text: string, tone: "ok" | "error", action?: ToastAction) => void;
}

export const conversationMenuDeps: ConversationMenuDeps = {
  rename: (s, title) => api(`${sessionPath(s.machine_id, s.session_id)}/rename`, { method: "POST", json: { title } }),
  archive: (s) => api(`/assistant/conversations/${enc(s.session_id)}/archive`, { method: "POST", json: {} }),
  unarchive: (s) => api(`/assistant/conversations/${enc(s.session_id)}/unarchive`, { method: "POST", json: {} }),
  end: (s) => api(`${sessionPath(s.machine_id, s.session_id)}/close`, { method: "POST" }),
  refresh: () => refreshAssistant(),
  copy: copyText,
  notify: (text, tone, action) => toast(text, tone, action),
};

/** A running conversation can be ended (iOS `status.isLive`). */
export const conversationLive = (s: Session) => s.status !== "offline";

const reread = (deps: ConversationMenuDeps) => void deps.refresh().catch(() => {});

async function undoArchive(s: Session, title: string, deps: ConversationMenuDeps) {
  try {
    await deps.unarchive(s);
    deps.notify(t("conversation-menu.unarchived", { title }), "ok");
  } catch (e) {
    deps.notify(t("conversation-menu.undo_failed", { error: errorText(e) }), "error");
  }
  reread(deps);
}

/** `leader`: a leader conversation, pinned by the hub on every browser, so it has no Pin / Unpin of its own. */
export function conversationMenuItems(s: Session, o: { main: boolean; pinned: boolean; leader?: boolean }, deps: ConversationMenuDeps = conversationMenuDeps): MenuItem[] {
  const title = sessionTitle(s);
  const own: MenuItem[] = o.main
    ? []
    : [
        ...(o.leader ? [] : [{ key: "pin", label: o.pinned ? t("conversation-menu.unpin") : t("conversation-menu.pin"), icon: "pin" as const, accel: "p", onSelect: () => togglePinnedConversation(s.session_id) }]),
        {
          key: "rename",
          label: t("conversation-menu.rename"),
          icon: "pencil",
          accel: "r",
          onSelect: () => {},
          rename: {
            value: s.title ?? "",
            placeholder: t("conversation-menu.title_placeholder"),
            commit: async (name) => {
              await deps.rename(s, name).catch((e) => Promise.reject(new Error(t("conversation-menu.rename_failed", { error: errorText(e) }))));
              deps.notify(t("conversation-menu.renamed"), "ok");
              reread(deps);
            },
          },
        },
        {
          key: "archive",
          label: t("conversation-menu.archive"),
          icon: "archive",
          onSelect: async () => {
            try {
              await deps.archive(s);
              // Undo puts it back as it was (an open conversation stays open: its page reads the machine's sessions)
              deps.notify(t("conversation-menu.archived", { title }), "ok", { label: t("conversation-menu.undo"), run: () => void undoArchive(s, title, deps) });
            } catch (e) {
              deps.notify(errorText(e), "error");
            }
            reread(deps);
          },
        },
      ];
  return [
    ...own,
    { key: "copy", label: t("conversation-menu.copy_id"), icon: "copy", accel: "c", onSelect: () => void deps.copy(s.session_id) },
    ...(conversationLive(s)
      ? [
          {
            key: "end",
            label: t("conversation-menu.end"),
            icon: "stop" as const,
            danger: true,
            divider: true,
            closes: true,
            confirm: t("conversation-menu.end_confirm"),
            inline: true,
            onSelect: async () => {
              await deps.end(s).catch((e) => Promise.reject(new Error(t("conversation-menu.end_failed", { error: errorText(e) }))));
              deps.notify(t("conversation-menu.ended"), "ok");
              reread(deps);
            },
          },
        ]
      : []),
  ];
}

/** The menu of a row (`at`: where a right click opened it; without it, beside `anchor`). */
export function ConversationRowMenu({ s, main, pinned, leader, anchor, at, onClose }: { s: Session; main: boolean; pinned: boolean; leader?: boolean; anchor: HTMLElement; at?: MenuAt; onClose: () => void }) {
  const p = at ?? { anchor };
  return <Menu anchor={p.anchor ?? null} point={p.point} onClose={onClose} align="right" width={200} items={conversationMenuItems(s, { main, pinned, leader })} />;
}

/** The section title's menu: "Unpin all" when something is pinned. */
export function pinnedSectionItems(count: number): MenuItem[] {
  return count ? [{ key: "unpin-all", label: t("conversation-menu.unpin_all"), icon: "pin", onSelect: () => unpinAllConversations() }] : [];
}
