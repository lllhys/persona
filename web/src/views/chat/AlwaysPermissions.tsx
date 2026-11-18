// Remembered permission grants belong to this conversation; revoke only after the hub accepts the deletion.
import { useEffect, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { ago } from "../../lib/format";
import { t } from "../../i18n";
import { Button, Loading } from "../../ui/atoms";

interface Grant {
  key_hash: string;
  tool_name: string;
  created_at: string;
}

const restartKey = (session: string) => `persona.always-restart:${session}`;
function needsRestart(session: string) {
  try { return sessionStorage.getItem(restartKey(session)) === "1"; } catch { return false; }
}

export function AlwaysPermissions({ session }: { session: string }) {
  const [grants, setGrants] = useState<Grant[] | null>(null);
  const [error, setError] = useState("");
  const [pending, setPending] = useState("");
  const [revision, setRevision] = useState(0);
  const [restart, setRestart] = useState(() => needsRestart(session));
  const path = `/assistant/conversations/${enc(session)}/always`;
  useEffect(() => {
    let live = true;
    setGrants(null);
    setError("");
    api<{ grants: Grant[] }>(path)
      .then((r) => live && setGrants(r.grants))
      .catch((e) => live && setError(errorText(e)));
    return () => { live = false; };
  }, [path, revision]);

  const revoke = async (key: string) => {
    setPending(key);
    setError("");
    try {
      const result = await api<{ restart_required: boolean }>(`${path}/${enc(key)}`, { method: "DELETE" });
      setGrants((rows) => rows?.filter((row) => row.key_hash !== key) ?? null);
      if (result.restart_required) {
        setRestart(true);
        try { sessionStorage.setItem(restartKey(session), "1"); } catch { /* The visible warning still remains. */ }
      }
    } catch (e) {
      setError(errorText(e));
    } finally {
      setPending("");
    }
  };

  return (
    <section aria-label={t("chat-sessioninfo.always_title")}>
      <h3 className="mb-1 text-[11.5px] font-medium text-ink-3">{t("chat-sessioninfo.always_title")}</h3>
      <p className="mb-2 text-[12px] leading-relaxed text-ink-3">{t("chat-sessioninfo.always_hint")}</p>
      {restart && <p role="status" className="mb-2 text-[12px] text-attention">{t("chat-sessioninfo.always_restart")}</p>}
      {error && <p role="alert" className="mb-2 text-[12px] text-red">{error}</p>}
      {!grants && !error && <Loading />}
      {!grants && error && <Button tone="secondary" onClick={() => setRevision((n) => n + 1)}>{t("chat-sessioninfo.always_retry")}</Button>}
      {grants?.length === 0 && <p className="text-[12.5px] text-ink-2">{t("chat-sessioninfo.always_empty")}</p>}
      {!!grants?.length && (
        <>
          <p className="mb-2 text-[12px] leading-relaxed text-ink-3">{t("chat-sessioninfo.always_revoke_hint")}</p>
          <div className="divide-y divide-line overflow-hidden rounded-[10px] bg-inset shadow-hairline">
            {grants.map((grant) => (
              <div key={grant.key_hash} className="flex items-center gap-3 px-3 py-2">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-[12.5px] text-ink" title={grant.tool_name}>{grant.tool_name || t("chat-sessioninfo.always_unknown")}</p>
                  <p className="text-[11.5px] text-ink-3">{ago(grant.created_at)}</p>
                </div>
                <Button tone="quiet" busy={pending === grant.key_hash} disabled={!!pending} onClick={() => void revoke(grant.key_hash)}>
                  {t("chat-sessioninfo.always_revoke")}
                </Button>
              </div>
            ))}
          </div>
        </>
      )}
    </section>
  );
}
