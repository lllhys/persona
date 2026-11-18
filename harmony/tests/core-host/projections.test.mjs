// Replays existing pure projections through the real Rust core's ArkTS host command. Every expected object comes
// from the unchanged checked-in fixture; no Rust output is used to construct an expectation. This is not a NAPI addon,
// production ArkTS adapter, HAP or device run.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { startCore } from './host.mjs';

const fixtures = fileURLToPath(new URL('../../../spec/fixtures/', import.meta.url));
function document(path) {
  return JSON.parse(readFileSync(fixtures + path, 'utf8'));
}
const answerDocs = new Map(['questions.json', 'recommended.json'].map((file) => [file, document('answer/' + file)]));
const streamDoc = document('stream-chunks/split.json');
const linesDocs = readdirSync(fixtures + 'lines').filter((file) => file.endsWith('.json')).sort()
  .map((file) => ({file, ...document('lines/' + file)}));
const lineCases = linesDocs.flatMap((doc) => doc.cases.map((c) => ({file: doc.file, ...c})));
const taskCases = lineCases.filter((c) => Object.hasOwn(c.expected, 'task_index'));
const receiptCase = lineCases.find((c) => c.name === 'background_running_badge');
assert.ok(receiptCase, 'the existing background-receipt boundary is present');

const expectedNames = new Map([
  ['answer/questions.json', [
    'single_tap_sends_original_label', 'single_pick_clears_other', 'other_answer_trimmed',
    'empty_answer_cannot_submit', 'blank_other_cannot_submit', 'multiple_picks_follow_option_order',
    'multiple_other_overrides_picks', 'blank_other_uses_picks', 'multiple_pick_waits_for_submit',
    'multiple_pick_toggles_off', 'multiple_questions_send_original_keys', 'last_page_missing_answer_returns_to_gap',
    'next_page_does_not_send', 'all_answered_can_submit_from_earlier_page', 'sending_disables_option',
  ]],
  ['answer/recommended.json', [
    'recommended_spaced', 'recommended_unspaced', 'recommended_chinese_fullwidth', 'recommended_chinese_ascii',
    'recommended_trims_prefix_space', 'recommended_case_sensitive', 'recommended_suffix_only',
    'recommended_trailing_space',
    'recommended_corefoundation_boundary',
  ]],
  ['stream-chunks/split.json', [
    'cuts_after_blank_line_once_next_block_starts', 'blank_line_at_end_is_not_cut_yet',
    'never_cuts_inside_code_fence', 'keeps_indented_continuation_with_block', 'prewarm_uses_the_tail',
    'longer_fence_ignores_shorter_inner_closer', 'language_suffixed_line_does_not_close_fence',
    'reassembles_full_text', 'empty_input', 'consecutive_blank_lines_keep_their_newline',
    'space_padded_separator_is_omitted', 'tab_indented_continuation_stays_with_block',
    'non_ascii_space_is_not_raw_indentation', 'carriage_return_is_not_a_line_separator',
    'crlf_carriage_return_is_not_a_blank_line', 'crlf_fence_is_not_recognized',
    'inline_backticks_do_not_open_a_fence', 'unclosed_fence_keeps_remainder_in_tail',
    'indented_fence_preserves_original_spaces', 'tilde_fence_requires_tilde_closer',
    'leading_blank_line_can_finish_an_empty_block',
  ]],
]);
const expectedTaskNames = [
  'task_notices_find_calls', 'task_title_containing_interrupt_is_not_interrupt',
  'monitor_event_uses_first_detail_line', 'task_without_loaded_call_uses_title', 'task_status_outcomes',
];
function checkManifest(label, cases, expected) {
  assert.deepEqual(cases.map((c) => c.name), expected, `${label}: exact case-name sequence`);
  assert.equal(new Set(cases.map((c) => c.name)).size, expected.length, `${label}: no duplicate cases`);
}
for (const [file, doc] of answerDocs) {
  assert.equal(doc.schema_version, 1, `${file}: schema version`);
  checkManifest('answer/' + file, doc.cases, expectedNames.get('answer/' + file));
}
checkManifest('stream-chunks/split.json', streamDoc.cases, expectedNames.get('stream-chunks/split.json'));
assert.equal(new Set(lineCases.map((c) => c.name)).size, lineCases.length, 'no duplicate transcript case names');
assert.deepEqual(taskCases.map((c) => c.name).sort(), [...expectedTaskNames].sort(), 'the exact task-index manifest');
assert.equal(taskCases.length, 5);
assert.equal(receiptCase.expected.task_index, undefined, 'the receipt is an additional boundary, not a sixth index fixture');

function compareExpected(label, actual, expected) {
  assert.deepEqual(actual, expected, label);
}
function expectComparisonFailure(label, actual, wrong) {
  assert.notDeepEqual(actual, wrong, `${label}: the deliberately wrong expectation must differ`);
  assert.throws(() => compareExpected(label, actual, wrong),
    (error) => error instanceof assert.AssertionError && error.message.includes(label));
}
function answerResult(c, transition, display) {
  const result = {allowed: transition.allowed, resolution: transition.resolution};
  if (transition.page !== c.input.operation.page) result.next_page = transition.page;
  const operation = c.input.operation;
  const question = c.input.interaction.questions[operation.page];
  if (operation.action === 'pick_option' && transition.allowed && question.multi_select) result.picks = transition.picks;
  if (display !== undefined) result.display = display;
  return result;
}
function taskReceipt(actual) {
  // The background receipt starts a call without producing notices or statuses; it keeps the original description.
  const tool = receiptCase.input.items.find((item) => item.id === '2#0');
  assert.equal(tool.tool_use_id, 'call_a');
  compareExpected('background_running_badge: complete receipt projection', actual, {
    started: [tool.tool_use_id], calls: {}, descriptions: {[tool.tool_use_id]: tool.tool_input.description},
    started_at: {}, statuses: {},
  });
}

async function withHost(context, run) {
  const host = startCore();
  // node:test's timeout does not unwind a pending await; release the helper and its stdio explicitly.
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
const options = {timeout: 10_000};

test('ArkTS host replays all answer and display_answer expectations', options, async (context) => {
  await withHost(context, async (host) => {
    let count = 0;
    for (const [file, doc] of answerDocs) {
      for (const c of doc.cases) {
        assert.equal(c.derived, true, `${c.name}: source-derived profile`);
        assert.equal(c.from_test, undefined);
        assert.ok(c.source.length > 0);
        assert.equal(c.input.profile, 'ios_current');
        const transition = await host.projection('answer', c.input);
        let display;
        if (file === 'recommended.json') {
          const operation = c.input.operation;
          const question = c.input.interaction.questions[operation.page];
          display = await host.projection('display_answer', question.options[operation.option_index].label);
        }
        compareExpected(`answer/${file}/${c.name}`, answerResult(c, transition, display), c.expected);
        count++;
      }
    }
    assert.equal(count, 24);
    console.log('  answer/display_answer host replay: 15 + 9 complete expected objects');
  });
});

test('ArkTS host replays all stream_chunks expectations', options, async (context) => {
  await withHost(context, async (host) => {
    const replayed = [];
    for (const c of streamDoc.cases) {
      const actual = await host.projection('stream_chunks', c.input);
      compareExpected(`stream-chunks/split.json/${c.name}`, actual, c.expected);
      if (c.name !== 'space_padded_separator_is_omitted') {
        assert.equal([...actual.done, actual.tail].join('\n\n'), c.input.text,
          `${c.name}: reconstruction is secondary, not the expected oracle`);
      }
      replayed.push(c.name);
    }
    assert.deepEqual(replayed, expectedNames.get('stream-chunks/split.json'));
    console.log('  stream_chunks host replay: 21 complete expected objects');
  });
});

test('ArkTS host replays every task_index and the background receipt boundary', options, async (context) => {
  await withHost(context, async (host) => {
    const replayed = [];
    for (const c of lineCases) {
      const actual = await host.projection('task_index', c.input);
      if (c.name === 'background_running_badge') {
        taskReceipt(actual);
      } else if (Object.hasOwn(c.expected, 'task_index')) {
        compareExpected(`${c.file}/${c.name}`, actual, c.expected.task_index);
        replayed.push(c.name);
      }
    }
    assert.deepEqual(replayed.sort(), [...expectedTaskNames].sort());
    console.log('  task_index host replay: 5 complete expected objects and one background receipt');
  });
});

test('ArkTS host projections reject malformed input and wrong expectations', options, async (context) => {
  await withHost(context, async (host) => {
    await assert.rejects(() => host.projectionJSON('not JSON'), /projection: input is not valid JSON/);
    await assert.rejects(() => host.projectionJSON('[]'), /projection: input is not an object/);
    await assert.rejects(() => host.projectionJSON('{"kind":"answer"}'), /expected exactly kind and input/);
    await assert.rejects(() => host.projection('unknown', {}), /unknown projection kind/);
    await assert.rejects(() => host.projection('answer', {invalid: true}), /answer: input does not decode/);
    await assert.rejects(() => host.projection('answer', []), /answer: input does not decode/);
    await assert.rejects(() => host.projection('display_answer', {label: 'Yes'}), /label is not a string/);
    await assert.rejects(() => host.projection('stream_chunks', {}), /stream_chunks: input does not decode/);
    await assert.rejects(() => host.projection('stream_chunks', {text: 1}), /stream_chunks: input does not decode/);
    await assert.rejects(() => host.projection('task_index', {items: {}}), /task_index: input does not decode/);
    await assert.rejects(() => host.projection('task_index', {items: [{id: 'row'}]}), /task_index: input does not decode/);

    const question = answerDocs.get('questions.json').cases[0];
    for (const [field, value] of [['page', 99], ['option_index', 99]]) {
      const input = structuredClone(question.input);
      input.operation[field] = value;
      await assert.rejects(() => host.projection('answer', input), /operation has no transition/);
    }
    const transition = await host.projection('answer', question.input);
    expectComparisonFailure(`${question.name}: answer`, answerResult(question, transition), {allowed: false, resolution: null});
    const recommendation = answerDocs.get('recommended.json').cases[0];
    const label = recommendation.input.interaction.questions[0].options[recommendation.input.operation.option_index].label;
    const display = await host.projection('display_answer', label);
    expectComparisonFailure(`${recommendation.name}: display_answer`, display, {text: label, recommended: false});

    const chunks = streamDoc.cases[0];
    const split = await host.projection('stream_chunks', chunks.input);
    expectComparisonFailure(`${chunks.name}: stream_chunks`, split, {done: [], tail: chunks.input.text});
    const index = await host.projection('task_index', taskCases[0].input);
    expectComparisonFailure(`${taskCases[0].name}: task_index`, index,
      {started: [], calls: {}, descriptions: {}, started_at: {}, statuses: {}});
  });
});
