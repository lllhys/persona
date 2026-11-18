// The quiet execution-catalog state shared by the @ panel and / palette.
import { t } from "../../i18n";
import type { SkillCatalogState } from "./useSkillCatalog";

export function SkillCatalogStatus({ status }: { status?: SkillCatalogState["status"] }) {
  if (!status) return null;
  return <div role="status" className="mt-1.5 shrink-0 border-t border-line px-[10px] pt-2 pb-1 text-xs text-ink-3">{t(`chat-composerpanel.skills_${status}`)}</div>;
}
