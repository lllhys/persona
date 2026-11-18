import { Wire } from '../models/Wire';

export type AssistantReceiptOutcome = 'done' | 'stopped' | 'failed' | 'interrupted' | 'unknown';
export type AssistantReceiptRecovery = 'retry' | 'continue' | '';

export interface AssistantReceiptCriterion {
  criterion: string;
  state: string;
  evidence: string;
  change: string;
}

export interface AssistantReceiptProjection {
  outcome: AssistantReceiptOutcome;
  title: string;
  summary: string;
  error: string;
  modelServiceInterruption: boolean;
  pullRequest: string;
  criteria: AssistantReceiptCriterion[];
  criteriaMore: number;
  metCount: number;
  continued: boolean;
  autoContinues: boolean;
  recovery: AssistantReceiptRecovery;
}

function modelServiceError(value: string): boolean {
  const text = value.trim();
  // Both prefixes are exact strings emitted by the legacy Hub, including its Chinese wire copy.
  return text.startsWith('API Error:') || text.startsWith('API 错误：');
}

export function receiptWireState(item: Record<string, Object>): string {
  const body = Wire.object(item, 'body'), state = Wire.text(body, 'state');
  return state === 'done' && (modelServiceError(Wire.text(body, 'error')) || modelServiceError(Wire.text(body, 'summary'))) ? 'interrupted' : state;
}

function automaticContinuation(agent: Record<string, Object>): boolean {
  if (Wire.bool(agent, 'turn_error_resumes')) return true;
  const work = Wire.object(agent, 'work');
  return Wire.text(work, 'phase') === 'blocked' && !!Wire.text(work, 'resets_at') && !!Wire.text(work, 'wake_at');
}

function usageLimitIsActive(agent: Record<string, Object>, now: number = Date.now()): boolean {
  if (Wire.text(agent, 'turn_error_kind') !== 'usage_limit' || !Wire.text(agent, 'turn_error') ||
    !['idle', 'unread'].includes(Wire.text(agent, 'state', Wire.text(agent, 'status')))) return false;
  const work = Wire.object(agent, 'work');
  const raw = Wire.text(agent, 'turn_error_resets_at') || Wire.text(work, 'resets_at');
  if (!raw) return true;
  const resetAt = Date.parse(raw);
  return !Number.isFinite(resetAt) || now <= resetAt + 10 * 60 * 1000;
}

/** Projects only facts present in the receipt, the successfully loaded to-do, and its lead Agent. */
export function projectAssistantReceipt(item: Record<string, Object>, todo: Record<string, Object>, todoReadSucceeded: boolean,
  leadAgent: Record<string, Object> = {}, canRetry: boolean = false, canContinue: boolean = false,
  writable: boolean = false): AssistantReceiptProjection {
  const body = Wire.object(item, 'body');
  const wireState = Wire.text(body, 'state');
  const summary = Wire.text(body, 'summary');
  const error = Wire.text(body, 'error');
  const resolution = Wire.text(body, 'resolution');
  const legacyInterrupted = wireState === 'done' && (modelServiceError(error) || modelServiceError(summary));
  let outcome: AssistantReceiptOutcome = 'unknown';

  if (wireState === 'failed') outcome = 'failed';
  else if (receiptWireState(item) === 'interrupted') outcome = 'interrupted';
  else if (wireState === 'done') {
    if (!todoReadSucceeded) outcome = 'unknown';
    else if (Object.keys(todo).length > 0 && !Wire.text(todo, 'done_at')) outcome = 'stopped';
    else outcome = 'done';
  }

  const criteria: AssistantReceiptCriterion[] = [];
  Wire.rows(body, 'criteria').forEach((row: Record<string, Object>) => {
    const state = Wire.text(row, 'state');
    const criterion: AssistantReceiptCriterion = {
      criterion: Wire.text(row, 'criterion'),
      state: ['met', 'not_met', 'unchecked', 'dropped'].includes(state) ? state : 'unknown',
      evidence: Wire.text(row, 'evidence'),
      change: Wire.text(row, 'change')
    };
    if (criterion.criterion) criteria.push(criterion);
  });
  const criteriaMore = Wire.number(body, 'criteria_more');
  const agentState = Wire.text(leadAgent, 'state', Wire.text(leadAgent, 'status'));
  const agentError = Wire.text(leadAgent, 'turn_error');
  const stoppedAgent = !!Object.keys(leadAgent).length && !!agentError && ['idle', 'unread'].includes(agentState);
  const autoContinues = automaticContinuation(leadAgent);
  const continued = outcome === 'interrupted' && (resolution === 'resumed' || !resolution && todoReadSucceeded && Object.keys(todo).length > 0 &&
    Object.keys(leadAgent).length > 0 && !stoppedAgent);
  const recoveryUnresolved = resolution === '' || resolution === 'pending';
  const automaticUsageWait = autoContinues && usageLimitIsActive(leadAgent);
  let recovery: AssistantReceiptRecovery = '';
  if (writable && todoReadSucceeded && Object.keys(todo).length > 0 && recoveryUnresolved) {
    if (outcome === 'failed' && canRetry) recovery = 'retry';
    else if (outcome === 'interrupted' && !Wire.text(todo, 'done_at') && canContinue && stoppedAgent && !automaticUsageWait) recovery = 'continue';
  }

  return {
    outcome,
    title: Wire.text(todo, 'title').trim() || Wire.text(body, 'title'),
    summary,
    error,
    modelServiceInterruption: legacyInterrupted,
    pullRequest: Wire.text(body, 'pr', Wire.text(todo, 'pr_url')),
    criteria,
    criteriaMore: Number.isSafeInteger(criteriaMore) && criteriaMore > 0 ? criteriaMore : 0,
    metCount: criteria.filter((row: AssistantReceiptCriterion) => row.state === 'met').length,
    continued,
    autoContinues,
    recovery
  };
}
