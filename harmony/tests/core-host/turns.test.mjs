// Replays the unchanged turn corpus and its checked-in current-iOS overlay through persona-core-ohos's host helper.
// Static projections and generated-ArkTS-codec runtime dispatch/drain meet the real Rust core without loading a NAPI
// addon. This is not #8808's real NAPI boundary, a production adapter, a HAP/HDC run or physical-device evidence.
// The runtime-operation replay is not an incremental ChatStore rebuild or timing proof.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { ReplayedChat, startCore } from './host.mjs';

const fixtures = fileURLToPath(new URL('../../../spec/fixtures/lines/', import.meta.url));
const files = readdirSync(fixtures).filter((file) => file.endsWith('.json')).sort();
assert.equal(files.length, 9, 'the lines file set is the manifest boundary');
const cases = files.flatMap((file) => JSON.parse(readFileSync(fixtures + file, 'utf8')).cases
  .map((c) => ({file, ...c})));
const byName = new Map(cases.map((c) => [c.name, c]));
assert.equal(byName.size, cases.length, 'no duplicate transcript case names');
const overlay = JSON.parse(readFileSync(fixtures + 'ios/differences.json', 'utf8'));
const wake = overlay.turn_wake_state;
const patchNames = new Set(overlay.cases.map((entry) => entry.name));
assert.equal(patchNames.size, overlay.cases.length, 'no duplicate overlay cases');
for (const name of patchNames) assert.ok(byName.has(name), 'overlay names an existing case: ' + name);

const expectedStaticCases = [
  'absorbed_user_images', 'background_running_badge', 'closed_work_identity',
  'compaction_stays_inside_work', 'empty_session', 'empty_thinking_between_tools_is_retained',
  'failed_tool_counts_known_failure', 'fold_counts_tools_and_thoughts',
  'frozen_snapshot_excludes_newer_items', 'frozen_uses_numeric_block_order', 'interrupt_is_trailing_notice',
  'last_result_for_call_wins', 'late_result_attaches_to_original_call', 'live_answer_after_wake',
  'live_empty_work', 'live_queued_message_keeps_position', 'live_reply_stays_after_work',
  'live_tail_runs_behind_chip', 'long_answer_after_wake', 'menu_owner_per_message',
  'missing_start_time_hides_duration', 'missing_start_time_uses_first_step', 'missing_tool_result_is_running',
  'monitor_event_uses_first_detail_line', 'multiple_interactions_in_footer',
  'multiple_wakes_pick_latest_answer', 'notification_without_wake',
  'older_page_stays_visible_during_freeze', 'only_last_reply_in_each_wake_is_candidate',
  'open_queued_messages_work', 'open_work_identity', 'orphan_assistant', 'orphan_tool_result_is_not_a_row',
  'orphan_wake_opens_with_notice', 'ordinary_user_starts_next_turn', 'photos_after_wake_stay_with_message',
  'plain_exchange', 'queued_without_existing_turn', 'recap_separates_turns', 'regular_user_images',
  'short_reply_after_wake_is_the_answer', 'single_interaction_in_footer', 'standalone_empty_thinking_hidden',
  'task_notices_find_calls', 'task_status_outcomes', 'task_title_containing_interrupt_is_not_interrupt',
  'task_without_loaded_call_uses_title', 'thinking_before_between_after_tools',
  'thinking_narration_preserved', 'three_users_apart_without_steps', 'timed_work_ends_at_answer',
  'tool_images_belong_to_call', 'trailing_tool_after_answer', 'unknown_item_is_ignored',
  'wake_after_recap_reopens_turn', 'wake_without_any_reply', 'finished_queued_messages_apart',
];
const expectedOperationCases = [
  'background_bursts_and_replacement', 'fold_replaces_extends_and_unfolds',
  'incremental_with_late_result_and_older_page', 'outside_fold_tool_keeps_late_result',
];
const staticCases = cases.filter((c) => !Object.hasOwn(c.input, 'operations'));
const operationCases = cases.filter((c) => Object.hasOwn(c.input, 'operations'));
assert.equal(cases.length, 61, '57 static cases plus four operation cases');
assert.deepEqual(staticCases.map((c) => c.name).sort(), [...expectedStaticCases].sort(), 'the exact static manifest');
assert.deepEqual(operationCases.map((c) => c.name).sort(), [...expectedOperationCases].sort(), 'the exact operation manifest');
assert.deepEqual([...patchNames].filter((name) => !byName.has(name)), [], 'no dangling overlay case');
const emptyOnlyCases = ['empty_session', 'multiple_interactions_in_footer', 'single_interaction_in_footer'];
const allTurnIds = new Set();
for (const c of cases) {
  for (const state of c.expected.checkpoints ?? [c.expected]) {
    for (const turn of state.turns ?? []) allTurnIds.add(c.name + '/' + turn.id);
  }
}
const pendingIds = new Set();
for (const reference of wake.pending) {
  const key = reference.case + '/' + reference.turn;
  assert.ok(!pendingIds.has(key), 'no duplicate pending overlay reference: ' + key);
  assert.ok(allTurnIds.has(key), 'pending overlay names an existing turn: ' + key);
  pendingIds.add(key);
}

// Apply only the checked-in Apple-observation overlay. Both expected and actual values are parsed JSON, so object key
// order cannot hide a difference; array order, every field and every scalar remain significant.
function expectedTurns(c, state = c.expected, applyPatch = true) {
  const expected = structuredClone(state);
  assert.ok(Array.isArray(expected.turns), c.name + ': fixture carries a complete turns array');
  expected.turns = expected.turns.map((original) => {
    const turn = {...original};
    assert.ok(Object.hasOwn(turn, wake.drop), c.name + ': stale wake drop field');
    delete turn[wake.drop];
    assert.ok(!Object.hasOwn(turn, wake.set), c.name + ': stale wake set field');
    turn[wake.set] = pendingIds.has(c.name + '/' + turn.id);
    return turn;
  });
  if (applyPatch) {
    for (const entry of overlay.cases.filter((entry) => entry.name === c.name)) {
      assert.deepEqual(Object.keys(entry).sort(), ['name', 'why', 'source', 'replace', 'turn_fields'].filter((key) => Object.hasOwn(entry, key)).sort(),
        c.name + ': only documented overlay fields');
      assert.ok(entry.why.length > 0 && entry.source.length > 0, c.name + ': overlay rationale and sources');
      for (const [key, replacement] of Object.entries(entry.replace ?? {})) {
        assert.notDeepEqual(expected[key], replacement, c.name + ': stale replacement ' + key);
        expected[key] = replacement;
      }
      for (const [id, fields] of Object.entries(entry.turn_fields ?? {})) {
        assert.equal(expected.turns.filter((turn) => turn.id === id).length, 1, c.name + ': overlay names one turn ' + id);
        expected.turns = expected.turns.map((original) => {
          const turn = {...original};
          if (turn.id === id) {
            for (const [key, replacement] of Object.entries(fields)) {
              assert.notDeepEqual(turn[key], replacement, c.name + '/' + id + ': stale field ' + key);
              turn[key] = replacement;
            }
          }
          return turn;
        });
      }
    }
  }
  return expected.turns;
}

function compareTurns(label, actual, expected) {
  assert.deepEqual(actual, expected, label + ': complete post-overlay turns array');
}
function wrongTurns(actual) {
  // Deliberately wrong in both the nonempty and empty directions: a comparator that always succeeds cannot pass.
  const wrong = actual.length === 0 ? [{id: 'deliberately-wrong'}] : [];
  assert.notDeepEqual(actual, wrong, 'the wrong expectation is observably different');
  return wrong;
}
function itemIds(items) {
  return items.map((json) => JSON.parse(json).id);
}
function projectionInput(c, operation, items) {
  const input = Object.fromEntries(Object.entries(c.input).filter(([key]) => key !== 'operations'));
  for (const key of ['open_work', 'live_turn_id']) {
    if (Object.hasOwn(operation, key)) input[key] = operation[key];
  }
  input.items = items.map((json) => JSON.parse(json));
  return input;
}
function batch(items) {
  return {items_json: JSON.stringify(items), history: null, from_seq: null, to_seq: null,
    backlog: false, has_more: null, more_after: false};
}
async function settle(replay) {
  const {requests} = await replay.settle();
  return requests;
}

// This is the same existing operation-to-runtime mapping as #8815, but every action, patch and effect crosses the
// generated ArkTS codec. A fold response is supplied only after the real runtime has requested its original range.
async function replayOperation(replay, operation) {
  const name = operation.op;
  const items = operation.items ?? [];
  const chat = replay.chat;
  if (name === 'merge') {
    await replay.dispatch({kind: 'chat', chat, event: {kind: 'pushed', batch: batch(items)}});
    assert.deepEqual(await settle(replay), [], name + ': merge needs no fetch');
  } else if (name === 'replace') {
    const seq = items.at(-1).seq;
    await replay.dispatch({kind: 'chat', chat, event: {kind: 'reloaded', items_json: JSON.stringify(items),
      history: 'replacement', to_seq: seq, has_more: false}});
    assert.deepEqual(await settle(replay), [], name + ': replacement needs no fetch');
  } else if (name === 'unfold') {
    const foldId = operation.fold_id;
    const fold = replay.list.map((json) => JSON.parse(json)).find((item) => item.id === foldId);
    assert.ok(fold, name + ': fold exists before it opens');
    await replay.dispatch({kind: 'chat', chat, event: {kind: 'open_fold', fold_id: foldId}});
    assert.deepEqual(await settle(replay), [{kind: 'fetch_range', fold_id: foldId,
      from_seq: fold.seq, through_seq: fold.fold.through_seq}],
    name + ': the runtime requests the original fold identity and range');
    await replay.dispatch({kind: 'chat', chat, event: {kind: 'range', fold_id: foldId,
      items_json: JSON.stringify(items), more: false}});
    assert.deepEqual(await settle(replay), [], name + ': supplied range needs no more fetches');
  } else if (name === 'burst_replace') {
    for (const items of operation.batches) {
      await replay.dispatch({kind: 'chat', chat, event: {kind: 'pushed', batch: batch(items)}});
    }
    assert.deepEqual(await settle(replay), [], name + ': accumulated background batches need no fetch');
    const seq = items.at(-1).seq;
    await replay.dispatch({kind: 'chat', chat, event: {kind: 'reloaded', items_json: JSON.stringify(items),
      history: 'replacement', to_seq: seq, has_more: false}});
    assert.deepEqual(await settle(replay), [], name + ': final replacement needs no fetch');
  } else {
    assert.fail('unknown existing operation: ' + name);
  }
}

async function withHost(context, run) {
  const host = startCore();
  // A node:test timeout does not unwind a pending helper read, so explicitly release the helper and its stdio.
  context.after(() => host.kill(), {timeout: 1_000});
  try {
    await run(host);
  } finally {
    try {
      await host.end();
    } finally {
      host.kill();
    }
  }
}
const options = {timeout: 60_000};

test('ArkTS host replays all 57 static cases and the complete iOS-overlay turns arrays', options, async (context) => {
  await withHost(context, async (host) => {
    let turnCount = 0;
    const empty = [];
    for (const c of staticCases) {
      const actual = await host.projection('turns', c.input);
      compareTurns(c.file + '/' + c.name, actual, expectedTurns(c));
      if (patchNames.has(c.name)) assert.notDeepEqual(actual, expectedTurns(c, c.expected, false), c.name + ': overlay is necessary');
      if (emptyOnlyCases.includes(c.name)) {
        assert.deepEqual(actual, [], c.name + ': empty turns only, not layout coverage');
        empty.push(c.name);
      } else {
        assert.ok(actual.length > 0, c.name + ': nonempty turn-grouping coverage');
      }
      turnCount += actual.length;
    }
    assert.deepEqual(empty.sort(), emptyOnlyCases, 'exactly three empty-array cases');
    assert.equal(turnCount, 73, '54 nonempty cases hold 73 complete turn objects');
    console.log('  turns ArkTS host replay: 57 cases; 54 with 73 complete turn objects; 3 empty arrays');
  });
});

test('ArkTS codec dispatch/drain replays all 10 existing operation checkpoints', options, async (context) => {
  await withHost(context, async (host) => {
    const config = await host.coreConfig();
    let checkpoints = 0;
    let turnCount = 0;
    let folds = 0;
    const observed = new Set();
    for (const c of operationCases) {
      assert.equal(c.input.operations.length, c.expected.checkpoints.length, c.name + ': one expectation per operation');
      const chat = 'fixture/turn-operations/' + c.name;
      const handle = await host.coreCreate(config);
      const replay = new ReplayedChat(host, handle, chat, c.name);
      await replay.dispatch({kind: 'open_chat', chat, snapshot: {items_json: JSON.stringify(c.input.items),
        history: null, covered: 0, has_more: null, loaded_once: false}});
      await settle(replay);
      for (const [index, operation] of c.input.operations.entries()) {
        observed.add(operation.op);
        await replayOperation(replay, operation);
        if (operation.op === 'unfold') folds++;
        const state = c.expected.checkpoints[index];
        const label = c.name + ' checkpoint ' + index;
        assert.deepEqual(itemIds(replay.list), state.item_ids, label + ': complete item IDs');
        const actual = await host.projection('turns', projectionInput(c, operation, replay.list));
        compareTurns(label, actual, expectedTurns(c, state));
        if (patchNames.has(c.name)) assert.notDeepEqual(actual, expectedTurns(c, state, false), label + ': overlay is necessary');
        checkpoints++;
        turnCount += actual.length;
      }
      await host.coreClose(handle);
      assert.deepEqual(host.takeCallbacks(handle), [], c.name + ': no callback after close');
    }
    assert.deepEqual([...observed].sort(), ['burst_replace', 'merge', 'unfold']);
    assert.equal(folds, 1, 'the existing fold operation checks its fold ID');
    assert.equal(checkpoints, 10, 'five fold operations, three incremental merges and two one-step cases');
    assert.equal(turnCount, 13, 'one turn per checkpoint except incremental checkpoints two and three');
    console.log('  turns ArkTS operation replay: 4 cases; 10 checkpoints; 13 complete turn objects');
  });
});

test('ArkTS runtime replays the existing burst as a standalone replacement', options, async (context) => {
  await withHost(context, async (host) => {
    const c = byName.get('background_bursts_and_replacement');
    const burst = c.input.operations[0];
    assert.equal(burst.op, 'burst_replace', 'reuse the existing burst without a new fixture');
    const replacement = {op: 'replace', items: burst.items};
    const chat = 'fixture/turn-operations/standalone-replacement';
    const handle = await host.coreCreate(await host.coreConfig());
    const replay = new ReplayedChat(host, handle, chat, c.name + ' standalone replace');
    await replay.dispatch({kind: 'open_chat', chat, snapshot: {items_json: '[]', history: null,
      covered: 0, has_more: null, loaded_once: false}});
    await settle(replay);
    await replayOperation(replay, replacement);
    const state = c.expected.checkpoints[0];
    assert.deepEqual(itemIds(replay.list), state.item_ids, 'replacement item IDs');
    const actual = await host.projection('turns', {items: replay.list.map((json) => JSON.parse(json))});
    compareTurns(c.name + ' standalone replace', actual, expectedTurns(c, state));
    await host.coreClose(handle);
  });
});

test('ArkTS turn projections reject malformed input and deliberately wrong expectations', options, async (context) => {
  await withHost(context, async (host) => {
    await assert.rejects(() => host.projectionJSON('not JSON'), /projection: input is not valid JSON/);
    await assert.rejects(() => host.projectionJSON('[]'), /projection: input is not an object/);
    await assert.rejects(() => host.projection('turns', null), /turns: input does not decode/);
    await assert.rejects(() => host.projection('turns', {}), /turns: input does not decode/);
    await assert.rejects(() => host.projection('turns', {items: {}}), /turns: input does not decode/);
    await assert.rejects(() => host.projection('turns', {items: [{id: 'row'}]}), /turns: input does not decode/);
    await assert.rejects(() => host.projection('turns', {items: '[]'}), /turns: input does not decode/);

    const c = byName.get('plain_exchange');
    const actual = await host.projection('turns', c.input);
    assert.throws(() => compareTurns(c.name, actual, wrongTurns(actual)),
      (error) => error instanceof assert.AssertionError && error.message.includes(c.name));
    const empty = await host.projection('turns', byName.get('empty_session').input);
    assert.throws(() => compareTurns('empty_session', empty, wrongTurns(empty)),
      (error) => error instanceof assert.AssertionError && error.message.includes('empty_session'));

    const operationCase = byName.get('incremental_with_late_result_and_older_page');
    const operation = operationCase.input.operations[0];
    const checkpointActual = await host.projection('turns', {items: operation.items});
    const state = operationCase.expected.checkpoints[0];
    compareTurns(operationCase.name + ' checkpoint 0', checkpointActual, expectedTurns(operationCase, state));
    assert.throws(() => compareTurns(operationCase.name + ' checkpoint 0', checkpointActual, wrongTurns(checkpointActual)),
      (error) => error instanceof assert.AssertionError && error.message.includes('checkpoint 0'));
  });
});
