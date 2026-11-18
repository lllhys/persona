import test from 'node:test';
import assert from 'node:assert/strict';
import {mainConversationData} from '../entry/src/main/ets/core/DefaultConversation.ts';
import {readFileSync} from 'node:fs';
import {registerHooks,stripTypeScriptTypes} from 'node:module';
registerHooks({resolve(s,c,n){if(c.parentURL?.endsWith('NavigationContext.ets')&&s==='./Wire')return {url:new URL('./Wire.ets',c.parentURL).href,shortCircuit:true};return n(s,c);},load(u,c,n){if(u.endsWith('.ets'))return{format:'module',shortCircuit:true,source:stripTypeScriptTypes(readFileSync(new URL(u),'utf8').replace(/@ObservedV2\s*/g,'').replace(/@Trace\s+/g,''))};return n(u,c);}});
const {NavigationContext}=await import('../entry/src/main/ets/models/NavigationContext.ets');
const {Tokens}=await import('../entry/src/main/ets/design/Tokens.ets');
const {ResourceRow,Wire}=await import('../entry/src/main/ets/models/Wire.ets');
// Load the real shell's navigation methods, excluding only ArkUI builders and platform lifecycle surfaces.
const source=readFileSync(new URL('../entry/src/main/ets/pages/Index.ets',import.meta.url),'utf8');
function method(name){
 const start=source.indexOf('  private ',source.indexOf(`  private ${name}(`)>=0?source.indexOf(`  private ${name}(`):source.indexOf(`  private async ${name}(`));
 assert.ok(start>=0,name);const body=source.indexOf('{',start);let depth=1,quote='',escape=false,i=body+1;
 for(;i<source.length&&depth;i++){const c=source[i];if(quote){if(escape)escape=false;else if(c==='\\')escape=true;else if(c===quote)quote='';}else if(c==='"'||c==="'"||c==='`')quote=c;else if(c==='{')depth++;else if(c==='}')depth--;}
 assert.equal(depth,0,name);return source.slice(start,i);
}
const names=['consumeNotification','destinationBackHidden','chatSelectionShown','activeModule','readingAgentDetail','routeTarget','openRow','mainDestination','openMain','hideChatTitle','newConversation','resolveMainDestination','ensureMainDestination','pushChat','redeem','childKey','routeRow','openSettings','settingsKind','settingsModel','targetReady','openLinkedResource','openFrom','openComputer','handoffResource','restoreTarget','routeShown','dismissChatActions','markDetailReady'];
const NativeNavigation=new Function('NavigationContext','ResourceRow','mainConversationData','Wire','Tokens','return '+stripTypeScriptTypes('class NativeNavigation {'+names.map(method).join('\n')+'}'))(NavigationContext,ResourceRow,mainConversationData,Wire,Tokens);
function row(id='source',manager='owner-b',kind='conversation'){const r=new ResourceRow(id,id,kind);r.manager=manager;r.machine='machine';r.session=id;return r;}
function deferred(){let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve};}
function fixture(){const shell=new NativeNavigation(),pushed=[];let gen=1;
 const primary={profile:{id:'hub'},selected:row('primary','owner-a'),connected:true,error:'',connectionGeneration:()=>gen,scope:()=> 'hub/owner-a',owns:(g,s)=>g===gen&&s==='hub/owner-a',reportError(e){this.error=e.message;},hideConversation(){},async listResources(){}};
 const detail={selected:row('previous'),connected:true,scope:()=> 'hub/owner-b',connectionGeneration:()=>gen,async select(r){this.selected=r;},hideConversation(){},showConversation(){},watchDecisions(){}};
 primary.viewFor=async()=>detail;shell.mainReadWaiters=[];shell.model=primary;shell.detailModel=primary;shell.activeChild='';shell.routeRevision=0;shell.creatingConversations=new Map();shell.chatReady=false;shell.detailRoute='';shell.stack={size:()=>pushed.length,clear:()=>pushed.splice(0),pushPath:r=>pushed.push(r),getAllPathName:()=>pushed.map(r=>r.name),getParamByIndex:i=>pushed[i]?.param};shell.savePath=()=>{};
 return {shell,primary,detail,pushed,bump:()=>gen++};
}
test('global settings remain workspace-owned and chat settings retain the captured target',()=>{
 const {shell,detail,primary,pushed}=fixture();shell.detailModel=detail;shell.openSettings('models');shell.openSettings('models',true);
 assert.equal(pushed[0].param,'models');assert.equal(shell.settingsModel(pushed[0].param),primary);assert.equal(shell.settingsModel(pushed[1].param),detail);
 const route=NavigationContext.from('settings',pushed[1].param);assert.equal(route.page,'models');assert.equal(route.row.key(),detail.selected.key());
});
test('tools and history restore their serialized owner and session before becoming interactive',async()=>{
 for(const name of ['tools','history','resource','inbox']){const {shell,detail}=fixture(),target=row(name,'owner-b',name==='resource'?'todo':'conversation'),param=JSON.stringify(target);shell.activeChild=shell.childKey(name,param);await shell.restoreTarget(name,param);assert.equal(detail.selected.key(),target.key());assert.equal(shell.detailModel,detail);assert.equal(shell.targetReady(name,param),true);}
});
test('linked inbox and todo cards retain dedicated destination kinds and owner data',async()=>{
 for(const kind of ['inbox','todo','knowledge']){const {shell,pushed,primary}=fixture(),target=row(kind,'owner-b',kind);primary.viewFor=async()=>{throw new Error('must not select a chat for resource navigation');};await shell.openLinkedResource(target);assert.equal(pushed[0].name,kind);assert.equal(NavigationContext.from(kind,pushed[0].param).row.manager,'owner-b');assert.equal(primary.error,'');}
});
test('a delayed owner reconnect is rebound after the primary connection generation changes',async()=>{
 const {shell,primary,detail,bump}=fixture(),target=row(),param=JSON.stringify(target),wait=deferred();shell.activeChild=shell.childKey('conversation',param);primary.viewFor=()=>wait.promise;
 const old=shell.restoreTarget('conversation',param);bump();wait.resolve(detail);await old;assert.equal(shell.chatReady,false);
 await shell.restoreTarget('conversation',param);assert.equal(shell.targetReady('conversation',param),true);assert.equal(detail.selected.id,target.id);
});
test('late route restoration and handoff cannot navigate or expose a replaced tab',async()=>{
 const {shell,primary,detail,pushed}=fixture(),target=row(),param=JSON.stringify(target),wait=deferred();shell.activeChild=shell.childKey('tools',param);primary.viewFor=()=>wait.promise;
 const restoring=shell.restoreTarget('tools',param);shell.activeChild='different-tab';wait.resolve(detail);await restoring;assert.equal(shell.chatReady,false);assert.notEqual(detail.selected.id,target.id);
 const handoff=deferred();primary.handoff=()=>handoff.promise;const sending=shell.handoffResource(row('todo','owner-b','todo'));shell.activeChild='new-route';handoff.resolve(target);await sending;assert.equal(pushed.length,0);
});
test('returning from a native picker to the same resolved destination preserves its mounted content',async()=>{
 const {shell,primary,detail,bump}=fixture(),target=detail.selected,param=JSON.stringify(target),gate=deferred();shell.activeChild=shell.childKey('conversation',param);shell.detailRoute=shell.activeChild;shell.chatReady=true;shell.detailModel=detail;shell.markDetailReady(shell.activeChild);primary.viewFor=()=>gate.promise;bump();
 const returning=shell.restoreTarget('conversation',param);assert.equal(shell.chatReady,true);assert.equal(shell.detailRoute,shell.activeChild);gate.resolve(detail);await returning;assert.equal(shell.targetReady('conversation',param),true);
});
test('a Knowledge source remains mounted while handoff selects the destination chat',async()=>{
 const {shell,primary,detail,pushed}=fixture(),source=row('knowledge','owner-b','knowledge'),param=JSON.stringify(source);
 shell.activeChild=shell.childKey('knowledge',param);await shell.restoreTarget('knowledge',param);
 assert.equal(shell.targetReady('knowledge',param),true);
 const main=row('main','owner-b');detail.selected=main;
 assert.equal(shell.targetReady('knowledge',param),true,'source callback can complete before its chat route is pushed');
 await shell.openLinkedResource(main);assert.equal(pushed.at(-1).name,'conversation');
 assert.equal(shell.targetReady('conversation',pushed.at(-1).param),false);
});

test('callbacks from a hidden workspace cannot open a destination in the new route', async()=>{
 const {shell,pushed}=fixture(),param='todo',target=row('linked','owner-b','todo');
 shell.activeChild=shell.childKey('resources',param);shell.openFrom('resources',param,target);
 await new Promise(resolve=>setImmediate(resolve));assert.equal(pushed.length,1);
 shell.activeChild='another-tab';shell.openFrom('resources',param,target);
 await new Promise(resolve=>setImmediate(resolve));assert.equal(pushed.length,1);
});
test('computer routes capture the machine owner and restore independently of chat selection',async()=>{
 const {shell,detail,pushed}=fixture(),machine=row('mac','owner-b','machine');
 shell.openComputer(machine);shell.openComputer(machine);assert.equal(pushed.length,1);assert.equal(pushed[0].name,'computer');
 assert.equal(NavigationContext.from('computer',pushed[0].param).row.manager,'owner-b');
 shell.activeChild=shell.childKey('computer',pushed[0].param);
 await shell.restoreTarget('computer',pushed[0].param);
 assert.equal(detail.selected.kind,'machine');assert.equal(shell.targetReady('computer',pushed[0].param),true);
});

test('a late pairing UI completion does not clear a newly opened connection form or its navigation',async()=>{const {shell,primary}=fixture(),gate=deferred();shell.pairingRevision=1;shell.address='first-link';shell.pairCode='first-code';shell.showSheet=true;primary.pair=()=>gate.promise;const completing=shell.redeem({}, {hubId:'paired-a'},()=>false);shell.pairingRevision=2;shell.address='new-link';shell.pairCode='new-code';primary.profile.id='chosen-c';gate.resolve();await completing;assert.equal(shell.address,'new-link');assert.equal(shell.pairCode,'new-code');assert.equal(shell.showSheet,true);assert.equal(primary.profile.id,'chosen-c');});

test('pairing completion cannot dismiss a later non-connect sheet in the same selected Hub',async()=>{const {shell,primary}=fixture(),gate=deferred();shell.pairingRevision=1;shell.sheetKind='connect';shell.address='confirmed-link';shell.pairCode='confirmed-code';shell.showSheet=false;primary.profile.id='paired-a';primary.pair=()=>gate.promise;const old=shell.redeem({}, {hubId:'paired-a'},()=>true);shell.sheetKind='hub';shell.showSheet=true;gate.resolve();await old;assert.equal(shell.sheetKind,'hub');assert.equal(shell.showSheet,true);assert.equal(shell.address,'confirmed-link');});


test('native extension pushes have distinct route identities and an eight-page limit',()=>{
 const NativeExtensions = new Function('ResourceRow','Wire','util','tr','return '+stripTypeScriptTypes('class NativeExtensions {'+method('openExtension')+'}'))(ResourceRow,{payload:(keys,values)=>Object.fromEntries(keys.map((key,i)=>[key,values[i]]))},{generateRandomUUID:(()=>{let id=0;return()=>String(++id);})()},value=>value);
 const shell=new NativeExtensions(),pushed=[],owner={profile:{id:'hub',manager:'owner'}};let warnings=0;
 shell.stack={getAllPathName:()=>pushed.map(row=>row.name),pushPath:row=>pushed.push(row)};shell.savePath=()=>{};shell.getUIContext=()=>({getPromptAction:()=>({showToast:()=>warnings++})});
 for(let i=0;i<10;i++)shell.openExtension(owner,'extension','same-page','Title');
 assert.equal(pushed.length,8);assert.equal(warnings,2);assert.equal(new Set(pushed.map(row=>row.param)).size,8);
 for(const route of pushed){const row=NavigationContext.rowFrom(route.param);assert.equal(row.group,'hub');assert.equal(row.manager,'owner');assert.equal(row.data().ui,'same-page');}
});


test('the default main read cannot steal a same-owner navigation opened while it is pending',async()=>{
 const {shell,primary,pushed}=fixture(),wait=deferred();let posts=0;
 const repo={call:async(path,method)=>{assert.equal(path,'/api/v1/assistant');if(method==='POST')posts++;return wait.promise;}};
 primary.repo=()=>repo;primary.section=0;shell.defaultMainPending=false;shell.mainReadWaiters=[];
 const selected=primary.selected,pending=shell.resolveMainDestination();
 shell.stack.pushPath({name:'resources',param:'knowledge'});
 wait.resolve({main:'main',machine_id:'machine',conversations:[{id:'main'}]});await pending;
 assert.equal(posts,0);assert.equal(primary.selected,selected);assert.deepEqual(pushed,[{name:'resources',param:'knowledge'}]);
});
test('a default main read uses the same response after a main conversation moves to another machine',async()=>{
 const {shell,primary,pushed}=fixture();let calls=0;
 const repo={async call(path,method){calls++;assert.equal(path,'/api/v1/assistant');assert.notEqual(method,'POST');return {main:'main',machine_id:'old',main_machine_id:'new',conversations:[{id:'main',machine_id:'old'},{id:'main',machine_id:'new'}]};}};
 primary.repo=()=>repo;primary.section=0;primary.mainId='cached';shell.defaultMainPending=false;shell.mainReadWaiters=[];
 await shell.resolveMainDestination();assert.equal(calls,1);assert.equal(pushed.length,1);assert.equal(JSON.parse(pushed[0].param).machine,'new');assert.equal(primary.mainId,'main');
});
test('opening main gates its composer until the shown route selects the resolved conversation',async()=>{
 const {shell,primary,pushed}=fixture(),selection=deferred(),entered=deferred();
 shell.chatMenuRevision=0;
 primary.profile.manager='owner-a';primary.section=0;primary.watchDecisions=()=>{};
 primary.viewFor=async()=>primary;
 primary.repo=()=>repository;const repository={async call(){return {main:'main',machine_id:'machine',conversations:[{id:'main'}]};}};
 const original=primary.selected;primary.draft='previous conversation draft';
 primary.select=async row=>{entered.resolve();await selection.promise;primary.selected=row;primary.draft='main conversation draft';};
 const oldParam=JSON.stringify(original);shell.activeChild=shell.childKey('conversation',oldParam);
 shell.markDetailReady(shell.activeChild);shell.defaultMainPending=false;
 await shell.resolveMainDestination();const route=pushed.at(-1);
 assert.equal(route.name,'conversation');assert.equal(primary.selected,original);
 assert.equal(shell.targetReady(route.name,route.param),false,'new destination cannot render the previous composer');
 shell.routeShown(route.name,route.param);await entered.promise;
 assert.equal(shell.targetReady(route.name,route.param),false,'pending selection keeps the destination gated');
 assert.equal(primary.draft,'previous conversation draft');
 selection.resolve();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(primary.selected.id,'main');assert.equal(primary.draft,'main conversation draft');
 assert.equal(shell.targetReady(route.name,route.param),true);assert.equal(shell.detailModel,primary);
});
test('a newly selected chat cannot reuse the previous route readiness while creation finishes',async()=>{
 const {shell,primary}=fixture(),done=deferred(),entered=deferred();
 const original=primary.selected,param=JSON.stringify(original);
 shell.activeChild=shell.childKey('conversation',param);shell.detailRoute=shell.activeChild;shell.chatReady=true;
 shell.detailOwner=primary;shell.detailSelection=JSON.stringify([primary.scope(),primary.selected.key()]);
 primary.newConversation=async()=>{primary.selected=row('new-chat','owner-a');entered.resolve();await done.promise;return primary.selected;};
 shell.newConversation();await entered.promise;
 assert.equal(shell.targetReady('conversation',param),false);
 done.resolve();await new Promise(resolve=>setImmediate(resolve));
});
test('unavailable and empty default directories never create or select a conversation',async()=>{
 for(const failure of [true,false]){
  const {shell,primary,pushed}=fixture();let calls=0;const selected=primary.selected;
  primary.repo=()=>repo;primary.section=0;shell.defaultMainPending=false;shell.mainReadWaiters=[];
  const repo={async call(path,method){calls++;assert.notEqual(method,'POST');if(failure)throw Error('offline');return {main:'absent',machine_id:'machine',conversations:[]};}};
  await shell.resolveMainDestination();assert.equal(calls,1);assert.equal(pushed.length,0);assert.equal(primary.selected,selected);
 }
});


test('an explicit new conversation retires a pending default read without starting another one', async () => {
 const {shell,primary,pushed}=fixture(), directory=deferred(), creation=deferred();let reads=0;
 const repo={async call(){reads++;return directory.promise;}};primary.repo=()=>repo;primary.section=0;primary.newConversation=()=>creation.promise;
 shell.navigationIntent=0;shell.defaultMainPending=false;shell.mainReadWaiters=[];shell.navigationRestoredScope=primary.scope();
 const pending=shell.resolveMainDestination();shell.newConversation();
 directory.resolve({main:'main',machine_id:'machine',conversations:[{id:'main'}]});await pending;
 assert.equal(reads,1);assert.equal(pushed.length,0);
 primary.ready=true;primary.mainId='now-known';shell.defaultMainAttempt='';shell.ensureMainDestination();assert.equal(reads,1,'inventory changes during explicit create cannot reopen main');
 creation.resolve(row('created','owner-a'));await new Promise(resolve=>setImmediate(resolve));
 assert.equal(pushed.length,1);assert.equal(JSON.parse(pushed[0].param).id,'created');
});

test('opening assistant detail restores decision authorization for its captured conversation',async()=>{
 const {shell,detail}=fixture(),target=detail.selected,param=JSON.stringify(target);let watching=false;
 detail.watchDecisions=value=>watching=value;shell.detailModel=detail;shell.activeChild=shell.childKey('assistant',param);
 await shell.restoreTarget('assistant',param);assert.equal(watching,true);assert.equal(shell.targetReady('assistant',param),true);
});

test('a new owner receives a fresh stack before async hydration and merges only its own new intent',async()=>{
 class Stack { routes=[];size(){return this.routes.length;}pushPath(route){this.routes.push(route);}getAllPathName(){return this.routes.map(r=>r.name);}getParamByIndex(i){return this.routes[i]?.param;} }
 const NativeScope=new Function('Wire','NavPathStack','NavigationContext','return '+stripTypeScriptTypes('class NativeScope {'+['routeRow','restoreScope','savePath','changeSection'].map(method).join('\n')+'}'))(Wire,Stack,NavigationContext);
 const s=new NativeScope(),gate=deferred(),saved=[];s.model={section:0,scope:()=> 'B',loadView:()=>gate.promise,saveView:async(k,v)=>saved.push(JSON.parse(v)),releaseTerminal(){},sessions:[]};
 s.stack=new Stack();s.stack.pushPath({name:'conversation',param:'private-A'});s.paths=new Map();s.activeScope='A';s.restoring=0;s.navigationIntent=0;s.rowVisibility=new Map();s.routeRevision=0;s.detailModel={hideConversation(){}};s.wide=()=>false;s.ensureMainDestination=()=>{};
 const restoring=s.restoreScope();assert.equal(s.stack.size(),0);
 s.changeSection(1);s.stack.pushPath({name:'session',param:'B-notification'});s.savePath();assert.equal(saved.length,0);
 gate.resolve(JSON.stringify({section:0,tabs:[{index:0,routes:[{name:'conversation',param:JSON.stringify(row('saved-B','owner-b'))}]}]}));await restoring;
 assert.equal(s.model.section,1);assert.deepEqual(s.stack.routes,[{name:'session',param:'B-notification'}]);
 assert.equal(JSON.stringify(saved).includes('private-A'),false);assert.equal(JSON.stringify(saved).includes('saved-B'),true);
});


test('an old owner creation does not suppress the new owners automatic read',async()=>{
 const {shell,primary}=fixture(),create=deferred();primary.newConversation=()=>create.promise;shell.navigationIntent=0;shell.newConversation();
 primary.scope=()=> 'hub/owner-b';primary.section=0;primary.ready=true;primary.mainId='main-b';primary.mainMachineId='machine';
 primary.owns=(g,s)=>g===primary.connectionGeneration()&&s===primary.scope();let reads=0;primary.repo=()=>({async call(){reads++;return {main:'main-b',machine_id:'machine',conversations:[{id:'main-b'}]};}});
 shell.navigationRestoredScope=primary.scope();shell.defaultMainPending=false;shell.mainReadWaiters=[];shell.ensureMainDestination();await new Promise(r=>setImmediate(r));assert.equal(reads,1);
 create.resolve(row('created-in-A','owner-a'));await new Promise(r=>setImmediate(r));assert.equal(shell.creatingConversations.size,0);
});


test('main reached from a source destination retains its child navigation identity',()=>{
 const {shell,primary}=fixture(),main=row('main','owner-a');main.group='hub';primary.profile.manager='owner-a';primary.mainId='main';primary.mainMachineId='machine';
 assert.equal(shell.mainDestination('conversation',JSON.stringify(main)),false,'source handoff is a child even though IDs match main');
 main.raw=JSON.stringify({navigation_root:true});assert.equal(shell.mainDestination('conversation',JSON.stringify(main)),true);
 assert.equal(shell.mainDestination('assistant',JSON.stringify(main)),false);
});


test('root selection and source links serialize independent route-local copies',async()=>{
 const {shell,primary,detail,pushed}=fixture(),inventory=row('shared','owner-a','session');inventory.raw=JSON.stringify({capabilities:{send:true}});const original=inventory.raw;
 shell.openRow(inventory);assert.equal(inventory.raw,original);assert.equal(JSON.parse(JSON.parse(pushed[0].param).raw).navigation_root,true);
 const previouslyRoot=NavigationContext.rowFrom(pushed[0].param);shell.activeChild='source-todo';primary.viewFor=async()=>detail;
 await shell.openLinkedResource(previouslyRoot);assert.equal(JSON.parse(JSON.parse(pushed[1].param).raw).navigation_root,false);assert.equal(previouslyRoot.data().navigation_root,true);assert.equal(inventory.raw,original);
});

test('conversation navigation preserves Back when a main notification pushes over an existing route',()=>{
 const {shell,primary,pushed}=fixture(),main=row('main','owner-a');main.group='hub';primary.profile.manager='owner-a';primary.mainId='main';primary.mainMachineId='machine';
 const original=main.raw;
 shell.wide=()=>true;shell.rail=()=>false;
 shell.openRow(main);assert.equal(shell.mainDestination('conversation',pushed[0].param),true,'an empty stack admits a root');
 primary.sessions=[];primary.conversations=[main];primary.notificationTarget=JSON.stringify(main);shell.changeSection=()=>{};
 pushed.push({name:'knowledge',param:'source'});shell.consumeNotification();
 assert.equal(pushed.length,3);assert.equal(shell.mainDestination('conversation',pushed[2].param),false,'a pushed notification remains a child with Back');
 assert.equal(shell.destinationBackHidden('conversation',pushed[2].param),false);
 assert.equal(main.raw,original,'route metadata never mutates the inventory row');
 const agent=row('agent','owner-a','session');shell.openRow(agent);
 assert.equal(pushed.length,1);assert.equal(JSON.parse(JSON.parse(pushed[0].param).raw).navigation_root,true,'Agent selection still replaces its stack');
});

test('normal secondary-conversation sidebar selection retains its split root title semantics',()=>{
 const {shell,primary,pushed}=fixture(),secondary=row('secondary','owner-a');
 primary.mainId='main';primary.mainMachineId='machine';primary.selected=secondary;primary.target=r=>(r||primary.selected).id;
 shell.rowVisibility=new Map([[primary.scope()+primary.target(secondary),1]]);shell.visibilityEpoch=0;shell.peer=()=>0;shell.wide=()=>true;shell.rail=()=>false;
 pushed.push({name:'conversation',param:JSON.stringify(row('main','owner-a'))});shell.openRow(secondary);
 assert.equal(secondary.data().navigation_root,undefined);assert.equal(shell.hideChatTitle('conversation',pushed[1].param),true);
 assert.equal(JSON.parse(JSON.parse(pushed[1].param).raw).navigation_root,true);
});


test('native chrome accepts global destination parameters without parsing them as chat rows',()=>{
 const {shell}=fixture();
 for(const [name,param] of [['resources','inbox'],['resources','todo'],['settings','look'],['applications',''],['resources','knowledge']]){
  assert.equal(shell.mainDestination(name,param),false);assert.equal(shell.hideChatTitle(name,param),false);
 }
});


test('main row visibility survives an authoritative main identity arriving after layout',()=>{
 const {shell,primary}=fixture(),r=row('main','owner-a');r.raw=JSON.stringify({navigation_root:true});primary.mainId='main';primary.mainMachineId='machine';primary.target=x=>(x||r).id;
 shell.mainRowVisible=1;shell.visibilityEpoch=0;shell.rowVisibility=new Map();shell.peer=()=>0;shell.wide=()=>true;shell.rail=()=>false;assert.equal(shell.hideChatTitle('conversation',JSON.stringify(r)),true);
 shell.mainRowVisible=0;assert.equal(shell.hideChatTitle('conversation',JSON.stringify(r)),false);
});


test('inbox handoff opening is a transient route intent and never part of saved navigation',async()=>{
 const {shell,primary,pushed}=fixture(),r=row('inbox-intent','owner-a','inbox');r.raw=JSON.stringify({open_handoff:true});await shell.openLinkedResource(r);
 const target=pushed[0];assert.equal(JSON.parse(JSON.parse(target.param).raw).open_handoff,false);assert.equal(shell.pendingInboxHandoff,shell.childKey(target.name,target.param));assert.equal(r.data().open_handoff,true);
});


test('tapping main during its initial read waits and fulfills the latest explicit navigation once',async()=>{
 const {shell,primary,pushed}=fixture(),first=deferred();let reads=0;primary.section=0;primary.repo=()=>repo;const repo={async call(){reads++;return reads===1?first.promise:{main:'main',machine_id:'machine',conversations:[{id:'main'}]};}};shell.navigationIntent=0;shell.defaultMainPending=false;
 const background=shell.resolveMainDestination(),tap=shell.openMain();first.resolve({main:'main',machine_id:'machine',conversations:[{id:'main'}]});await background;await tap;assert.equal(reads,2);assert.equal(pushed.length,1);
});

test('leaving before a queued main read finishes retires the explicit destination request',async()=>{
 const {shell,primary,pushed}=fixture(),first=deferred();let reads=0;primary.section=0;primary.repo=()=>({async call(){reads++;return first.promise;}});shell.navigationIntent=0;shell.defaultMainPending=false;
 const background=shell.resolveMainDestination(),tap=shell.openMain();shell.routeRevision++;first.resolve({main:'main',machine_id:'machine',conversations:[{id:'main'}]});await background;await tap;assert.equal(reads,1);assert.equal(pushed.length,0);
});

test('reading an Agent retains its queue across native background visibility, and releases on true exit',()=>{
 const {shell,primary,pushed}=fixture();primary.section=1;primary.selected=row('agent','owner-a','session');shell.activeChild='shown-session';pushed.push({name:'session'});assert.equal(shell.readingAgentDetail(),true);
 shell.activeChild='';assert.equal(shell.readingAgentDetail(),true);pushed.push({name:'history'});assert.equal(shell.readingAgentDetail(),true);
 pushed.splice(0);assert.equal(shell.readingAgentDetail(),false);pushed.push({name:'settings'});assert.equal(shell.readingAgentDetail(),false);
 pushed.push({name:'session'});primary.section=0;assert.equal(shell.readingAgentDetail(),false);primary.section=1;primary.selected.kind='conversation';assert.equal(shell.readingAgentDetail(),false);
});

test('peer icons retain native selection and independent accessible names without a text fallback',()=>{
 const title=source.slice(source.indexOf('  @Builder peerTitle('),source.indexOf('  private selectPeer('));
 assert.match(title,/CapsuleSegmentButtonV2/);
 for(const peer of ['persona','agents'])assert.ok(title.includes('symbol: Symbols.'+peer+", accessibilityText: tr('"+peer+"')"));
 assert.match(title,/\$selectedIndex: \(index: number\) => this\.selectPeer\(index\)/);
 assert.match(title,/itemMinHeight: LengthMetrics\.vp\(Tokens\.touch\)/);
 assert.ok(Tokens.headerPeerIcons>=Tokens.touch*2);
 assert.doesNotMatch(title,/Select\(/);
});

test('a cached chat selection is highlighted only while its destination context is displayed',()=>{
 const {shell,pushed}=fixture();shell.activeChild='';assert.equal(shell.chatSelectionShown('conversation'),false);
 pushed.push({name:'settings'});shell.activeChild='settings';assert.equal(shell.chatSelectionShown('conversation'),false);assert.equal(shell.chatSelectionShown('session'),false);
 pushed.splice(0);pushed.push({name:'session'},{name:'history'});shell.activeChild='history';assert.equal(shell.chatSelectionShown('session'),true);
 shell.activeChild='';assert.equal(shell.chatSelectionShown('session'),false);pushed.splice(0);pushed.push({name:'conversation'});shell.activeChild='chat';assert.equal(shell.chatSelectionShown('conversation'),true);
});

test('root Agent details hide Back only in the expanded Agents navigation context',()=>{
 for(const wide of [false,true]) for(const section of [0,1]) for(const root of [false,true]) {
  const {shell,primary}=fixture();primary.section=section;shell.wide=()=>wide;
  const target=row('agent','owner-b','session');target.raw=JSON.stringify({navigation_root:root});
  assert.equal(shell.destinationBackHidden('session',JSON.stringify(target)),wide&&section===1&&root);
  assert.equal(shell.destinationBackHidden('resources','inbox'),false);
 }
});

test('split root title spacing follows the visible path and keeps child Back controls',()=>{
 const {shell,pushed}=fixture();shell.wide=()=>true;
 pushed.push({name:'resources',param:'inbox'});
 assert.equal(shell.destinationBackHidden('resources','inbox'),true);
 pushed.push({name:'resources',param:'knowledge'});
 assert.equal(shell.destinationBackHidden('resources','knowledge'),false);
 pushed[1].param='inbox';
 assert.equal(shell.destinationBackHidden('resources','inbox'),false,'same-name child must keep its native Back control');
 pushed.pop();shell.wide=()=>false;
 assert.equal(shell.destinationBackHidden('resources','inbox'),false,'a single-pane first destination returns to its primary list');
});
