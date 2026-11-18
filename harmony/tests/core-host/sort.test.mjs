// Replays every sort projection fixture through the real Rust core's sort command, proving that the ArkTS host can
// reach the same pure-function boundary the Rust tests exercise. Each case's input goes in as JSON; the output must
// match the fixture's expected JSON exactly.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { HostCore, helperPath, startCore } from './host.mjs';

const fixtureDir = fileURLToPath(new URL('../../../spec/fixtures/sort/', import.meta.url));
const documents = readdirSync(fixtureDir).filter((f) => f.endsWith('.json')).sort().map((f) => ({
  file: f,
  ...JSON.parse(readFileSync(fixtureDir + f, 'utf8')),
}));

// Sort and state are separate pure projections; both use the fixture's complete expected objects.
const projectionDocs = documents.filter((d) => d.operation === 'agents_list');
function exactCases(docs) {
  const names = new Set();
  for (const doc of docs) {
    for (const c of doc.cases) {
      assert.ok(!names.has(c.name), `duplicate projection case: ${c.name}`);
      names.add(c.name);
    }
  }
  return names;
}
const projectionNames = exactCases(projectionDocs);
const stateDocs = documents.filter((d) => d.operation === 'session_state' || d.operation === 'hub_state');
exactCases(stateDocs);
const expectedStateNames = new Map([
  ['session_state.json', ['client_state_precedence', 'waiting_requires_quiet_state']],
  ['hub_state.json', [
    'hub_blocked', 'hub_pending_offline', 'hub_working_seen', 'hub_done_unseen',
    'hub_done_seen_before_tolerance', 'hub_done_seen_at_tolerance', 'hub_done_seen_equal',
    'hub_done_seen_after', 'hub_idle', 'hub_offline', 'hub_unknown',
  ]],
]);
assert.equal(stateDocs.length, expectedStateNames.size, 'exact state fixture document set');
for (const [file, names] of expectedStateNames) {
  const doc = stateDocs.find((d) => d.file === file);
  assert.ok(doc, `missing state fixture: ${file}`);
  assert.deepEqual(doc.cases.map((c) => c.name), names, `${file}: exact case-name sequence`);
}
function compareState(label, actual, expected) {
  assert.deepEqual(actual, expected, label);
}
const stateTestOptions = {timeout: 10_000};
async function stopStateHost(host) {
  try {
    await host.end();
  } finally {
    host.kill();
  }
}

test('sort projection replays every agents_list fixture through the host', async () => {
  const host = new HostCore(helperPath);
  try {
    const replayed = new Set();
    for (const doc of projectionDocs) {
      for (const c of doc.cases) {
        const actual = await host.sort(c.input);
        assert.deepEqual(actual, c.expected, `${doc.file}/${c.name}`);
        replayed.add(c.name);
      }
    }
    assert.deepEqual([...replayed].sort(), [...projectionNames].sort(), 'exact projection case set');
    console.log(`  sort host replay: ${replayed.size} cases`);
  } finally {
    await host.end();
  }
});

test('sort host rejects invalid JSON input', async () => {
  const host = new HostCore(helperPath);
  try {
    await assert.rejects(() => host.sort({ invalid: true }), /sort/);
  } finally {
    await host.end();
  }
});

test('ArkTS host replays the exact session_state and hub_state fixtures', stateTestOptions, async (context) => {
  const host = startCore();
  // node:test's timeout does not unwind a pending await; abort the child explicitly so stdio cannot keep Node alive.
  context.after(() => host.kill(), {timeout: 1_000});
  try {
    const replayed = new Set();
    for (const doc of stateDocs) {
      for (const c of doc.cases) {
        const actual = await host.state(doc.operation, c.input);
        compareState(`${doc.file}/${c.name}`, actual, c.expected);
        replayed.add(c.name);
      }
    }
    const expected = [...expectedStateNames.values()].flat().sort();
    assert.deepEqual([...replayed].sort(), expected, 'every exact state case ran exactly once');
    console.log(`  state host replay: ${replayed.size} cases`);
  } finally {
    await stopStateHost(host);
  }
});

test('ArkTS host state projection rejects malformed and wrong-shaped input', stateTestOptions, async (context) => {
  const host = startCore();
  context.after(() => host.kill(), {timeout: 1_000});
  try {
    await assert.rejects(() => host.state('unknown', {}), /unknown projection kind/);
    await assert.rejects(() => host.state('session_state', {sessions: {}}), /sessions does not decode/);
    await assert.rejects(() => host.state('hub_state', {session: {status: 'done', pending_interactions: '0'}}), /pending_interactions/);
    await assert.rejects(() => host.state('hub_state', {session: {status: 'done', pending_interactions: 0, updated_at: 'invalid'}}), /timestamp/);
  } finally {
    await stopStateHost(host);
  }
});

test('ArkTS host state comparison rejects a wrong fixture expectation', stateTestOptions, async (context) => {
  const c = documents.find((d) => d.operation === 'hub_state').cases[0];
  const host = startCore();
  context.after(() => host.kill(), {timeout: 1_000});
  try {
    const actual = await host.state('hub_state', c.input);
    assert.throws(() => compareState(`hub_state.json/${c.name}`, actual, {state: 'idle'}),
      (error) => error instanceof assert.AssertionError && error.message.includes(`hub_state.json/${c.name}`));
  } finally {
    await stopStateHost(host);
  }
});
