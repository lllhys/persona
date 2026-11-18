// The Files panel's New folder / New text file (#5828; iOS FilesPane's ＋): a name, then made in the folder shown, the
// new one marked in the list; a text file opens. A taken name is never replaced.
import { useState } from "react";
import { t } from "../../../i18n";
import { makeFolder, nameProblem, opErrorText, writeTextFile } from "../../../lib/file-ops";
import { joinPath } from "../../../lib/files";
import { Button, Dialog } from "../../../ui/atoms";

export type NewKind = "folder" | "text";

export function NewItemDialog({ mid, cwd, dir, folderName, kind, onClose, onMade }: {
  mid: string;
  cwd: string;
  /** Where it goes: the folder shown. */
  dir: string;
  folderName: string;
  kind: NewKind;
  onClose: () => void;
  /** Made: its path and kind. */
  onMade: (path: string, kind: NewKind) => void;
}) {
  const [name, setName] = useState(kind === "folder" ? t("files-new.untitled_folder") : t("files-new.untitled_text"));
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const create = async () => {
    const n = name.trim();
    const problem = nameProblem(n);
    if (problem) return setError(problem);
    setBusy(true);
    setError("");
    try {
      if (kind === "folder") await makeFolder(mid, dir, cwd, n);
      else await writeTextFile(mid, joinPath(dir, n), cwd, "", { create: true });
      onClose();
      onMade(joinPath(dir, n), kind);
    } catch (e) {
      setError(opErrorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      onClose={onClose}
      title={kind === "folder" ? t("files-new.new_folder") : t("files-new.new_text")}
      width={400}
      footer={
        <>
          <Button onClick={onClose}>{t("files-new.cancel")}</Button>
          <Button tone="primary" busy={busy} onClick={create}>{t("files-new.create")}</Button>
        </>
      }
    >
      <form className="flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); void create(); }}>
        <p className="text-xs text-ink-2">{kind === "folder" ? t("files-new.in_folder", { name: folderName }) : t("files-new.in_folder_opens", { name: folderName })}</p>
        <input
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          onFocus={(e) => {
            // the name before its extension, as Finder selects it
            const dot = kind === "text" ? e.target.value.lastIndexOf(".") : -1;
            e.target.setSelectionRange(0, dot > 0 ? dot : e.target.value.length);
          }}
          aria-label={t("files-new.name")}
          className="h-9 rounded-control bg-field px-3 text-xs text-ink outline-none focus:shadow-[inset_0_0_0_1px_var(--accent)]"
        />
        {error && <p role="alert" className="text-xs text-red">{error}</p>}
      </form>
    </Dialog>
  );
}
