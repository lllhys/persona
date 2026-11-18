import test from 'node:test';
import assert from 'node:assert/strict';
import {registerHooks,stripTypeScriptTypes} from 'node:module';
import {readFileSync} from 'node:fs';
import {deviceLookPalette,contrastRatio} from '../entry/src/main/ets/core/DeviceLook.ts';
registerHooks({resolve(s,c,n){if(c.parentURL?.endsWith('/LookTheme.ets')){if(s==='../models/DeviceLook')return {url:'native-theme:model',shortCircuit:true};if(s==='../core/DeviceLook')return {url:new URL('../core/DeviceLook.ts',c.parentURL).href,shortCircuit:true};}return n(s,c);},load(u,c,n){if(u==='native-theme:model')return{format:'module',shortCircuit:true,source:'export class DeviceLook {}'};if(u.endsWith('/LookTheme.ets'))return{format:'module',shortCircuit:true,source:stripTypeScriptTypes(readFileSync(new URL(u),'utf8'))};return n(u,c);}});
const {nativeLookTheme,nativeMessageTheme}=await import('../entry/src/main/ets/design/LookTheme.ets');
test('default System emphasized controls and canvas text each meet their own contrast floor',()=>{
 const palette=deviceLookPalette('style','system',undefined),look={palette:()=>palette};
 for(const dark of [false,true]){
  const resources=JSON.parse(readFileSync(new URL('../entry/src/main/resources/'+(dark?'dark':'base')+'/element/color.json',import.meta.url),'utf8'));
  const canvas=resources.color.find(item=>item.name==='persona_canvas').value;
  const theme=nativeLookTheme(look,'system',dark);
  for(const colors of [theme.colors,theme.darkColors]){
   assert.ok(contrastRatio(colors.compBackgroundEmphasize,colors.fontOnPrimary)>=4.5);
   assert.equal(colors.iconOnPrimary,colors.fontOnPrimary);
  }
  assert.ok(contrastRatio(theme.colors.fontEmphasize,canvas)>=4.5);
 }
 assert.equal(nativeLookTheme(look,'system',true).colors.fontEmphasize,'#86B6FF');
});
for(const style of ['system','glass','brief','board'])for(const accent of ['style','blue','coral','lavender','teal','lark'])for(const dark of [false,true])test(`${style} ${accent} ${dark?'dark':'light'} scope supplies the actual application palette`,()=>{const palette=deviceLookPalette(accent,style,undefined),look={palette:()=>palette},current=dark?palette.dark:palette.light,theme=nativeLookTheme(look,style,dark).colors,bubble=nativeMessageTheme(look,style,true,dark).colors;assert.equal(theme.fontEmphasize,current.emphasize);assert.equal(theme.compBackgroundEmphasize,current.brand);assert.equal(bubble.compBackgroundPrimary,current.userBubble);assert.equal(bubble.fontPrimary,current.userBubbleText);assert.ok(contrastRatio(bubble.compBackgroundPrimary,bubble.fontPrimary)>=4.5);assert.deepEqual(nativeMessageTheme(look,style,false,dark),{});});
