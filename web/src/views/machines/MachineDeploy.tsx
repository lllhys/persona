import { useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { machineDeployment } from "../../lib/machine-deploy";
import { refresh } from "../../lib/store";
import type { Machine } from "../../lib/types";
import { Button, cx, toast } from "../../ui/atoms";
import { t } from "../../i18n";

/** The update action both the deployment strip and the node self-check use: the same request, the same capability checks. */
export function useDeploy(machine: Machine) {
  const [pending, setPending] = useState(false);
  // why the last request was refused (the alert sheet's stepper says it in place); "" when it went or none was made
  const [error, setError] = useState("");
  const deployment = machineDeployment(machine);

  const deploy = async () => {
    if (!deployment.canDeploy || pending) return false;
    setPending(true);
    setError("");
    try {
      await api(`/machines/${enc(machine.id)}/deploy`, { method: "POST" });
      await refresh().catch(() => {});
      return true;
    } catch (e) {
      setError(errorText(e));
      toast(t("machines-machinedeploy.failed", { error: errorText(e) }), "error");
      return false;
    } finally {
      setPending(false);
    }
  };
  return { deployment, pending, deploy, error };
}

/** The list and detail keep the same capability checks, stages and confirmation wording. */
/** `onDetails`: the Machines card's "Details", which opens the computer's alert sheet on the deployment. */
export function MachineDeploy({ machine, className, onDetails }: { machine: Machine; className?: string; onDetails?: () => void }) {
  const { deployment, pending, deploy } = useDeploy(machine);
  if (!deployment.visible) return null;

  return (
    <div className={cx("mx-4 flex items-center gap-2 rounded-[8px] bg-orange-tint py-1.5 pl-2.5 pr-1.5 text-[12px] text-orange", className)}>
      <div className="min-w-0 flex-1 break-words" role="status">
        {deployment.message && <p>{deployment.message}</p>}
        {deployment.error && <p>{deployment.error}</p>}
        {deployment.reason && !deployment.busy && <p>{deployment.reason}</p>}
      </div>
      {onDetails && (
        <Button tone="quiet" size="sm" onClick={onDetails} testId="deploy-details">
          {t("machines-machinedeploy.details")}
        </Button>
      )}
      {(deployment.supported || deployment.busy) && (
        <Button tone="secondary" icon="retry" busy={deployment.busy || pending} disabled={!deployment.canDeploy || pending} title={deployment.reason || undefined} onClick={() => void deploy()}>
          {pending && !deployment.busy ? t("machines-machinedeploy.requesting") : deployment.label}
        </Button>
      )}
    </div>
  );
}
