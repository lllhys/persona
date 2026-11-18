import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {stripTypeScriptTypes} from 'node:module';
const source=readFileSync(new URL('../entry/src/main/ets/components/KnowledgeRecordingView.ets',import.meta.url),'utf8');
function method(name){const start=source.indexOf('  private '+name+'(');assert.ok(start>=0);const body=source.indexOf('{',start);let depth=1,quote='',escape=false,i=body+1;for(;i<source.length&&depth;i++){const c=source[i];if(quote){if(escape)escape=false;else if(c==='\\')escape=true;else if(c===quote)quote='';}else if(c==='"'||c==="'")quote=c;else if(c==='{')depth++;else if(c==='}')depth--;}return source.slice(start,i);}
const Harness=new Function('tr','return '+stripTypeScriptTypes('class Harness{'+['transcribeLabel','confirmTranscription'].map(method).join('\n')+'}'))(k=>k);
test('recording consent is explicit and cannot retarget another recording or owner',()=>{
 for(const change of ['cancel','owner','entry','blocked','confirm']){
  let calls=0,dialog;const owner={entry:{id:'synthetic-recording'},transcriptionState:'idle',allowed:true,canTranscribe(){return this.allowed;},transcribeAndUpload(){calls++;return Promise.resolve(true);}};
  const host=new Harness();Object.assign(host,{model:owner,getUIContext:()=>({showAlertDialog:d=>dialog=d})});host.confirmTranscription();
  assert.equal(calls,0);assert.equal(dialog.message,'knowledge_recording_save_notice');
  if(change==='cancel')dialog.primaryButton.action();else{if(change==='owner')host.model={...owner};if(change==='entry')owner.entry={id:'another'};if(change==='blocked')owner.allowed=false;dialog.secondaryButton.action();}
  assert.equal(calls,change==='confirm'?1:0,change);
 }
 for(const locale of ['base','en_US','zh_CN','ja_JP']){
  const strings=JSON.parse(readFileSync(new URL('../entry/src/main/resources/'+locale+'/element/string.json',import.meta.url),'utf8')).string;
  for(const key of ['knowledge_recording_transcribe','knowledge_recording_retry_transcribe','knowledge_recording_save_notice','forget_hub_notice'])assert.ok(strings.find(item=>item.name===key)?.value,key+' '+locale);
 }
});
