// Replays the Apple StreamRepair assertions and existing JNI-derived boundaries through the real Rust core's ArkTS
// host projection command. Expectations are independently transcribed from the Apple tests/source and the existing
// JNI replay, never from Rust output. Host evidence only: no NAPI addon, production adapter, HAP or device run.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { startCore } from './host.mjs';

// 22 fence expectations (21 repair inputs plus the additional `let a` end input): StreamRepairTests.closesAnOpenCodeFenceWithItsOwnMarker,
// holdsAFencesOpeningLineBackUntilItsInfoStringIsWhole, holdsAHalfWrittenClosingRunBack,
// changesNothingInsideAFenceButItsEnd and closesAFenceInTheListItemItOpenedIn. The `prefix` input is the existing
// JNI-derived partial-closing-run boundary, not an Apple test assertion. Every entry is a complete {repaired, end}
// object; the end values come from StreamRepair.end's code/fence rules, never from the repaired text or Rust output.
const fence = [
  [{text: 'text\n```ts\nconst a', repaired: 'text\n```ts\nconst a\n```', end: 'code'}, 'closesAnOpenCodeFenceWithItsOwnMarker'],
  [{text: '~~~~\nx\n', repaired: '~~~~\nx\n~~~~', end: 'code'}, 'closesAnOpenCodeFenceWithItsOwnMarker'],
  [{text: '```\na\n```\ndone', repaired: '```\na\n```\ndone', end: 'text'}, 'closesAnOpenCodeFenceWithItsOwnMarker'],
  [{text: '看这段：\n```swi', repaired: '看这段：\n', end: 'code'}, 'holdsAFencesOpeningLineBackUntilItsInfoStringIsWhole'],
  [{text: '```mermaid', repaired: '', end: 'code'}, 'holdsAFencesOpeningLineBackUntilItsInfoStringIsWhole'],
  [{text: '看这段：\n```swift\n', repaired: '看这段：\n```swift\n```', end: 'code'}, 'holdsAFencesOpeningLineBackUntilItsInfoStringIsWhole'],
  [{text: '```\nlet a = 1\n``', repaired: '```\nlet a = 1\n```', end: 'code'}, 'holdsAHalfWrittenClosingRunBack'],
  [{text: '````\nx\n```', repaired: '````\nx\n````', end: 'code'}, 'holdsAHalfWrittenClosingRunBack'],
  [{text: '````\nx\n```\n', repaired: '````\nx\n```\n````', end: 'code'}, 'holdsAHalfWrittenClosingRunBack'],
  [{text: 'prefix\n````\nx\n```\n', repaired: 'prefix\n````\nx\n```\n````', end: 'code'}, 'JNI-derived prefix + partial closer'],
  [{text: '```\n**x [a](b', repaired: '```\n**x [a](b\n```', end: 'code'}, 'changesNothingInsideAFenceButItsEnd'],
  [{text: '```\n-', repaired: '```\n-\n```', end: 'code'}, 'changesNothingInsideAFenceButItsEnd'],
  [{text: '1. 安装依赖：\n\n   ```bash\n   npm install', repaired: '1. 安装依赖：\n\n   ```bash\n   npm install\n   ```', end: 'code'}, 'closesAFenceInTheListItemItOpenedIn'],
  [{text: '1. 安装依赖：\n   ```bash\n   npm install\n', repaired: '1. 安装依赖：\n   ```bash\n   npm install\n   ```', end: 'code'}, 'closesAFenceInTheListItemItOpenedIn'],
  [{text: '- 运行：\n  ~~~\n  make', repaired: '- 运行：\n  ~~~\n  make\n  ~~~', end: 'code'}, 'closesAFenceInTheListItemItOpenedIn'],
  [{text: '- 运行：\n  ```\n  make\n  ``', repaired: '- 运行：\n  ```\n  make\n  ```', end: 'code'}, 'closesAFenceInTheListItemItOpenedIn'],
  [{text: '```\na\n  ', repaired: '```\na\n```', end: 'code'}, 'leavesOutALineOfOnlyIndentationSoFar'],
  [{text: '这是 **重要', repaired: '这是 **重要**', end: 'text'}, 'closesWhatTheGrowingLineLeavesOpen (#9286)'],
  [{text: '```swift\nlet a\n```', repaired: '```swift\nlet a\n```', end: 'code'}, 'tellsWhereTheTextEnds'],
  [{text: '```swift\nlet a\n```\n', repaired: '```swift\nlet a\n```\n', end: 'text'}, 'tellsWhereTheTextEnds'],
  [{text: '```swift\nlet a', repaired: '```swift\nlet a\n```', end: 'code'}, 'tellsWhereTheTextEnds'],
  [{text: '', repaired: '', end: 'text'}, 'tellsWhereTheTextEnds'],
];

// 14 unfinished-marker expectations: holdsALastLineOfOnlyMarkersBack, leavesALineThatHasEnded and existing JNI
// controls. All are complete objects. The ten-digit/full-width and marker-prefixed digit boundaries follow
// Scan.markersOnly's ASCII/length/position rules; Scan.string retains the newline before a held-back marker.
const marker = [
  [{text: '- 第一项\n-', repaired: '- 第一项\n', end: 'text'}, 'Apple markersOnly'],
  [{text: '- 第一项\n- ', repaired: '- 第一项\n', end: 'text'}, 'Apple markersOnly'],
  [{text: '1. 第一步\n2.', repaired: '1. 第一步\n', end: 'text'}, 'Apple markersOnly'],
  [{text: '1. 第一步\n12', repaired: '1. 第一步\n', end: 'text'}, 'Apple markersOnly'],
  [{text: '正文\n##', repaired: '正文\n', end: 'text'}, 'Apple markersOnly'],
  [{text: '正文\n>', repaired: '正文\n', end: 'text'}, 'Apple markersOnly'],
  [{text: '标题\n---', repaired: '标题\n', end: 'text'}, 'Apple markersOnly'],
  [{text: '- [ ]', repaired: '', end: 'text'}, 'Apple markersOnly'],
  [{text: '**', repaired: '', end: 'text'}, 'Apple markersOnly'],
  [{text: '- 第', repaired: '- 第', end: 'text'}, 'Apple markersOnly control'],
  [{text: '2026年', repaired: '2026年', end: 'text'}, 'Apple markersOnly control'],
  [{text: '1.5 倍', repaired: '1.5 倍', end: 'text'}, 'Apple markersOnly control'],
  [{text: '-\n', repaired: '-\n', end: 'text'}, 'leavesALineThatHasEnded'],
  [{text: '正文\n---\n', repaired: '正文\n---\n', end: 'text'}, 'existing JNI ended-line control'],
  [{text: '1234567890', repaired: '1234567890', end: 'text'}, 'existing JNI ten-digit boundary'],
  [{text: '- 12', repaired: '- 12', end: 'text'}, 'existing JNI marker-prefixed digits'],
  [{text: '１２', repaired: '１２', end: 'text'}, 'existing JNI full-width digits'],
  [{text: '\n-', repaired: '\n', end: 'text'}, 'existing JNI leading blank line'],
  [{text: '\n##', repaired: '\n', end: 'text'}, 'existing JNI leading blank line'],
];

// 39 table expectations (22 repair inputs, all 15 original end inputs including overlaps, and seven
// JNI/Foundation delimiter boundaries): from holdsATablesHalfWrittenLastRowBack,
// givesATableHeaderItsDelimiterRowEarly, findsATableRightUnderAParagraphLine, keepsATableInTheListItemItIsIn
// and findsNoTableInALazyLine. Complete end values follow the explicitly asserted table boundaries and
// StreamRepair.end's no-table/no-fence fallback. JNI-derived delimiter boundaries use Foundation .whitespaces:
// tab and Unicode Zs are trimmed; CR, vertical tab, NEL, line separator and paragraph separator are not.
const head = '| 文件 | 改动 |\n| --- | --- |\n| a.swift | 刷新 |\n';
const table = [
  [{text: head + '| b.swi', repaired: head, end: 'table'}, 'half-written last row'],
  [{text: head + '| b.swift | 超时 |\n', repaired: head + '| b.swift | 超时 |\n', end: 'table'}, 'finished last row'],
  [{text: '| 文件 | 改', repaired: '| 文件 | 改', end: 'text'}, 'unfinished header'],
  [{text: 'Use a | b\n| not a table', repaired: 'Use a | b\n| not a table', end: 'text'}, 'pipe in prose'],
  [{text: '| 文件 | 改动 |\n', repaired: '| 文件 | 改动 |\n| --- | --- |', end: 'table'}, 'early delimiter'],
  [{text: '| a | b |\n| --', repaired: '| a | b |\n| --- | --- |', end: 'table'}, 'partial delimiter'],
  [{text: '| a | b |\n|', repaired: '| a | b |\n| --- | --- |', end: 'table'}, 'partial delimiter'],
  [{text: '## 结果\n| a | b | c |\n', repaired: '## 结果\n| a | b | c |\n| --- | --- | --- |', end: 'table'}, 'heading + header'],
  [{text: '| a | b |\n| :-- | --: |', repaired: '| a | b |\n| :-- | --: |', end: 'table'}, 'aligned delimiter'],
  [{text: '| 不是表格\n', repaired: '| 不是表格\n', end: 'text'}, 'prose pipe row'],
  [{text: '| x |\n| a | b |\n', repaired: '| x |\n| a | b |\n', end: 'text'}, 'pipe row under pipe row'],
  [{text: '**结果：**\n| a | b |\n', repaired: '**结果：**\n| a | b |\n| --- | --- |', end: 'table'}, 'paragraph + header'],
  [{text: '**结果：**\n| a | b |\n| --', repaired: '**结果：**\n| a | b |\n| --- | --- |', end: 'table'}, 'paragraph + delimiter'],
  [{text: '**结果：**\n| a | b |\n| --- | --- |\n| 1', repaired: '**结果：**\n| a | b |\n| --- | --- |\n', end: 'table'}, 'Apple repair/end'],
  [{text: '- 项\n\n  | a | b |\n', repaired: '- 项\n\n  | a | b |\n  | --- | --- |', end: 'table'}, 'Apple list table/end'],
  [{text: '1. 项\n   | a | b |\n   | -', repaired: '1. 项\n   | a | b |\n   | --- | --- |', end: 'table'}, 'ordered list table'],
  [{text: '- 项\n  | a | b |\n  | --- | --- |\n  | 1', repaired: '- 项\n  | a | b |\n  | --- | --- |\n', end: 'table'}, 'Apple list repair/end'],
  [{text: '- 项\n\n  | a | b |\n  | --- | --- |\n  ', repaired: '- 项\n\n  | a | b |\n  | --- | --- |\n', end: 'table'}, 'Apple list repair/end'],
  [{text: '- 项\n| a | b |\n', repaired: '- 项\n| a | b |\n', end: 'text'}, 'lazy list continuation'],
  [{text: '- a\n  - b\n  | x |\n', repaired: '- a\n  - b\n  | x |\n', end: 'text'}, 'lazy list continuation'],
  [{text: '> 引用\n| a | b |\n', repaired: '> 引用\n| a | b |\n', end: 'text'}, 'quote continuation'],
  [{text: '- 项\n| a | b |\n| --- | --- |\n| 1', repaired: '- 项\n| a | b |\n| --- | --- |\n| 1', end: 'text'}, 'Apple lazy repair/end'],
  [{text: '**结果：**\n| a | b |\n| --- | --- |\n| 1 | 2 |\n', repaired: '**结果：**\n| a | b |\n| --- | --- |\n| 1 | 2 |\n', end: 'table'}, 'Apple finished table end'],
  [{text: '| a | b |\n| --- | --- |\nfoo', repaired: '| a | b |\n| --- | --- |\nfoo', end: 'table'}, 'Apple row without pipes'],
  [{text: '| a | b |\n| --- | --- |\n- 项', repaired: '| a | b |\n| --- | --- |\n- 项', end: 'text'}, 'Apple block after table'],
  [{text: '- 项\n\n  | a | b |\n  ', repaired: '- 项\n\n  | a | b |\n  | --- | --- |', end: 'table'}, 'Apple indented table end'],
  [{text: '| a | b |\n| --- | --- |\n| 1', repaired: '| a | b |\n| --- | --- |\n', end: 'table'}, 'tellsWhereTheTextEnds'],
  [{text: '| a | b |\n| --- | --- |\n| 1 | 2 |\n', repaired: '| a | b |\n| --- | --- |\n| 1 | 2 |\n', end: 'table'}, 'tellsWhereTheTextEnds'],
  [{text: '| a | b |\n', repaired: '| a | b |\n| --- | --- |', end: 'table'}, 'tellsWhereTheTextEnds'],
  [{text: '| a | b |\n| -', repaired: '| a | b |\n| --- | --- |', end: 'table'}, 'tellsWhereTheTextEnds'],
  [{text: '| a | b', repaired: '| a | b', end: 'text'}, 'tellsWhereTheTextEnds'],
  [{text: '| a | b |\n| --- | --- |\n\n后文', repaired: '| a | b |\n| --- | --- |\n\n后文', end: 'text'}, 'tellsWhereTheTextEnds'],
  [{text: '| a |\n| -\r |\n| x', repaired: '| a |\n| -\r |\n| x', end: 'text'}, 'existing JNI CR delimiter'],
  [{text: '| a |\n| -\u000b |\n| x', repaired: '| a |\n| -\u000b |\n| x', end: 'text'}, 'existing JNI vertical-tab delimiter'],
  [{text: '| a |\n| -\t |\n| x\n', repaired: '| a |\n| -\t |\n| x\n', end: 'table'}, 'existing JNI tab delimiter'],
  [{text: '| a |\n| -\u00a0 |\n| x\n', repaired: '| a |\n| -\u00a0 |\n| x\n', end: 'table'}, 'existing JNI NBSP delimiter'],
  [{text: '| a |\n| -\u0085 |\n| x', repaired: '| a |\n| -\u0085 |\n| x', end: 'text'}, 'existing JNI NEL delimiter'],
  [{text: '| a |\n| -\u2028 |\n| x', repaired: '| a |\n| -\u2028 |\n| x', end: 'text'}, 'existing JNI line-separator delimiter'],
  [{text: '| a |\n| -\u2029 |\n| x', repaired: '| a |\n| -\u2029 |\n| x', end: 'text'}, 'Apple-source paragraph-separator boundary'],
];

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

// #9515's five explicit Apple assertions plus 31 source-derived controls, including its malformed-input case, are
// replayed through the real Rust plain_open_fence host projection. Complete {text} objects and drawn arrays are
// copied from the independently reviewed expectations, never inferred from this surface's output.
const plainFence = [
  [{text: '看图：\n```mermaid\ngraph TD', drawn: ['mermaid', 'math']}, {text: '看图：\n```\ngraph TD'}, 'Apple explicit'],
  [{text: '  ~~~ math \nx^2\n', drawn: ['mermaid', 'math']}, {text: '  ~~~\nx^2\n'}, 'Apple explicit'],
  [{text: '```mermaid\ngraph TD\n```\n', drawn: ['mermaid', 'math']}, {text: '```mermaid\ngraph TD\n```\n'}, 'Apple explicit'],
  [{text: '```mermaid\na\n```\n```swift\nlet', drawn: ['mermaid', 'math']}, {text: '```mermaid\na\n```\n```swift\nlet'}, 'Apple explicit'],
  [{text: '```swift\nlet', drawn: ['mermaid', 'math']}, {text: '```swift\nlet'}, 'Apple explicit'],
  [{text: '', drawn: ['mermaid', 'math']}, {text: ''}, 'empty input'],
  [{text: 'mermaid', drawn: ['mermaid', 'math']}, {text: 'mermaid'}, 'no fence run'],
  [{text: '`mermaid`', drawn: ['mermaid', 'math']}, {text: '`mermaid`'}, 'inline code span'],
  [{text: '``mermaid\nx', drawn: ['mermaid', 'math']}, {text: '``mermaid\nx'}, 'two opener backticks'],
  [{text: '```code```\nx', drawn: ['mermaid', 'math']}, {text: '```code```\nx'}, 'extra opener backtick'],
  [{text: '    ```mermaid\nx', drawn: ['mermaid', 'math']}, {text: '    ```\nx'}, 'four-column indentation'],
  [{text: '\t~~~math \nx', drawn: ['mermaid', 'math']}, {text: '\t~~~\nx'}, 'tab indentation'],
  [{text: '````math \nx', drawn: ['mermaid', 'math']}, {text: '````\nx'}, 'four-character opener'],
  [{text: '```mermaid', drawn: ['mermaid', 'math']}, {text: '```'}, 'opener only'],
  [{text: '```mermaid\n', drawn: ['mermaid', 'math']}, {text: '```\n'}, 'opener and newline'],
  [{text: '```mermaid\r', drawn: ['mermaid', 'math']}, {text: '```'}, 'trailing CR'],
  [{text: '```mermaid\r\nx', drawn: ['mermaid', 'math']}, {text: '```\nx'}, 'CRLF'],
  [{text: '\r```mermaid\nx', drawn: ['mermaid', 'math']}, {text: '\r```\nx'}, 'leading CR'],
  [{text: '```mermaid \nx', drawn: ['mermaid', 'math']}, {text: '```\nx'}, 'trailing NBSP'],
  [{text: ' ```mermaid\nx', drawn: ['mermaid', 'math']}, {text: ' ```mermaid\nx'}, 'leading NBSP'],
  [{text: '```mermaid\nx', drawn: ['mermaid', 'math']}, {text: '```\nx'}, 'vertical tab before info'],
  [{text: '```mermaid\nx', drawn: ['mermaid', 'math']}, {text: '```\nx'}, 'NEL after info'],
  [{text: '```mermaid \nx', drawn: ['mermaid', 'math']}, {text: '```\nx'}, 'line separator after info'],
  [{text: '```mermaid \nx', drawn: ['mermaid', 'math']}, {text: '```\nx'}, 'paragraph separator after info'],
  [{text: '```mermaid extra\nx', drawn: ['mermaid', 'math']}, {text: '```mermaid extra\nx'}, 'complete predicate'],
  [{text: '```MERMAID\nx', drawn: ['mermaid', 'math']}, {text: '```MERMAID\nx'}, 'case-sensitive predicate'],
  [{text: '```mermaid\nx\n~~~\n', drawn: ['mermaid', 'math']}, {text: '```\nx\n~~~\n'}, 'different-character closer'],
  [{text: '````mermaid\nx\n```\n', drawn: ['mermaid', 'math']}, {text: '````\nx\n```\n'}, 'shorter closer'],
  [{text: '```mermaid\nx\n```\n~~~math\ny', drawn: ['mermaid', 'math']}, {text: '```mermaid\nx\n```\n~~~\ny'}, 'open math fence'],
  [{text: '$$\n```mermaid\nx', drawn: ['mermaid', 'math']}, {text: '$$\n```mermaid\nx'}, 'fence-looking line inside math'],
  [{text: '```mermaid\n$$\nx', drawn: ['mermaid', 'math']}, {text: '```\n$$\nx'}, 'math-looking line inside fence'],
  [{text: '```mermaid\nx', drawn: ['math']}, {text: '```mermaid\nx'}, 'wrong drawn predicate'],
  [{text: '```mermaid\nx', drawn: []}, {text: '```mermaid\nx'}, 'empty drawn input'],
  [{text: '```mermaid extra\nx', drawn: ['mermaid extra']}, {text: '```\nx'}, 'complete drawn info'],
  [{text: '```图 é \nx', drawn: ['图 é']}, {text: '```\nx'}, 'decomposed Unicode info'],
];

async function replay(host, cases, label) {
  for (const [{text, ...expected}, source] of cases) {
    assert.deepEqual(Object.keys(expected).sort(), ['end', 'repaired'], `${label}: complete expectation shape`);
    const actual = await host.projection('stream_repair', {text});
    assert.deepEqual(actual, expected, `${label}: ${JSON.stringify(text)} (${source})`);
  }
}

function wrongObject(actual, mutation) {
  const wrong = structuredClone(actual);
  mutation(wrong);
  assert.notDeepEqual(wrong, actual, 'the negative control must differ before the comparison');
  assert.throws(() => assert.deepEqual(actual, wrong), assert.AssertionError);
}

test('ArkTS host replays every fence expectation as a complete object', options, async (context) => {
  await withHost(context, async (host) => {
    assert.equal(fence.length, 22);
    await replay(host, fence, 'fence');
    console.log(`  stream_repair fence host replay: ${fence.length} complete objects`);
  });
});

test('ArkTS host replays every marker and boundary expectation as a complete object', options, async (context) => {
  await withHost(context, async (host) => {
    assert.equal(marker.length, 19);
    await replay(host, marker, 'marker');
    console.log(`  stream_repair marker host replay: ${marker.length} complete objects`);
  });
});

test('ArkTS host replays every table and delimiter expectation as a complete object', options, async (context) => {
  await withHost(context, async (host) => {
    assert.equal(table.length, 39);
    await replay(host, table, 'table');
    console.log(`  stream_repair table host replay: ${table.length} complete objects`);
  });
});

test('ArkTS host replays every independent plain-fence expectation as a complete object', options, async (context) => {
  await withHost(context, async (host) => {
    assert.equal(plainFence.length, 35);
    for (const [input, expected, source] of plainFence) {
      assert.deepEqual(Object.keys(expected), ['text'], 'complete plain-fence expectation shape');
      const actual = await host.projection('plain_open_fence', input);
      assert.deepEqual(actual, expected, `plain_open_fence: ${JSON.stringify(input.text)} (${source})`);
    }
    for (const input of [
      {text: '```mermaid'},
      {drawn: []},
      {text: 1, drawn: []},
      {text: 'x', drawn: 1},
      {text: 'x', drawn: [1]},
    ]) {
      await assert.rejects(() => host.projection('plain_open_fence', input), /plain_open_fence: input does not decode/);
    }
    const actual = await host.projection('plain_open_fence', plainFence[0][0]);
    wrongObject(actual, (wrong) => { wrong.text = plainFence[0][0].text; });
    wrongObject(actual, (wrong) => { wrong.extra = true; });
    const nonDrawn = await host.projection('plain_open_fence', {
      text: '```swift\nx', drawn: ['mermaid', 'math'],
    });
    assert.deepEqual(nonDrawn, {text: '```swift\nx'}, 'non-drawn input is preserved');
    wrongObject(nonDrawn, (wrong) => { wrong.text = '```\nx'; });
    console.log(`  plain_open_fence host replay: ${plainFence.length} complete objects plus two wrong expectations`);
  });
});

test('ArkTS host rejects malformed inputs and wrong end, repaired and extra-field expectations', options, async (context) => {
  await withHost(context, async (host) => {
    await assert.rejects(() => host.projection('stream_repair', {}), /stream_repair: input does not decode/);
    await assert.rejects(() => host.projection('stream_repair', {text: 1}), /stream_repair: input does not decode/);
    for (const text of ['text\n```ts\nconst a', '| a | b |\n| -', '正文\n##']) {
      const actual = await host.projection('stream_repair', {text});
      wrongObject(actual, (wrong) => { wrong.end = 'not-an-end'; });
      wrongObject(actual, (wrong) => { wrong.repaired += '\nwrong'; });
      wrongObject(actual, (wrong) => { wrong.extra = true; });
    }
  });
});
