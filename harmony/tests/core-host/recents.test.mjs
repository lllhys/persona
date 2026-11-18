// Replays every existing recents fixture through the real Rust ArkTS host projection. Expected objects and case names
// come directly from the unchanged JSON, never from Rust output. Host evidence only: no NAPI addon, persistence,
// production adapter, UI or device execution.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { startCore } from './host.mjs';

const fixtures = fileURLToPath(new URL('../../../spec/fixtures/recents/', import.meta.url));
const documents = ['recent_conversations', 'recent_setups'].map((domain) => ({
  domain, ...JSON.parse(readFileSync(`${fixtures}${domain}.json`, 'utf8')),
}));
const expectedNames = {
  recent_conversations: [
    'open_into_empty', 'open_goes_first', 'open_again_moves_to_front', 'open_the_first_again', 'keeps_ten',
    'ignores_empty', 'shown_skips_the_current', 'shown_skips_the_missing', 'shown_up_to_the_limit',
    'shown_without_a_current', 'shown_nothing_else',
  ],
  recent_setups: [
    'record_into_empty', 'record_moves_the_same_setup_first', 'record_keeps_twenty', 'record_differs_by_model',
    'cards_skip_unknown_machines', 'cards_at_most_three', 'cards_offline_shown',
    'model_of_the_latest_same_launcher', 'model_other_launcher_is_default', 'model_unknown_machine',
  ],
};
for (const doc of documents) {
  assert.equal(doc.schema_version, 1, `${doc.domain}: schema version`);
  assert.deepEqual(doc.cases.map((c) => c.name), expectedNames[doc.domain], `${doc.domain}: exact manifest`);
  assert.equal(new Set(doc.cases.map((c) => c.name)).size, doc.cases.length, `${doc.domain}: unique names`);
}

async function withHost(context, run) {
  const host = startCore();
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

test('ArkTS host replays every recents fixture as a complete expected object', options, async (context) => {
  await withHost(context, async (host) => {
    let count = 0;
    for (const doc of documents) {
      for (const c of doc.cases) {
        const input = {domain: doc.domain, operation: c.operation, input: c.input};
        const actual = await host.projection('recents', input);
        assert.deepEqual(actual, c.expected, `${doc.domain}/${c.name}: complete expected object`);
        count++;
      }
    }
    assert.equal(count, 21);
    console.log('  recents host replay: 11 conversation + 10 setup complete objects');
  });
});

test('ArkTS host rejects malformed recents input and wrong expected objects', options, async (context) => {
  await withHost(context, async (host) => {
    const first = documents[0].cases[0];
    const actual = await host.projection('recents', {
      domain: 'recent_conversations', operation: first.operation, input: first.input,
    });
    const wrong = structuredClone(actual);
    wrong.after = ['not-the-opened-conversation'];
    assert.notDeepEqual(actual, wrong, 'the wrong expected object must differ');
    assert.throws(() => assert.deepEqual(actual, wrong), assert.AssertionError);
    for (const input of [
      {}, {domain: 'recent_conversations', operation: first.operation},
      {domain: 'recent_conversations', operation: 'unknown', input: first.input},
      {domain: 'recent_conversations', operation: first.operation, input: {}},
      {domain: 'recent_conversations', operation: first.operation, input: {before: 1, opened: 's:m1/a'}},
    ]) {
      await assert.rejects(() => host.projection('recents', input), /recents:/);
    }
  });
});
