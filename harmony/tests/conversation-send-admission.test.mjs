import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {stripTypeScriptTypes} from 'node:module';

const source=readFileSync(new URL('../entry/src/main/ets/components/Conversation.ets',import.meta.url),'utf8');
function method(name){
 const match=new RegExp('  private (?:async )?'+name+'\\(').exec(source);assert.ok(match,name);
 const start=match.index,body=source.indexOf('{',start);let depth=1,quote='',escaped=false,end=body+1;
 for(;end<source.length&&depth;end++){
  const c=source[end];if(quote){if(escaped)escaped=false;else if(c==='\\')escaped=true;else if(c===quote)quote='';}
  else if(c==='"'||c==="'"||c==='`')quote=c;else if(c==='{')depth++;else if(c==='}')depth--;
 }
 assert.equal(depth,0,name);return source.slice(start,end);
}
const Composer=new Function('return '+stripTypeScriptTypes('class Composer {'+['sendCurrent','sendDraft'].map(method).join('\n')+'}'))();
function deferred(){let resolve;const promise=new Promise(done=>resolve=done);return {promise,resolve};}
function fixture(){
 const composer=new Composer();let writes=0,generation=1,target='original',admitted=true;
 const model={draft:'Keep draft',sourceContext:'Keep context',canSend:()=>true,scope:()=> 'hub/manager',connectionGeneration:()=>generation,
  target:()=>target,owns:(g,s)=>g===generation&&s==='hub/manager',prepareMessageText:text=>text,
  async send(admit){if(!admit||admit())writes++;},async sendPrepared(_prepared,_draft,_context,admit){if(!admit||admit())writes++;}};
 Object.assign(composer,{model,active:true,mounted:true,sending:false,admission:()=>admitted,
  attachments:{attachments:[],busy:false,async prepareSend(){return {};}}});
 return {composer,model,get writes(){return writes;},retire:()=>admitted=false,bump:()=>generation++,move:()=>target='new-target'};
}
for(const reason of ['inactive','unmounted','route'])test('a '+reason+' composer rejects a queued send callback',async()=>{
 const f=fixture();if(reason==='inactive')f.composer.active=false;else if(reason==='unmounted')f.composer.mounted=false;else f.retire();
 await f.composer.sendDraft();assert.equal(f.writes,0);assert.equal(f.model.draft,'Keep draft');
});
for(const reason of ['inactive','route','generation','target','owner'])test('attachment preparation cannot send after '+reason+' changes',async()=>{
 const f=fixture(),gate=deferred(),entered=deferred();f.composer.attachments.attachments=[{id:'file'}];
 f.composer.attachments.prepareSend=async()=>{entered.resolve();return gate.promise;};
 const sending=f.composer.sendDraft();await entered.promise;
 if(reason==='inactive')f.composer.active=false;else if(reason==='route')f.retire();else if(reason==='generation')f.bump();
 else if(reason==='target')f.move();else f.composer.model={...f.model};
 gate.resolve({});await sending;assert.equal(f.writes,0);assert.equal(f.composer.sending,false);
 assert.equal(f.model.draft,'Keep draft');assert.equal(f.model.sourceContext,'Keep context');
});
test('an active current composer can send either plain text or prepared attachments once',async()=>{
 for(const attachments of [[],[{id:'file'}]]){const f=fixture();f.composer.attachments.attachments=attachments;
  await f.composer.sendDraft();assert.equal(f.writes,1);assert.equal(f.composer.sending,false);}
});
