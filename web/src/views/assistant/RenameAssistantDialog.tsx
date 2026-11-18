// The assistant's rename dialog, loaded only on its first open (lazyDialog), so it stays out of the first-screen
// bundle (#6401). The rename itself goes through PATCH /managers/{id}, which the hub keeps for every device.
import { useEffect, useState } from "react";
import { errorText } from "../../lib/api";
import { NAME_MAX, renameManager, validName, type Manager } from "../../lib/managers";
import { Button, Dialog, toast } from "../../ui/atoms";
import { t } from "../../i18n";

export function RenameAssistantDialog({
  open,
  onClose,
  manager,
  currentName,
}: {
  open: boolean;
  onClose: () => void;
  manager: Manager | null;
  currentName: string;
}) {
  const [draft, setDraft] = useState(currentName);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (open) setDraft(currentName);
  }, [open, currentName]);
  const ok = validName(draft);
  const save = async () => {
    if (!manager || !ok || busy) return;
    setBusy(true);
    try {
      await renameManager(manager.id, draft.trim());
      toast(t("assistant-profile.name_saved"), "ok");
      onClose();
    } catch (e) {
      toast(t("assistant-profile.name_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={() => !busy && onClose()}
      title={t("assistant-profile.name_dialog_title")}
      footer={
        <>
          <Button tone="quiet" disabled={busy} onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button tone="primary" busy={busy} disabled={!ok || draft.trim() === currentName} onClick={() => void save()}>
            {t("settings-managers.save")}
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <input
          autoFocus
          value={draft}
          maxLength={NAME_MAX * 2}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t("assistant-profile.name_placeholder")}
          aria-label={t("settings-managers.name")}
          className="w-full rounded-control bg-inset px-3 py-2 text-sm leading-5 text-ink shadow-hairline outline-none focus:ring-2 focus:ring-accent"
        />
      </form>
    </Dialog>
  );
}
