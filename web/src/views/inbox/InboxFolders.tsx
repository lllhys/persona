// Inbox folders (docs/assistant/inbox.md "Folders", #2463): the folder bar under the filters (crumbs down to the folder
// picked, the folders under it as chips), "Move to folder…" from an item's menu, and the folder settings from the ⋯
// menu (the model files items when on; the user's folder rules guide it).
import { useEffect, useId, useMemo, useState, useSyncExternalStore } from "react";
import { moveToFolder, useInbox, type InboxItem } from "../../lib/inbox";
import { childFolders, folderCrumbs, knownFolders, normalizeFolder, saveFolderSettings, unfiledCount, UNFILED, useFolderSettings } from "../../lib/inbox-folders";
import { errorText } from "../../lib/api";
import { composing } from "../../lib/send-history";
import { t } from "../../i18n";
import { Button, cx, Dialog, Switch, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Chip } from "./InboxParts";

/** Whether the list shows folders: they are on, or some item is filed (moved by hand, or before they were turned off). */
export function useFoldersShown(items: InboxItem[]): boolean {
  const settings = useFolderSettings();
  return Boolean(settings?.enabled) || items.some((it) => it.folder);
}

/** The folder bar: "All folders › work › alerts" (each level a link), then the folders right under the one picked
 *  with their open counts, and "Unfiled" at the top. `items` are the items the other filters leave. */
export function FolderBar({ items, folder, onPick }: { items: InboxItem[]; folder: string; onPick: (folder: string) => void }) {
  const unfiled = folder === UNFILED;
  const children = useMemo(() => (unfiled ? [] : childFolders(items, folder)), [items, folder, unfiled]);
  const none = useMemo(() => unfiledCount(items), [items]);
  const crumbs = folderCrumbs(folder);
  return (
    <nav aria-label={t("inbox-folders.bar")} className="mt-2.5" data-folder-bar>
      <ol className="flex min-w-0 flex-wrap items-center gap-0.5 text-[12px]">
        <li className="flex items-center">
          <button type="button" onClick={() => onPick("")} aria-current={!folder ? "page" : undefined}
            className={cx("inline-flex h-6 items-center gap-1.5 rounded-[6px] px-1.5 whitespace-nowrap transition-colors hover:bg-hover", !folder ? "font-medium text-ink" : "text-ink-3 hover:text-ink")}>
            <Icon name="folder" size={13} className="shrink-0" />
            {t("inbox-folders.all")}
          </button>
        </li>
        {unfiled && <Crumb name={t("inbox-folders.unfiled")} current onPick={() => onPick(UNFILED)} />}
        {crumbs.map((c, i) => <Crumb key={c.path} name={c.name} current={i === crumbs.length - 1} onPick={() => onPick(c.path)} />)}
      </ol>
      {(children.length > 0 || (!folder && none.count > 0)) && (
        <div role="toolbar" aria-label={t("inbox-folders.subfolders")} className="mt-1.5 flex flex-wrap items-center gap-1.5 max-sm:-my-1 max-sm:flex-nowrap max-sm:overflow-x-auto max-sm:py-1 max-sm:scrollbar-none max-sm:[&>*]:shrink-0">
          {children.map((c) => (
            <Chip key={c.path} on={false} onClick={() => onPick(c.path)} title={c.path}>
              <span className="inline-flex items-center gap-1.5">
                <Icon name="folder" size={12} className="shrink-0 text-ink-3" />
                {c.name}
                {c.open > 0 && <span className="text-ink-3 tabular-nums">{c.open}</span>}
              </span>
            </Chip>
          ))}
          {!folder && none.count > 0 && (
            <Chip on={false} onClick={() => onPick(UNFILED)}>
              <span className="inline-flex items-center gap-1.5 text-ink-3">
                {t("inbox-folders.unfiled")}
                {none.open > 0 && <span className="tabular-nums">{none.open}</span>}
              </span>
            </Chip>
          )}
        </div>
      )}
    </nav>
  );
}

function Crumb({ name, current, onPick }: { name: string; current: boolean; onPick: () => void }) {
  return (
    <li className="flex min-w-0 items-center">
      <Icon name="chevronRight" size={11} stroke={2.2} className="shrink-0 text-ink-3" />
      <button type="button" onClick={onPick} aria-current={current ? "page" : undefined}
        className={cx("h-6 min-w-0 truncate rounded-[6px] px-1.5 whitespace-nowrap transition-colors hover:bg-hover", current ? "font-medium text-ink" : "text-ink-3 hover:text-ink")}>
        {name}
      </button>
    </li>
  );
}

/** A row's folder, when the list is not already inside it: the path, its last level in front. */
export function FolderLine({ it, folder }: { it: InboxItem; folder: string }) {
  if (!it.folder || it.folder === folder) return null;
  return (
    <span className="mt-0.5 flex min-w-0 items-center gap-1 text-[11px] text-ink-3" title={it.folder} data-item-folder>
      <Icon name="folder" size={11} className="shrink-0" />
      <span className="truncate">{it.folder}</span>
    </span>
  );
}

// ── Move to folder… ─────────

let moving: InboxItem | null = null;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

/** Opens "Move to folder…" for an item (a row's menu, the item's page); the Inbox page's FolderDialogHost draws it. */
export function openFolderDialog(it: InboxItem) {
  moving = it;
  emit();
}

function closeFolderDialog() {
  moving = null;
  emit();
}

/** Mounted once on the Inbox page: "Move to folder…" when it is open. */
export function FolderDialogHost() {
  const it = useSyncExternalStore((l) => (listeners.add(l), () => void listeners.delete(l)), () => moving, () => moving);
  return it ? <MoveFolderDialog key={`${it.manager ?? ""}:${it.id}`} item={it} onClose={closeFolderDialog} /> : null;
}

/** The path field with the folders in use to pick from, "No folder" and "Let AI decide". */
export function MoveFolderDialog({ item, onClose }: { item: InboxItem; onClose: () => void }) {
  const items = useInbox((s) => s.items);
  const known = useMemo(() => knownFolders(items), [items]);
  const [text, setText] = useState(item.folder ?? "");
  const [busy, setBusy] = useState(false);
  const listId = useId();
  const path = normalizeFolder(text);
  const move = async (folder: string | null) => {
    setBusy(true);
    const ok = await moveToFolder(item, folder);
    setBusy(false);
    if (!ok) return;
    toast(folder === null ? t("inbox-folders.moved_auto") : folder ? t("inbox-folders.moved", { folder }) : t("inbox-folders.moved_none"), "ok");
    onClose();
  };
  const suggestions = known.filter((f) => f !== path && (!path || f.toLowerCase().includes(path.toLowerCase()))).slice(0, 8);
  return (
    <Dialog open onClose={onClose} title={t("inbox-folders.move_title")} width={420}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>{t("common.cancel")}</Button>
          <Button tone="primary" icon="folder" busy={busy} disabled={!path} onClick={() => void move(path)}>{t("inbox-folders.move")}</Button>
        </>
      }>
      <p className="mb-3 line-clamp-2 text-[12.5px] text-ink-2">{item.title}</p>
      <label className="mb-1.5 block text-[12px] font-medium text-ink-2" htmlFor={`${listId}-path`}>{t("inbox-folders.path")}</label>
      <input id={`${listId}-path`} value={text} autoFocus spellCheck={false}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && !composing(e.nativeEvent) && path && void move(path)}
        placeholder={t("inbox-folders.path_placeholder")}
        className="h-9 w-full rounded-control bg-surface px-3 text-[13px] text-ink shadow-btn outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)]" />
      <p className="mt-1.5 text-[11.5px] text-ink-3">{t("inbox-folders.path_hint")}</p>
      {suggestions.length > 0 && (
        <ul className="-mx-1 mt-2 flex flex-col" aria-label={t("inbox-folders.suggestions")}>
          {suggestions.map((f) => (
            <li key={f}>
              <button type="button" onClick={() => setText(f)} className="flex w-full items-center gap-2 rounded-control px-1.5 py-1.5 text-left text-[12.5px] text-ink hover:bg-hover">
                <Icon name="folder" size={13} className="shrink-0 text-ink-3" />
                <span className="min-w-0 truncate">{f}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="mt-3 flex flex-wrap gap-1.5 border-t border-line pt-3">
        <Button tone="secondary" icon="sparkles" disabled={busy || (!item.folder_by && !item.folder)} onClick={() => void move(null)}>{t("inbox-folders.auto")}</Button>
        <Button tone="secondary" icon="x" disabled={busy || (item.folder_by === "user" && !item.folder)} onClick={() => void move("")}>{t("inbox-folders.none")}</Button>
      </div>
    </Dialog>
  );
}

// ── Folders… (settings) ─────────

/** "Folders…" in the ⋯ menu: automatic folders on / off and the user's folder rules. */
export function FolderSettingsDialog({ onClose }: { onClose: () => void }) {
  const settings = useFolderSettings();
  const [enabled, setEnabled] = useState(false);
  const [rules, setRules] = useState("");
  const [busy, setBusy] = useState(false);
  const id = useId();
  useEffect(() => {
    if (!settings) return;
    setEnabled(settings.enabled);
    setRules(settings.rules);
  }, [settings]);
  const save = async () => {
    setBusy(true);
    try {
      await saveFolderSettings({ enabled, rules });
      toast(t("inbox-folders.saved"), "ok");
      onClose();
    } catch (e) {
      toast(t("inbox-folders.save_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onClose={onClose} title={t("inbox-folders.settings_title")} width={460}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>{t("common.cancel")}</Button>
          <Button tone="primary" busy={busy} disabled={!settings} onClick={() => void save()}>{t("common.save")}</Button>
        </>
      }>
      <div className="flex items-start gap-3">
        <span className="min-w-0 flex-1">
          <span className="block text-[13px] font-medium text-ink">{t("inbox-folders.auto_title")}</span>
          <span id={`${id}-about`} className="mt-0.5 block text-[12px] leading-relaxed text-ink-3">{t("inbox-folders.auto_about")}</span>
        </span>
        <Switch on={enabled} onChange={setEnabled} disabled={!settings} label={t("inbox-folders.auto_title")} describedBy={`${id}-about`} />
      </div>
      <label className="mt-4 mb-1.5 block text-[12px] font-medium text-ink-2" htmlFor={`${id}-rules`}>{t("inbox-folders.rules")}</label>
      <textarea id={`${id}-rules`} value={rules} onChange={(e) => setRules(e.target.value)} rows={4} disabled={!settings}
        placeholder={t("inbox-folders.rules_placeholder")}
        className="w-full resize-y rounded-control bg-surface px-3 py-2 text-[13px] leading-relaxed text-ink shadow-btn outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)]" />
      <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-3">{t("inbox-folders.rules_hint")}</p>
    </Dialog>
  );
}
