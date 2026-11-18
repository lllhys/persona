import test from 'node:test';
import { stripTypeScriptTypes } from 'node:module';
import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert/strict';
import {fileURLToPath} from 'node:url';

const w=fileURLToPath(new URL('../entry/src/main/ets',import.meta.url));

test('action icons distinguish approvals, uncertain writes and the operation that is actually sending',()=>{
const inbox=fs.readFileSync(w+'/components/InboxWorkspacePage.ets','utf8');
function method(src,name){
 const hit=src.indexOf('private '+name+'(): string {');assert(hit>=0,name);const start=src.indexOf('{',hit);let level=0,quote='',escaped=false;
 for(let i=start;i<src.length;i++){const ch=src[i];if(quote){if(escaped)escaped=false;else if(ch==='\\')escaped=true;else if(ch===quote)quote='';continue;}if(ch==='"'||ch==="'"){quote=ch;continue;}if(ch==='{')level++;if(ch==='}'&&--level===0)return new Function(src.slice(start+1,i));}throw new Error(name);
}

const context=state=>({workspace:{writeState:'',attachmentBusy:false,handoffBusy:false,mailComposeState:'',mailComposeBusy:false,...state}});
const cases=[
 ['conversationSendKey',{},'send'],['conversationSendKey',{writeState:'sending'},'sending'],
 ['conversationSendKey',{writeState:'unknown'},'result_unknown'],['conversationSendKey',{writeState:'pending'},'pending'],
 ['conversationSendKey',{writeState:'sending',attachmentBusy:true},'send'],
 ['attachmentSendKey',{writeState:'sending',attachmentBusy:false},'inbox_send_attachment'],
 ['attachmentSendKey',{writeState:'sending',attachmentBusy:true},'sending'],
 ['mailSendKey',{mailComposeState:'sending',mailComposeBusy:false},'sending'],
 ['mailSendKey',{mailComposeState:'draft',mailComposeBusy:true,mailComposeReady:false},'loading'],
 ['mailSendKey',{mailComposeState:'draft',mailComposeBusy:true,mailComposeReady:true},'sending'],
 ['mailSendKey',{mailComposeState:'unknown'},'result_unknown'],['mailSendKey',{mailComposeState:'pending'},'pending'],
 ['handoffSendKey',{writeState:'sending',handoffBusy:false},'inbox_handoff_send'],
 ['handoffSendKey',{writeState:'sending',handoffBusy:true},'sending'],
 ['handoffSendKey',{writeState:'unknown'},'result_unknown']
];for(const [name,state,expected]of cases)assert.equal(method(inbox,name).call(context(state)),expected,name+JSON.stringify(state));
let icons=fs.readFileSync(w+'/design/IconActions.ets','utf8').replace(/^import .*\n/gm,'').replace('export class','class').replace(/: (?:string|boolean|ResourceColor|Resource)\b/g,'');
const IconActions=vm.runInNewContext(icons+'; IconActions',{$r:s=>s,Colors:{onAction:'onAction',danger:'danger',link:'link'}});
for(const [key,glyph] of [['assistant_feed_start','play_fill'],['send','arrow_up'],['assistant_detail_retry','arrow_clockwise'],['approve','checkmark_shield'],['deny','nosign'],['confirm','checkmark'],['inbox_send_attachment','doc_text_badge_arrow_up'],['result_unknown','exclamationmark_circle']])assert.equal(IconActions.symbol(key),'sys.symbol.'+glyph,key);
assert.equal(IconActions.busy('unknown'),false);assert.equal(IconActions.busy('result_unknown'),false);
const decision=fs.readFileSync(w+'/components/InteractionDecisionForm.ets','utf8');
for(const kind of ['permission','plan'])for(const selected of ['approve','deny',''])assert.equal(method(decision,'submitIconKey').call({request:{kind},selected}),selected||'confirm');
assert.equal(method(decision,'submitIconKey').call({request:{kind:'screen'},selected:'deny'}),'confirm');

});


test('Agent quick filters retain full names using native Select when the measured pane is too narrow',()=>{
 const src=fs.readFileSync(w+'/components/AgentListPage.ets','utf8');
 const start=src.indexOf('  private quickFilterUsesSelect()'),end=src.indexOf('  @Builder filters()',start);assert(start>=0&&end>start);
 let labelPixels=210;
 const Filters=vm.runInNewContext(stripTypeScriptTypes('class Filters {'+src.slice(start,end)+'}; Filters'),{
  Tokens:{meta:14,touch:48,inset:12,page:16,fine:4},QUICK_LABELS:['all','needs_you','working','unread','agent_group_background'],QUICK_FILTERS:['all','needsYou','working','unread','backgroundWaiting'],FontWeight:{Medium:'medium'},tr:key=>key
 });
 const view=new Filters();view.environment={fontScale:1};view.getUIContext=()=>({px2vp:p=>p/3,getMeasureUtils:()=>({measureText:()=>labelPixels})});
 for(const width of [0,280,320,420]){view.filterWidth=width;assert.equal(view.quickFilterUsesSelect(),true,String(width));}
 view.filterWidth=713;assert.equal(view.quickFilterUsesSelect(),false);
 labelPixels=420;assert.equal(view.quickFilterUsesSelect(),true,'long localized label');
 labelPixels=210;view.environment.fontScale=1.5;assert.equal(view.quickFilterUsesSelect(),true,'enlarged system text');
});
