import test from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks, stripTypeScriptTypes } from 'node:module';
import { readFileSync } from 'node:fs';

const screen = { foldable: false, region: undefined, failure: false };
globalThis.__personaNativeEnvironment = screen;
registerHooks({
  resolve(specifier, context, next) {
    if (specifier === '@kit.AbilityKit') return { url: 'native-environment:ability', shortCircuit: true };
    if (specifier === '@kit.ArkUI') return { url: 'native-environment:display', shortCircuit: true };
    return next(specifier, context);
  },
  load(url, context, next) {
    if (url === 'native-environment:ability') return {format:'module',shortCircuit:true,source:'export const ConfigurationConstant={ColorMode:{COLOR_MODE_DARK:0,COLOR_MODE_LIGHT:1}};'};
    if (url === 'native-environment:display') return { format: 'module', shortCircuit: true, source: `
      const s = globalThis.__personaNativeEnvironment;
      export const window = {AvoidAreaType:{TYPE_NAVIGATION_INDICATOR:3,TYPE_SYSTEM:0,TYPE_KEYBOARD:5}};
      export const display = { isFoldable: () => s.foldable, getCurrentFoldCreaseRegion() {
        if (s.failure) throw new Error('display unavailable'); return s.region;
      } };
    ` };
    if (url.endsWith('/NativeEnvironment.ets')) return { format: 'module', shortCircuit: true,
      source: stripTypeScriptTypes(readFileSync(new URL(url), 'utf8').replace(/@ObservedV2\s*/g, '').replace(/@Trace\s+/g, '')) };
    return next(url, context);
  }
});
const { NativeEnvironment } = await import('../entry/src/main/ets/platform/NativeEnvironment.ets');

test('ordinary native layout survives absent, invalid or unavailable fold geometry', () => {
  const environment = new NativeEnvironment();
  screen.foldable = true;
  for (const region of [undefined, { creaseRects: [] }, { creaseRects: [{ left: 0, top: 100, width: 0, height: 10 }] },
    { creaseRects: [{ left: 0, top: NaN, width: 100, height: 10 }] }]) {
    screen.region = region; environment.update({}); assert.equal(environment.nativeHoverAvailable, false);
  }
  screen.region = { creaseRects: [{ left: 0, top: 100, width: 100, height: 10 }] };
  environment.update({ fontSizeScale: 3.2 }); assert.equal(environment.nativeHoverAvailable, true);
  environment.update({}); assert.equal(environment.fontScale, 3.2);
  screen.failure = true; environment.update({}); assert.equal(environment.nativeHoverAvailable, false);
  screen.failure = false; screen.foldable = false; environment.update({}); assert.equal(environment.nativeHoverAvailable, false);
});


test('window geometry and native navigation avoidance update together and listeners retire',()=>{
 const environment=new NativeEnvironment(),handlers=new Map();let rect={top:40,height:1000},bottom=60;
 const nativeWindow={getWindowProperties:()=>({windowRect:rect}),getWindowAvoidArea:()=>({bottomRect:{height:bottom}}),on:(name,callback)=>handlers.set(name,callback),off:(name,callback)=>{assert.equal(handlers.get(name),callback);handlers.delete(name);}};
 environment.bindWindow(nativeWindow);assert.equal(environment.safeBottomPx,980);
 rect={top:40,height:1200};handlers.get('windowSizeChange')({width:600,height:1200});assert.equal(environment.safeBottomPx,1180);
 bottom=80;handlers.get('avoidAreaChange')({type:3,area:{}});assert.equal(environment.safeBottomPx,1160);
 rect={top:140,height:1200};handlers.get('windowRectChange')({rect});assert.equal(environment.safeBottomPx,1260);
 environment.unbindWindow();assert.equal(handlers.size,0);
});


test('native color mode follows resolved configuration without resetting for unrelated updates',()=>{
 const environment=new NativeEnvironment();environment.update({colorMode:0});assert.equal(environment.dark,true);environment.update({fontSizeScale:2});assert.equal(environment.dark,true);environment.update({colorMode:1});assert.equal(environment.dark,false);
});

test('popup geometry uses native window width and the window-relative top avoidance',()=>{const e=new NativeEnvironment(),handlers=new Map();let rect={top:140,width:1260,height:2720};const win={getWindowProperties:()=>({windowRect:rect}),getWindowAvoidArea:type=>type===0?{topRect:{height:117}}:{bottomRect:{height:84}},on:(key,fn)=>handlers.set(key,fn),off:()=>{}};e.bindWindow(win);assert.equal(e.windowWidthPx,1260);assert.equal(e.safeTopPx,117);rect={top:240,width:2584,height:1828};handlers.get('windowRectChange')({rect});assert.equal(e.windowWidthPx,2584);assert.equal(e.safeTopPx,117);e.unbindWindow();});


test('keyboard visibility follows its window and a retired listener cannot resize the replacement',()=>{
 const env=new NativeEnvironment();
 function fakeWindow(initial){const events=new Map();return {events,getWindowProperties:()=>({windowRect:{top:0,width:1260,height:2720}}),getWindowAvoidArea:type=>({bottomRect:{height:type===5?initial:0}}),on:(name,fn)=>events.set(name,fn),off:(name,fn)=>{assert.equal(events.get(name),fn);events.delete(name);}};}
 const first=fakeWindow(0);env.bindWindow(first);const stale=first.events.get('keyboardHeightChange');
 stale(900);assert.equal(env.keyboardHeightPx,900);stale(0);assert.equal(env.keyboardHeightPx,0);
 const second=fakeWindow(700);env.bindWindow(second);assert.equal(first.events.size,0);assert.equal(env.keyboardHeightPx,700);
 stale(1200);assert.equal(env.keyboardHeightPx,700);const retired=second.events.get('keyboardHeightChange');env.bindWindow(second);retired(1200);assert.equal(env.keyboardHeightPx,700);second.events.get('keyboardHeightChange')(-10);assert.equal(env.keyboardHeightPx,0);
 const removed=second.events.get('keyboardHeightChange');env.unbindWindow();assert.equal(env.keyboardHeightPx,0);assert.equal(env.keyboardChanged,undefined);removed(900);assert.equal(env.keyboardHeightPx,0);assert.equal(second.events.size,0);
});
