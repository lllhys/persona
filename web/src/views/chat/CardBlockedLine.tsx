// Under a card this page can't answer (#7497): why, and the one way to answer it here where there is one (Live link,
// the ⋯ menu's action). Once linked, the session's new row can approve, the card answers and this line goes. Off the
// first screen and the idle tier: Interactions.tsx loads it (lazyPage) with the first card that is blocked, its strings
// (`chat-cardblocked`) in the rest of the catalog.
import { useState } from "react";
import { t } from "../../i18n";
import { attachLive } from "../../lib/attach-live";
import type { Interaction } from "../../lib/types";
import { Button } from "../../ui/atoms";

export function CardBlockedLine({ ix, block }: { ix: Interaction; block: "attach" | "computer" }) {
  const [linking, setLinking] = useState(false);
  const link = async () => {
    setLinking(true);
    try {
      await attachLive(ix);
    } finally {
      setLinking(false);
    }
  };
  return (
    <div data-card-blocked={block} role="status" className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 px-1 text-xs text-attention">
      <p className="min-w-0 flex-1 basis-64 [overflow-wrap:anywhere]">{t(block === "attach" ? "chat-cardblocked.attach" : "chat-cardblocked.computer")}</p>
      {block === "attach" && (
        <Button size="sm" icon="link" busy={linking} disabled={linking} onClick={() => void link()}>
          {t("chat-chatview.attach")}
        </Button>
      )}
    </div>
  );
}
