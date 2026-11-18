import test from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks, stripTypeScriptTypes } from 'node:module';
import { readFileSync } from 'node:fs';

registerHooks({
  resolve(specifier, context, next) {
    if (specifier === '@kit.ArkUI') return { url: 'native-insets:arkui', shortCircuit: true };
    return next(specifier, context);
  },
  load(url, context, next) {
    if (url === 'native-insets:arkui') return {
      format: 'module', shortCircuit: true, source: 'export class UIContext {}'
    };
    if (url.endsWith('/NativeInsets.ets')) return {
      format: 'module', shortCircuit: true,
      source: stripTypeScriptTypes(readFileSync(new URL(url), 'utf8'))
    };
    return next(url, context);
  }
});
const { nativeSheetInset } = await import('../entry/src/main/ets/platform/NativeInsets.ets');

test('native sheets add only the screen-space overlap beyond the current safe boundary', () => {
  let topVp = 600, heightPx = 920;
  const context = {
    getFrameNodeById: id => id === 'sheet' ? {
      getPositionToScreenWithTransform: () => ({ y: topVp }),
      getMeasuredSize: () => ({ height: heightPx })
    } : undefined,
    vp2px: value => value * 3,
    px2vp: value => value / 3
  };
  assert.equal(nativeSheetInset(context, 'sheet', 2636), 28);
  // A centered or already-avoided sheet must not receive another system inset.
  heightPx = 836;
  assert.equal(nativeSheetInset(context, 'sheet', 2636), 0);
  topVp = 300;
  assert.equal(nativeSheetInset(context, 'sheet', 2636), 0);
  topVp = 600; heightPx = 920;
  assert.equal(nativeSheetInset(context, 'sheet', 2500), 220 / 3);
});

test('native sheets do not invent an inset before their node or safe boundary exists', () => {
  assert.equal(nativeSheetInset({}, 'sheet', 0), 0);
  assert.equal(nativeSheetInset({ getFrameNodeById: () => undefined }, 'sheet', 2636), 0);
});
