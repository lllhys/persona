// Changing a file's or folder's permissions in the Files panel (#5831; iOS FilePermissionsSheet): read, write and run
// for the owner, the group and everyone else; Save asks first and says what changes. Not for a Windows computer, nor
// where the computer takes no changes. docs/protocol/protocol.md "Changing files" (`files/chmod`).
import { useEffect, useState } from "react";
import { t } from "../../../i18n";
import { api, enc, errorText, isAbort } from "../../../lib/api";
import { opErrorText } from "../../../lib/file-ops";
import { fileName, type FileInfo } from "../../../lib/files";
import { modeOctal, modeSymbolic } from "../../../lib/files-nav";
import { Button, Dialog, Loading, toast } from "../../../ui/atoms";
import { accessWords } from "./FileInfo";

export const FILE_CHMOD_FEATURE = "file_chmod";

/** What changes between two modes, for whom: "Everyone else: Read only → Read & run". */
export function modeChangeWords(from: number, to: number): string {
  const who = [t("files-info.owner"), t("files-perm.group"), t("files-perm.others")];
  return [6, 3, 0]
    .map((shift, i) => (((from >> shift) & 7) === ((to >> shift) & 7) ? "" : `${who[i]}: ${accessWords((from >> shift) & 7)} → ${accessWords((to >> shift) & 7)}`))
    .filter(Boolean)
    .join(" · ");
}

export function PermissionsDialog({ mid, cwd, path, mode, onClose, onChanged }: {
  mid: string;
  cwd: string;
  path: string;
  /** The bits now, when known; else read here. */
  mode?: number;
  onClose: () => void;
  onChanged: (mode: number) => void;
}) {
  const [old, setOld] = useState<number | null>(mode === undefined ? null : mode & 0o777);
  const [bits, setBits] = useState(mode === undefined ? 0 : mode & 0o777);
  const [names, setNames] = useState<{ owner?: string; group?: string }>({});
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const ac = new AbortController();
    api<{ stats: FileInfo[] }>(`/machines/${enc(mid)}/stat?path=${enc(path)}&cwd=${enc(cwd)}&detail=1`, { signal: ac.signal })
      .then((r) => {
        const info = r.stats[0];
        setNames({ owner: info?.owner, group: info?.group });
        if (old === null && info?.mode !== undefined) {
          setOld(info.mode & 0o777);
          setBits(info.mode & 0o777);
        }
      })
      // An abort is this effect cleaning up (a closed dialog, or React mounting it twice in development): not an error.
      .catch((e) => !isAbort(e) && old === null && setError(errorText(e)));
    return () => ac.abort();
    // read once per path
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mid, cwd, path]);

  const save = async () => {
    setBusy(true);
    try {
      const r = await api<{ mode: number }>(`/machines/${enc(mid)}/files/chmod`, { method: "POST", json: { path, cwd, mode: bits } });
      toast(t("files-perm.changed"), "ok");
      onChanged(r.mode);
      onClose();
    } catch (e) {
      setAsking(false);
      setError(opErrorText(e));
    } finally {
      setBusy(false);
    }
  };

  const rows = [
    { title: t("files-info.owner"), detail: names.owner, shift: 6 },
    { title: t("files-perm.group"), detail: names.group, shift: 3 },
    { title: t("files-perm.others"), detail: undefined, shift: 0 },
  ];
  const cols = [t("files-perm.read"), t("files-perm.write"), t("files-perm.run")];
  return (
    <>
      <Dialog
        open={!asking}
        onClose={onClose}
        title={t("files-perm.title", { name: fileName(path) })}
        width={420}
        footer={
          <>
            <Button onClick={onClose}>{t("files-editor.cancel")}</Button>
            <Button tone="primary" disabled={old === null || old === bits} onClick={() => setAsking(true)}>{t("files-perm.change")}</Button>
          </>
        }
      >
        {old === null ? (
          error ? <p className="text-xs text-red">{error}</p> : <div className="flex h-24 items-center justify-center"><Loading label={t("files-info.loading")} /></div>
        ) : (
          <div className="flex flex-col gap-3" data-testid="permissions">
            <table className="w-full text-xs">
              <thead>
                <tr className="text-[11.5px] text-ink-3">
                  <th />
                  {cols.map((c) => <th key={c} className="pb-1 font-medium">{c}</th>)}
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.shift} className="border-t border-line">
                    <td className="py-2 pr-2 text-ink">
                      {r.title}
                      {r.detail && <span className="block text-[11.5px] text-ink-3">{r.detail}</span>}
                    </td>
                    {[4, 2, 1].map((bit, i) => {
                      const mask = bit << r.shift;
                      return (
                        <td key={bit} className="text-center">
                          <input
                            type="checkbox"
                            aria-label={`${r.title}: ${cols[i]}`}
                            checked={(bits & mask) !== 0}
                            onChange={(e) => setBits((b) => (e.target.checked ? b | mask : b & ~mask))}
                            className="size-4 accent-[var(--accent)]"
                          />
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
            <p className="font-mono text-xs text-ink">{modeSymbolic(bits)} · {modeOctal(bits)}{old !== bits && <span className="text-ink-3"> ({t("files-perm.before", { code: `${modeSymbolic(old)} · ${modeOctal(old)}` })})</span>}</p>
            <p className="text-xs text-ink-3">{t("files-perm.run_hint")}</p>
            {error && <p role="alert" className="text-xs text-red">{error}</p>}
          </div>
        )}
      </Dialog>
      <Dialog
        open={asking}
        onClose={() => setAsking(false)}
        title={t("files-perm.confirm_title", { name: fileName(path) })}
        footer={
          <>
            <Button onClick={() => setAsking(false)}>{t("files-editor.cancel")}</Button>
            <Button tone="primary" busy={busy} onClick={() => void save()}>{t("files-perm.change")}</Button>
          </>
        }
      >
        <p className="text-xs text-ink-2">{old !== null && modeChangeWords(old, bits)}</p>
        <p className="mt-2 font-mono text-xs text-ink-2">{old !== null && `${modeSymbolic(old)} (${modeOctal(old)}) → ${modeSymbolic(bits)} (${modeOctal(bits)})`}</p>
      </Dialog>
    </>
  );
}
