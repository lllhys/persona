import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {stripTypeScriptTypes} from 'node:module';

const source=readFileSync(new URL('../entry/src/main/ets/components/ExtensionView.ets',import.meta.url),'utf8');
function method(name){
 const start=source.indexOf('  private '+name+'(');assert.ok(start>=0,name);
 const body=source.indexOf('{',start);let depth=1,quote='',escaped=false,i=body+1;
 for(;i<source.length&&depth;i++){const c=source[i];if(quote){if(escaped)escaped=false;else if(c==='\\')escaped=true;else if(c===quote)quote='';}else if(c==='"'||c==="'"||c==='`')quote=c;else if(c==='{')depth++;else if(c==='}')depth--;}
 return source.slice(start,i);
}
const Wire={text:(o,k,f='')=>typeof o[k]==='string'?o[k]:f,object:(o,k)=>o[k]||{},has:(o,k)=>k in o};
const UIUtils={makeBinding:read=>({get value(){return read();}})};
const Harness=new Function('Wire','UIUtils','tr','diffContextValues','return '+stripTypeScriptTypes('class Harness{'+['menuAction','reviewActions','contextRangeActions'].map(method).join('\n')+'}'))(Wire,UIUtils,k=>k,(path,range)=>({path,...range}));
function fixture(){const host=new Harness(),owner={presentationKey:'owner/page/epoch',allowed:true,canInteract(){return this.allowed;}},calls=[];Object.assign(host,{active:true,model:owner,projectionGeneration:1,menuEpoch:0,rootKey:()=> 'root',runAction:(...args)=>calls.push(args)});return {host,owner,calls};}

test('a native menu reads the current fields but cannot follow an owner, page or presentation change',()=>{
 for(const change of ['owner','page','generation','hidden','visibility-cycle','blocked']){
  const {host,owner,calls}=fixture();const command=host.menuAction({value:{action:{id:'same-action'},key:'same-key',event:'submit',values:{note:'private synthetic draft'},role:'',summary:'',title:'Save'}});
  if(change==='owner')host.model={...owner};
  if(change==='page')owner.presentationKey='owner/another-page/epoch';
  if(change==='generation')host.projectionGeneration++;
  if(change==='hidden')host.active=false;
  if(change==='visibility-cycle')host.menuEpoch++;
  if(change==='blocked')owner.allowed=false;
  command();assert.equal(calls.length,0,change);
 }
 const {host,calls}=fixture();let draft='before';
 const command=host.menuAction({get value(){return {action:{id:'save'},key:'note',event:'submit',values:{note:draft},role:'',summary:'',title:'Save note'};}});
 assert.equal(calls.length,0,'opening a menu does not dispatch');draft='edited while menu open';command();assert.equal(calls.length,1);assert.deepEqual(calls[0][3],{note:draft});
});

test('review danger tone is visual and does not invent a destructive permission role',()=>{
 const {host}=fixture();const review={name:'Review',buttons:[{title:'Request changes',tone:'danger',action:{id:'request_changes'}}]};
 Object.assign(host,{renderView:()=>({review}),choices:v=>Array.isArray(v)?v:[],safeActionValues:(name,value)=>({[name]:value})});host.model.fieldValue=()=> 'Revise this';
 const item=host.reviewActions()[0].value;
 assert.equal(item.tone,'danger');assert.equal(item.role,'');assert.equal(item.action.confirm,undefined);
 assert.match(source,/button\.value\.tone === 'danger' \? Colors\.danger/);
});

test('a diff menu range retires when its file or range changes under the same action id',()=>{
 for(const change of ['range','path']){
  const {host,calls}=fixture();const item={path:'a.ts',context:{id:'context'}};let ranges=[{side:'new',from:10,to:20}];
  host.contextRangesAt=()=>ranges;host.contextTitle=range=>String(range.from)+'-'+String(range.to);
  const button=host.contextRangeActions(item,'same-key','top',-1)[0],oldIdentity=button.value.identity,command=host.menuAction(button);
  if(change==='range')ranges=[{side:'new',from:30,to:40}];else item.path='b.ts';
  assert.notEqual(host.contextRangeActions(item,'same-key','top',-1)[0].value.identity,oldIdentity);
  assert.equal(button.value.action.id,undefined);command();assert.equal(calls.length,0,change);
 }
});
