import { t } from "../i18n";
import { navigate } from "../lib/router";
import { Button, IconButton } from "../ui/atoms";

/** A deleted chat has no transcript or send controls, including when opened through an old link. */
export function DeletedConversation({ onToggleSidebar }: { onToggleSidebar?: () => void }) {
  return <div className="flex h-full flex-col">
    <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
      {onToggleSidebar && <IconButton icon="sidebar" label={t("assistant-history.sidebar")} onClick={onToggleSidebar} />}
      <h1 className="text-sm leading-normal font-semibold text-ink">{t("conversation-delete.deleted_title")}</h1>
    </header>
    <div role="status" className="flex flex-1 flex-col items-center justify-center gap-3 px-5 text-center">
      <p className="text-meta text-ink-2">{t("conversation-delete.deleted_body")}</p>
      <Button onClick={() => navigate({ name: "history" })}>{t("conversation-delete.all_chats")}</Button>
    </div>
  </div>;
}
