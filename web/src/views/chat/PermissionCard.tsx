// Tool approval is not part of the chat's first screen: load the complete card with the first permission interaction,
// as CommandScreenCard does. Its optional question editor is a further chunk loaded only when that action opens.
import { Suspense, useState } from "react";
import { t } from "../../i18n";
import { alwaysScope, offersAlways } from "../../lib/always-scope";
import { lazyPage } from "../../lib/lazy-page";
import { cardToolName } from "../../lib/turns";
import type { Interaction } from "../../lib/types";
import { Button, Kbd } from "../../ui/atoms";
import { useChatCardTapGuard } from "./card-tap-guard";
import { Card, PermissionBody, resolve, useCardKeys } from "./Interactions";
import { usePermissionText } from "./permission-text";

const ApprovalQuestion = lazyPage(() => import("./ApprovalQuestion").then((m) => m.ApprovalQuestion));

export function PermissionCard({ ix }: { ix: Interaction }) {
  const [busy, setBusy] = useState(false);
  const [questionOpen, setQuestionOpen] = useState(false);
  const editable = usePermissionText(ix);
  const go = async (body: Record<string, unknown>) => {
    setBusy(true);
    await resolve(ix, body);
    setBusy(false);
  };
  // Enter allows (the Allow button), except while the independent question
  // editor is open. Deny is never a key away.
  const keys = useCardKeys({
    enter: () => {
      if (busy || questionOpen || editable.composing || !editable.canAllow) return false;
      void go(editable.allow());
    },
  });
  const tapGuard = useChatCardTapGuard(ix);
  return (
    <Card
      kicker={t("chat-interactions.permission_kicker")}
      title={t("chat-interactions.permission_title", { tool: cardToolName(ix) })}
      cardKeys={keys}
      tapGuard={tapGuard}
      onDismiss={() => go({ dismiss: true })}
      busy={busy}
      footer={
        <>
          <span className="flex min-w-0 gap-1.5">
            {ix.can_clarify && (
              <Button tone="quiet" disabled={busy} aria-expanded={questionOpen} onClick={() => setQuestionOpen((v) => !v)}>
                {t("chat-interactions.have_question")}
              </Button>
            )}
            <Button tone="quiet" disabled={busy} onClick={() => go(editable.deny())}>
              {t("chat-interactions.deny")}
            </Button>
          </span>
          <span className="flex gap-1.5">
            {/* Only where an Always is kept (offersAlways): never on the manager's own cards. */}
            {offersAlways(ix) && !editable.changed && (
              <Button tone="secondary" disabled={busy} onClick={() => go({ allow: true, always: true })}>
                {t("chat-interactions.always_allow")}
              </Button>
            )}
            <Button tone="primary" busy={busy} disabled={!editable.canAllow} onClick={() => go(editable.allow())}>
              {t("chat-interactions.allow")}
              {keys.focused && !questionOpen && <Kbd>Enter</Kbd>}
            </Button>
          </span>
        </>
      }
    >
      <PermissionBody ix={ix} />
      {editable.editor}
      {questionOpen && <Suspense fallback={null}><ApprovalQuestion ix={ix} busy={busy} onClose={() => setQuestionOpen(false)} /></Suspense>}
      {alwaysScope(ix) && <p className="mt-2 text-label leading-relaxed text-ink-3">{alwaysScope(ix)}</p>}
    </Card>
  );
}
