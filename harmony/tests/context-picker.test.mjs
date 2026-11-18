import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {stripTypeScriptTypes,registerHooks} from 'node:module';
registerHooks({resolve(s,c,n){if(c.parentURL?.endsWith('.ets')&&s==='./Wire')return{url:new URL('./Wire.ets',c.parentURL).href,shortCircuit:true};return n(s,c);},load(u,c,n){if(u.endsWith('/Wire.ets'))return{format:'module',shortCircuit:true,source:stripTypeScriptTypes(readFileSync(new URL(u),'utf8').replace(/@ObservedV2\s*/g,'').replace(/@Trace\s+/g,''))};return n(u,c);}});
const {ResourceRow,Wire}=await import('../entry/src/main/ets/models/Wire.ets');
const source=readFileSync(new URL('../entry/src/main/ets/components/ContextPicker.ets',import.meta.url),'utf8');
function method(name){const m=(name==='ownerChanged'?/\bownerChanged\(/:new RegExp('  private (?:async )?'+name+'\\(')).exec(source);assert.ok(m);const start=m.index,body=source.indexOf('{',start);let depth=1,quote='',escape=false,i=body+1;for(;i<source.length&&depth;i++){const c=source[i];if(quote){if(escape)escape=false;else if(c==='\\')escape=true;else if(c===quote)quote='';}else if(c==='"'||c==="'"||c==='`')quote=c;else if(c==='{')depth++;else if(c==='}')depth--;}return source.slice(start,i);}
class Failure extends Error{}
const Picker=new Function('ResourceRow','Wire','segment','KNOWLEDGE_KINDS','HttpFailure','return '+stripTypeScriptTypes('class Picker {'+['current','sameRepository','load','loadKnowledge','visibleRows','ownerChanged'].map(method).join('\n')+'}'))(ResourceRow,Wire,encodeURIComponent,['projects','chats','people','docs','meetings','events','recordings'],Failure);
function fixture(repo){const p=new Picker();let owner='work';p.model={connected:true,owns:()=>true,target:()=> 'chat',profile:{id:'hub',manager:'work'},repo:()=>owner==='work'?repo:undefined};Object.assign(p,{mounted:true,active:true,target:'chat',scope:'scope',generation:1,repository:repo,kind:'knowledge',knowledgeKind:'projects',query:'',revision:0,rows:[]});return {p,retire:()=>owner='other'};}
test('knowledge search resolves cross-category entries with their canonical identity',async()=>{
 const paths=[];const {p}=fixture({async call(path){paths.push(path);return{results:[{entry:{id:'same',kind:'docs',name:'API reference',manager:'work'}},{entry:{id:'same',kind:'projects',name:'Project',manager:'work'}}]};}});
 p.query='reference';await p.load();assert.equal(paths[0],'/api/v1/kb-search?q=reference&results=1');assert.deepEqual(p.rows.map(r=>r.path),['docs','projects']);assert.equal(p.visibleRows().length,2,'server full-text hits are not discarded by a title-only filter');
});
test('empty knowledge queries use the chosen native category and late retired responses cannot publish',async()=>{
 let release;const gate=new Promise(r=>release=r),kinds=[];const {p,retire}=fixture({async knowledge(kind){kinds.push(kind);return gate;}});p.knowledgeKind='meetings';const pending=p.load();retire();release([new ResourceRow('private','Private','knowledge')]);await pending;assert.deepEqual(kinds,['meetings']);assert.deepEqual(p.rows,[]);assert.equal(p.current(),false);
});
test('a same-owner conversation change retires each context picker and rejects its late rows',async()=>{
 const clientSource=readFileSync(new URL('../entry/src/main/ets/models/ClientModel.ets',import.meta.url),'utf8');
 const target=clientSource.match(/  target\(row: ResourceRow = this.selected\): string \{[^\n]*\}/)[0];
 const Owner=new Function('return '+stripTypeScriptTypes('class Owner {'+target+'}'))();
 const click=new Function('row',source.match(/\.onClick\(\(\) => \{([^\n{}]*)\}\)\.id\('context\./)[1]);
 for(const kind of ['todo','inbox','knowledge']){
  let release,retired=0;const gate=new Promise(resolve=>release=resolve);
  const repository={todos:()=>gate,inbox:()=>gate,knowledge:()=>gate}, {p}=fixture(repository);
  p.model.selected=new ResourceRow('chat-one','First','conversation');p.model.selected.machine='machine';
  p.model.scope=()=> 'hub/work';p.model.target=Owner.prototype.target;p.target=p.model.target();
  p.kind=kind;p.onRetired=()=>retired++;
  const staged=[],row=new ResourceRow('reference','Reference',kind);row.manager='work';
  p.onSelect=value=>staged.push([p.model.target(),value]);p.rows=[row];
  click.call(p,row);assert.equal(staged.length,1);
  const old=p.load();assert.equal(p.current(),true);
  p.model.selected=new ResourceRow('chat-two','Second','conversation');p.model.selected.machine='machine';
  assert.equal(p.current(),false,'a row click cannot select after its captured conversation changes');
  click.call(p,row);assert.equal(staged.length,1,'queued native click cannot stage into the new conversation');
  p.ownerChanged();assert.equal(retired,1);assert.deepEqual(p.rows,[]);
  release([row]);await old;assert.deepEqual(p.rows,[]);assert.equal(retired,1);
 }
});


test('leaving a loading inbox handoff revokes its one-shot opening before a late read completes',()=>{
 const source=readFileSync(new URL('../entry/src/main/ets/components/InboxWorkspacePage.ets',import.meta.url),'utf8');
 const consume=source.match(/  private consumeHandoff\(\): void \{[\s\S]*?\n  \}/)[0];
 const changed=source.match(/handoffRequestedChanged\(\): void \{[^\n]*\}/)[0];
 const Page=new Function('return '+stripTypeScriptTypes('class Page{'+consume+'\n'+changed+'}'))(),p=new Page();let opens=0,consumed=0;
 const row=new ResourceRow('item','Item','inbox');Object.assign(p,{row,workspace:{selected:row,isReference:()=>false},active:true,visible:true,handoffRequested:true,pendingHandoff:true,handoffLoaded:false,onHandoffConsumed(){consumed++;},openHandoff(){opens++;}});
 p.handoffRequested=false;p.handoffRequestedChanged();p.handoffLoaded=true;p.consumeHandoff();assert.equal(opens,0);assert.equal(consumed,0);
 p.handoffRequested=true;p.handoffRequestedChanged();assert.equal(opens,1);assert.equal(consumed,1);p.consumeHandoff();assert.equal(opens,1);
});
