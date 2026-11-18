export interface CodexTakeoverPlan {
  running: boolean;
  can_resume: boolean;
  reason?: string;
  token: string;
  cwd: string;
  transcript_path: string;
  continuation_prompt: string;
}

/** A running terminal turn is never stopped until the user confirms in the dialog. */
export function takeoverBody(plan: Pick<CodexTakeoverPlan, "token" | "running">, confirmed: boolean) {
  if (plan.running && !confirmed) return null;
  return { token: plan.token, confirm_stop: plan.running && confirmed };
}

export function continuationLaunch(plan: CodexTakeoverPlan, machineId: string, todoId?: string) {
  return {
    machineId, cwd: plan.cwd, launcher: "codex", agentType: "codex", inlinePrompt: true,
    text: plan.continuation_prompt, attachments: [] as never[], todoId,
  };
}
