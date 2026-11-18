// The Files panel's Info dialog (#5827; iOS FileInfoSheet): what a file or folder is, where, how big, its times, who may
// do what with it, and for a symlink what it says and where it leads (`/stat?detail=1`, docs/protocol/protocol.md "File
// details and links"). No permissions for a Windows computer, which has none of these bits.
import { useEffect, useState } from "react";
import { api, enc, errorText, isAbort } from "../../../lib/api";
import { dirName, fileName, type FileInfo, type FsEntry } from "../../../lib/files";
import { modeOctal, modeSymbolic, tildePath } from "../../../lib/files-nav";
import { bytes } from "../../../lib/format";
import { hubHas, useStore } from "../../../lib/store";
import { formatDate, formatNumber, t } from "../../../i18n";
import { Button, copyText, Dialog, Loading } from "../../../ui/atoms";

export const FILE_INFO_FEATURE = "file_info";

/** What one class (owner, group, others) may do, in words. */
export function accessWords(bits: number): string {
  return t(`files-info.access_${bits & 7}`);
}

/** "Owner: Read & Write · Everyone else: Read Only" (the group named only when it differs from everyone else). */
export function permissionWords(mode: number): string {
  const owner = (mode >> 6) & 7,
    group = (mode >> 3) & 7,
    others = mode & 7;
  return group === others
    ? t("files-info.words_short", { owner: accessWords(owner), others: accessWords(others) })
    : t("files-info.words_long", { owner: accessWords(owner), group: accessWords(group), others: accessWords(others) });
}

function when(iso?: string): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : formatDate(d, { dateStyle: "medium", timeStyle: "short" });
}

export function FileInfoDialog({
  mid,
  cwd,
  path,
  entry,
  onClose,
  onGoTo,
  onChangeMode,
}: {
  mid: string;
  cwd: string;
  path: string;
  /** What the listing said, shown until the details are here (and instead of them from an older hub or node). */
  entry?: FsEntry;
  onClose: () => void;
  /** Go to where a link leads: a folder opens, a file is shown in its folder. */
  onGoTo?: (path: string, dir: boolean) => void;
  /** Change the permissions (#5831): offered where the computer takes them. */
  onChangeMode?: (mode: number) => void;
}) {
  const features = useStore((s) => s.features);
  const machine = useStore((s) => s.machines.find((m) => m.id === mid));
  const home = machine?.home;
  const windows = machine?.os?.toLowerCase() === "windows";
  const hubHasInfo = hubHas(features, FILE_INFO_FEATURE);
  const [info, setInfo] = useState<FileInfo | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(hubHasInfo);

  useEffect(() => {
    if (!hubHasInfo) return;
    const ac = new AbortController();
    api<{ stats: FileInfo[] }>(`/machines/${enc(mid)}/stat?path=${enc(path)}&cwd=${enc(cwd)}&detail=1`, { signal: ac.signal })
      .then((r) => setInfo(r.stats[0] ?? null))
      .catch((e) => !isAbort(e) && setError(errorText(e)))
      .finally(() => setLoading(false));
    return () => ac.abort();
  }, [mid, cwd, path, hubHasInfo]);

  const name = fileName(path);
  const isDir = info ? info.kind === "dir" : Boolean(entry?.dir);
  const symlink = info?.symlink ?? entry?.symlink;
  const broken = info?.link_broken ?? entry?.link_broken;
  const says = info?.link_target ?? entry?.link_target;
  const detailed = info ? info.mode !== undefined || info.mtime !== undefined : false;
  const size = info?.size ?? entry?.size;
  const kind = broken ? t("files-info.kind_broken") : symlink ? (isDir ? t("files-info.kind_link_folder") : t("files-info.kind_link_file")) : isDir ? t("files-info.kind_folder") : t("files-info.kind_file");
  const note = error
    ? error
    : !hubHasInfo
      ? t("files-info.update_hub")
      : info?.kind === "missing"
        ? t("files-info.missing")
        : info && !detailed
          ? t("files-info.update_node", { name: machine?.name || mid })
          : "";

  const rows: [string, string, boolean?][] = [];
  rows.push([t("files-info.location"), tildePath(dirName(path) || "/", home), true]);
  if (isDir) {
    if (info?.items !== undefined) rows.push([t("files-info.contents"), info.items_more ? t("files-info.items_more", { count: info.items }) : t("files-info.items", { count: info.items })]);
  } else if (size !== undefined) {
    rows.push([t("files-info.size"), size >= 1024 ? t("files-info.size_bytes", { size: bytes(size), bytes: formatNumber(size) }) : t("files-info.bytes", { bytes: formatNumber(size) })]);
  }
  const created = when(info?.birthtime),
    modified = when(info?.mtime ?? entry?.mtime),
    changed = when(info?.ctime);
  if (created) rows.push([t("files-info.created"), created]);
  if (modified) rows.push([t("files-info.modified"), modified]);
  if (changed) rows.push([t("files-info.changed"), changed]);

  return (
    <Dialog open onClose={onClose} title={t("files-info.title", { name })} width={460} footer={
      <>
        <Button icon="copy" onClick={() => copyText(path)}>{t("files-info.copy_path")}</Button>
        <Button tone="primary" onClick={onClose}>{t("files-info.done")}</Button>
      </>
    }>
      <div className="flex flex-col gap-3 text-xs" data-testid="file-info">
        <p className="text-ink-2">{kind}</p>
        {symlink && says !== undefined && (
          <Group title={t("files-info.link")}>
            <Row label={t("files-info.link_says")} value={says} mono />
            {broken ? (
              <p className="px-3 py-2 text-red">{t("files-info.link_missing")}</p>
            ) : info?.link_real ? (
              <>
                <Row label={t("files-info.real_location")} value={tildePath(info.link_real, home)} mono />
                {onGoTo && (
                  <div className="px-3 py-2">
                    <Button icon="arrowUpRight" onClick={() => { onClose(); onGoTo(info.link_real!, isDir); }}>{t("files-info.go_real")}</Button>
                  </div>
                )}
              </>
            ) : null}
          </Group>
        )}
        <Group>
          {rows.map(([label, value, mono]) => <Row key={label} label={label} value={value} mono={mono} />)}
        </Group>
        {!windows && info?.mode !== undefined && (
          <Group title={t("files-info.permissions")}>
            <Row label={t("files-info.who")} value={permissionWords(info.mode)} />
            {info.owner && <Row label={t("files-info.owner")} value={info.group ? t("files-info.owner_group", { owner: info.owner, group: info.group }) : info.owner} />}
            <Row label={t("files-info.code")} value={`${modeSymbolic(info.mode)} · ${modeOctal(info.mode)}`} mono />
            {onChangeMode && !info.symlink && (
              <div className="px-3 py-2">
                <Button icon="pencil" onClick={() => onChangeMode(info.mode!)}>{t("files-perm.change")}</Button>
              </div>
            )}
          </Group>
        )}
        {loading && <Loading label={t("files-info.loading")} />}
        {note && <p className="text-xs text-ink-3">{note}</p>}
      </div>
    </Dialog>
  );
}

function Group({ title, children }: { title?: string; children: React.ReactNode }) {
  return (
    <section>
      {title && <h3 className="mb-1 px-1 text-[11.5px] font-medium text-ink-3">{title}</h3>}
      <div className="divide-y divide-line overflow-hidden rounded-card border border-line">{children}</div>
    </section>
  );
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-4 px-3 py-2">
      <span className="shrink-0 text-ink-2">{label}</span>
      <span className={mono ? "min-w-0 text-right font-mono text-xs break-all text-ink" : "min-w-0 text-right text-ink"}>{value}</span>
    </div>
  );
}
