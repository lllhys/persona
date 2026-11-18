// Replays the viewed-image leaf through the real Rust ArkTS host projection. The ordinary expected object comes from
// the checked-in current-iOS overlay; the folded object follows ViewedImagesTests.aFoldGivesTheSameStripAsItsWork.
// No expectation is generated from Rust output. Host helper evidence only: no NAPI addon, adapter, image loading,
// rendering, HAP or device execution.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { startCore } from './host.mjs';

const fixtures = fileURLToPath(new URL('../../../spec/fixtures/lines/', import.meta.url));
const lineCases = readdirSync(fixtures).filter((file) => file.endsWith('.json')).sort()
  .flatMap((file) => JSON.parse(readFileSync(fixtures + file, 'utf8')).cases);
const ordinary = lineCases.find((c) => c.name === 'tool_images_belong_to_call');
const folded = lineCases.find((c) => c.name === 'fold_counts_tools_and_thoughts');
const overlay = JSON.parse(readFileSync(fixtures + 'ios/differences.json', 'utf8'));
const ordinaryExpected = overlay.cases.find((c) => c.name === ordinary.name)
  .replace.lines.find((line) => line.type === 'viewed');
assert.ok(ordinary && folded && ordinaryExpected, 'the independent source cases and overlay are present');
const foldedExpected = {
  id: 'vi-1#0',
  image_ids: ['p:/w/shot.png', 's:image/png:900:16x10', 'p:/w/big.JPG', 's:image/png:300:8x5'],
  is_input: false,
  type: 'viewed',
};

function foldImage(itemId, toolUseId, path, size, width, height,
                  tool = 'Read', source = 'read') {
  return {
    ...(itemId === null ? {} : {item_id: itemId}), tool_use_id: toolUseId, tool, source,
    mime: 'image/png', ...(path === null ? {} : {path}),
    ...(size === null ? {} : {size}), ...(width === null ? {} : {width}),
    ...(height === null ? {} : {height}),
  };
}
const foldedImages = [
  foldImage('5#0', 'a', '/w/shot.png', 100, 4, 3),
  foldImage('8#0', 'b', null, 900, 16, 10, 'mcp__persona__computer_state', 'screenshot'),
  foldImage(null, 'd', '/w/big.JPG', null, null, null),
  foldImage('20#0', 'g', null, 300, 8, 5, 'mcp__persona__browser_look', 'browser'),
];
const foldedInput = structuredClone(folded.input);
const foldItem = foldedInput.items.find((item) => item.kind === 'fold');
assert.ok(foldItem, 'the original fold remains the mutation target');
foldItem.fold.images = foldedImages;

function wrong(actual, mutation) {
  const value = structuredClone(actual);
  mutation(value);
  assert.notDeepEqual(value, actual, 'the wrong expectation must differ before comparison');
  assert.throws(() => assert.deepEqual(actual, value), assert.AssertionError);
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

test('ArkTS host replays complete ordinary and folded viewed-image expectations', options, async (context) => {
  await withHost(context, async (host) => {
    const actual = await host.projection('viewed_images', ordinary.input);
    assert.deepEqual(actual, ordinaryExpected, 'complete post-overlay ordinary viewed object');
    assert.deepEqual(Object.keys(actual).sort(), ['id', 'image_ids', 'is_input', 'type'], 'complete object shape');
    const actualFold = await host.projection('viewed_images', foldedInput);
    assert.deepEqual(actualFold, foldedExpected, 'complete source-derived folded viewed object');
    wrong(actual, (value) => { value.id = 'vi-fold:2#0'; });
    wrong(actualFold, (value) => { value.image_ids = ['i:5#0']; });
    console.log('  viewed_images host replay: ordinary post-overlay and folded complete objects');
  });
});

test('ArkTS host rejects malformed, no-image and deliberately wrong viewed-image expectations', options, async (context) => {
  await withHost(context, async (host) => {
    for (const input of [{invalid: true}, {}, {items: 1}]) {
      await assert.rejects(() => host.projection('viewed_images', input), /viewed_images: input does not decode/);
    }
    await assert.rejects(() => host.projection('viewed_images', {items: []}), /viewed_images: input has no viewed line/);
    await assert.rejects(() => host.projection('viewed_images', folded.input), /viewed_images: input has no viewed line/);
    const actual = await host.projection('viewed_images', ordinary.input);
    wrong(actual, (value) => { value.image_ids = ['i:call_a']; });
  });
});
