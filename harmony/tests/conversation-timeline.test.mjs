import test from 'node:test';
import assert from 'node:assert/strict';
import {mergeConversationTimeline,reconcileTimelineRows} from '../entry/src/main/ets/core/ConversationTimeline.ts';
const message=(id,role,at)=>({id,role,at});
test('plans follow the complete answer even when created during its streamed text',()=>{
 const entries=mergeConversationTimeline([message('ask','user',10),message('work','work',12),message('answer','assistant',20),message('next','user',30)],[{id:'plan',at:15}]);
 assert.deepEqual(entries.map(row=>row.id),['message:ask','message:work','message:answer','feed:plan','message:next']);
});
test('late receipts stay after the last exchange and input ties stay together',()=>{
 const entries=mergeConversationTimeline([message('text','user',10),message('image','user',10),message('answer','assistant',20)],[{id:'same-time',at:10},{id:'receipt',at:40}]);
 assert.deepEqual(entries.map(row=>row.id),['message:text','message:image','message:answer','feed:same-time','feed:receipt']);
});
test('overlapping feed pages upsert identities in stable time order',()=>{
 const entries=mergeConversationTimeline([message('next','user',30)],[{id:'b',at:20},{id:'a',at:20},{id:'b',at:20}]);
 assert.deepEqual(entries.map(row=>row.cardId).filter(Boolean),['a','b']);
});
test('a cached message with no clock does not split an exchange around a card',()=>{
 const entries=mergeConversationTimeline([message('cached','user',0),message('answer','assistant',0)],[{id:'receipt',at:10}]);
 assert.deepEqual(entries.map(row=>row.id),['message:cached','message:answer','feed:receipt']);
});

test('history reconciliation describes a prepend and append without replacing its observed array',()=>{
 const current=[{id:'b',text:'old'},{id:'c',text:'C'}],identity=current,mutations=[];
 const observed=new Proxy(current,{get(target,key,receiver){if(key==='splice')return(...args)=>{mutations.push(args.slice(0,2));return Array.prototype.splice.apply(target,args);};return Reflect.get(target,key,receiver);}});
 const next=[{id:'a',text:'A'},{id:'b',text:'updated'},{id:'c',text:'C'}];reconcileTimelineRows(observed,next);assert.equal(current,identity);assert.deepEqual(current,next);assert.deepEqual(mutations,[[0,0]]);
 mutations.length=0;reconcileTimelineRows(observed,[...next,{id:'d',text:'D'}]);assert.deepEqual(mutations,[[3,0]]);assert.equal(current[1].text,'updated');
});
test('history reconciliation handles reorder, withdrawal and same-ID request revisions',()=>{
 const current=[{id:'a',raw:'v1'},{id:'b',raw:'v1'},{id:'c',raw:'v1'}];
 for(const next of [[{id:'b',raw:'v2'},{id:'a',raw:'v1'}],[{id:'b',raw:'v3'},{id:'a',raw:'v1'}],[]]){reconcileTimelineRows(current,next);assert.deepEqual(current,next);}
});

test('one refresh can insert history and live replies while retaining the visible middle run',()=>{
 const current=[{id:'b'},{id:'c'},{id:'d'}],mutations=[];
 const observed=new Proxy(current,{get(target,key,receiver){if(key==='splice')return(...args)=>{mutations.push(args.slice(0,2));return Array.prototype.splice.apply(target,args);};return Reflect.get(target,key,receiver);}});
 const next=[{id:'a'},{id:'b'},{id:'c'},{id:'d'},{id:'e'}];reconcileTimelineRows(observed,next);assert.deepEqual(current,next);assert.deepEqual(mutations,[[0,0],[4,0]]);
});
