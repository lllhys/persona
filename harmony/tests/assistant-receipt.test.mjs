import test from 'node:test';
import assert from 'node:assert/strict';
import {registerHooks, stripTypeScriptTypes} from 'node:module';
import {readFileSync} from 'node:fs';

registerHooks({
  resolve(specifier, context, next) {
    if (context.parentURL?.endsWith('.ets') && specifier.startsWith('.'))
      return {url: new URL(specifier + '.ets', context.parentURL).href, shortCircuit: true};
    return next(specifier, context);
  },
  load(url, context, next) {
    if (url.endsWith('.ets')) {
      const source = readFileSync(new URL(url), 'utf8').replace(/@ObservedV2\s*/g, '').replace(/@Trace\s+/g, '');
      return {format: 'module', source: stripTypeScriptTypes(source), shortCircuit: true};
    }
    return next(url, context);
  }
});

const {projectAssistantReceipt} = await import('../entry/src/main/ets/models/AssistantReceipt.ets');

function item(state = 'done', body = {}) {
  return {id: 'receipt-1', kind: 'receipt', ref: 'todo-1', body: {state, title: 'Receipt title', ...body}};
}

function todo(doneAt = '') {
  return {id: 'todo-1', origin: 'conversation-1', title: 'Current title', done_at: doneAt};
}

test('done receipts use only a successful todo read to distinguish done, stopped, and missing tasks', () => {
  assert.equal(projectAssistantReceipt(item(), todo('2026-11-14T00:00:00Z'), true).outcome, 'done');
  assert.equal(projectAssistantReceipt(item(), todo(), true).outcome, 'stopped');
  assert.equal(projectAssistantReceipt(item(), {}, false).outcome, 'unknown', 'a failed todo read cannot be guessed as stopped or done');
  assert.equal(projectAssistantReceipt(item(), {}, true).outcome, 'done', 'a successfully read missing todo stands on its receipt');
});

test('wire failure and interruption remain distinct, including legacy English and Chinese API error summaries', () => {
  assert.equal(projectAssistantReceipt(item('failed'), todo(), true).outcome, 'failed');
  assert.equal(projectAssistantReceipt(item('interrupted'), todo(), true).outcome, 'interrupted');
  const english = projectAssistantReceipt(item('done', {summary: 'API Error: 529 overloaded'}), todo(), true);
  assert.equal(english.outcome, 'interrupted');
  assert.equal(english.modelServiceInterruption, true);
  assert.equal(english.summary, 'API Error: 529 overloaded', 'the legacy wire text remains available below the localized explanation');
  const chinese = projectAssistantReceipt(item('done', {summary: 'API 错误：模型服务暂不可用'}), todo(), true);
  assert.equal(chinese.outcome, 'interrupted');
  assert.equal(chinese.modelServiceInterruption, true);
  assert.equal(projectAssistantReceipt(item('done', {summary: 'Gateway: API Error: 529'}), todo('2026-11-14T00:00:00Z'), true).outcome, 'done');
});

test('criteria count only met, expose only omitted count, and retain real PR and current task title', () => {
  const projection = projectAssistantReceipt(item('done', {
    pr: 'https://github.com/example/project/pull/12', criteria_more: 2,
    criteria: [
      {criterion: 'Build passes', state: 'met', evidence: 'release build'},
      {criterion: 'Review complete', state: 'not_met'},
      {criterion: 'Device checked', state: 'unchecked'},
      {criterion: 'No longer required', state: 'dropped'}
    ]
  }), todo('2026-11-14T00:00:00Z'), true);
  assert.equal(projection.title, 'Current title');
  assert.equal(projection.metCount, 1);
  assert.equal(projection.criteria.length, 4);
  assert.equal(projection.criteriaMore, 2);
  assert.equal(projection.pullRequest, 'https://github.com/example/project/pull/12');
  assert.deepEqual(projection.criteria.map((criterion) => criterion.state), ['met', 'not_met', 'unchecked', 'dropped']);
});

test('receipt recovery stays capability and lead-agent gated and never follows a completed task', () => {
  const failed = projectAssistantReceipt(item('failed'), todo(), true, {}, true, false, false);
  assert.equal(failed.recovery, '', 'a read-only surface exposes no write action');
  const retry = projectAssistantReceipt(item('failed'), todo(), true, {}, true, false, true);
  assert.equal(retry.recovery, 'retry');
  const stoppedAgent = {state: 'idle', turn_error: 'rate_limited'};
  assert.equal(projectAssistantReceipt(item('interrupted'), todo(), true, stoppedAgent, false, true, true).recovery, 'continue');
  assert.equal(projectAssistantReceipt(item('interrupted'), todo('2026-11-14T00:00:00Z'), true, stoppedAgent, false, true, true).recovery, '');
  assert.equal(projectAssistantReceipt(item('interrupted'), todo(), true, {}, false, true, true).recovery, '');
  const autoResumes = {state: 'idle', turn_error: 'rate_limited', turn_error_kind: 'usage_limit', turn_error_resumes: true};
  const projection = projectAssistantReceipt(item('interrupted'), todo(), true, autoResumes, false, false, true);
  assert.equal(projection.autoContinues, true);
  assert.equal(projection.recovery, '');
});

test('receipt resolution suppresses old actions while a later unresolved interruption stays actionable', () => {
  const stoppedAgent = {state: 'idle', turn_error: 'rate_limited'};
  const resumed = projectAssistantReceipt(item('interrupted', {resolution: 'resumed'}), todo(), true,
    stoppedAgent, false, true, true);
  assert.equal(resumed.continued, true);
  assert.equal(resumed.recovery, '', 'a resumed receipt cannot act on a later interruption');

  const pending = projectAssistantReceipt(item('interrupted', {resolution: 'pending'}), todo(), true,
    stoppedAgent, false, true, true);
  assert.equal(pending.recovery, 'continue');
  const unknownLead = projectAssistantReceipt(item('interrupted', {resolution: 'pending'}), todo(), true,
    {}, false, false, true);
  assert.equal(unknownLead.continued, false, 'an unlisted/missing session never counts as a continuation');
});

test('automatic usage-limit continuation uses the same active-reset grace as the session model', () => {
  const stoppedTodo = todo();
  const afterGrace = {
    state: 'idle', turn_error: 'rate_limited', turn_error_kind: 'usage_limit', turn_error_resumes: true,
    turn_error_resets_at: new Date(Date.now() - 11 * 60 * 1000).toISOString()
  };
  const expired = projectAssistantReceipt(item('interrupted', {resolution: 'pending'}), stoppedTodo, true,
    afterGrace, false, true, true);
  assert.equal(expired.autoContinues, true);
  assert.equal(expired.recovery, 'continue', 'expired usage waits are interrupted again after the ten-minute grace');

  const unknownReset = {...afterGrace};
  delete unknownReset.turn_error_resets_at;
  const stillWaiting = projectAssistantReceipt(item('interrupted', {resolution: 'pending'}), stoppedTodo, true,
    unknownReset, false, true, true);
  assert.equal(stillWaiting.recovery, '', 'an automatic usage wait with no known reset remains gated');

  const automaticNonLimit = {state: 'idle', turn_error: 'transient', turn_error_resumes: true};
  const interrupted = projectAssistantReceipt(item('interrupted', {resolution: 'pending'}), stoppedTodo, true,
    automaticNonLimit, false, true, true);
  assert.equal(interrupted.recovery, 'continue', 'only an active usage limit suppresses the manual recovery');
});


test('explicit terminal resolutions never inherit continued from a running lead agent',()=>{
 for(const resolution of ['cancelled','done','accepted','rerouted'])assert.equal(projectAssistantReceipt(item('interrupted',{resolution}),todo(),true,{state:'working'},false,false,true).continued,false);
 assert.equal(projectAssistantReceipt(item('interrupted',{resolution:'resumed'}),todo(),true,{},false,false,true).continued,true);
});
