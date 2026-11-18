import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {registerHooks,stripTypeScriptTypes} from 'node:module';

registerHooks({
  load(url,context,next) {
    if (url.endsWith('/ComposerOverlayWorkspace.ets')) {
      const source=readFileSync(new URL(url),'utf8').replace(/@ObservedV2\s*/g,'').replace(/@Trace\s+/g,'');
      return {format:'module',shortCircuit:true,source:stripTypeScriptTypes(source)};
    }
    return next(url,context);
  }
});

const {ComposerOverlayWorkspace}=await import('../entry/src/main/ets/models/ComposerOverlayWorkspace.ets');

function fixture({connected=false,kind='conversation'}={}) {
  const state={connected,kind,generation:1,scope:'hub/owner',target:'conversation/main',repo:{},retired:false};
  const model={
    get connected(){return state.connected;},
    scope:()=>state.scope,
    connectionGeneration:()=>state.generation,
    target:()=>state.target,
    isChat:()=>state.kind==='conversation'||state.kind==='session',
    owns:(generation,scope)=>!state.retired&&generation===state.generation&&scope===state.scope,
    repo:()=>{if(!state.connected||!state.repo)throw new Error('offline');return state.repo;}
  };
  return {state,model,workspace:new ComposerOverlayWorkspace()};
}

test('offline chat admits local chooser while a non-chat remote selection is rejected',()=>{
  const {state,model,workspace}=fixture();
  const session=workspace.open(model,true,'attachments',0,true);
  assert.ok(session);
  assert.equal(session.request,0);
  assert.equal(session.preserveInput,true);
  assert.equal(workspace.current(session),true);
  assert.equal(workspace.current(session,true),false);

  workspace.close(session);
  state.kind='machine';
  assert.equal(workspace.open(model,true,'actions',1,false),undefined);
  assert.equal(workspace.open(model,false,'actions',1,false),undefined);
});

test('a repository lease is optional locally and checks the captured connected repository when requested',()=>{
  const {state,model,workspace}=fixture({connected:true});
  const session=workspace.open(model,true,'attachments',3,false);
  assert.ok(session);
  const local=workspace.ticket(session);
  const remote=workspace.ticket(session,true);
  assert.equal(local(),true);
  assert.equal(remote(),true);

  state.repo={};
  assert.equal(local(),true);
  assert.equal(remote(),false);
  state.connected=false;
  assert.equal(remote(),false);
});

test('sync revokes a lease when the owner, scope generation, target, chat selection, or visibility changes',()=>{
  for(const change of ['owner','scope','generation','target','selection','hidden']) {
    const {state,model,workspace}=fixture();
    const session=workspace.open(model,true,'actions',5,false);
    assert.ok(session);
    const ticket=workspace.ticket(session);
    let syncModel=model,active=true;
    if(change==='owner') syncModel=fixture().model;
    else if(change==='scope') state.scope='hub/other';
    else if(change==='generation') state.generation++;
    else if(change==='target') state.target='conversation/other';
    else if(change==='selection') state.kind='file';
    else if(change==='hidden') active=false;
    workspace.sync(syncModel,active);
    assert.equal(ticket(),false,change);
    assert.equal(workspace.shown,false,change);
  }
});

test('the same prompt receives a fresh token and preserves the More actions request value',()=>{
  const {model,workspace}=fixture();
  let actionsRequest=17;
  const first=workspace.open(model,true,'actions',actionsRequest,true);
  assert.ok(first);
  actionsRequest=18;
  assert.equal(first.request,17);
  workspace.close(first);
  const second=workspace.open(model,true,'actions',17,true);
  assert.ok(second);
  assert.notEqual(second.id,first.id);
  assert.equal(second.request,17);
  assert.equal(second.owner,model);
  assert.equal(workspace.current(first),false);
});

test('close keeps a started native presentation locked until its matching finish',()=>{
  const {model,workspace}=fixture();
  const first=workspace.open(model,true,'attachments',0,true);
  assert.ok(first);
  assert.equal(workspace.presenting,false);
  workspace.begin(first);
  assert.equal(workspace.presenting,true);
  assert.equal(workspace.close(first),true);
  assert.equal(workspace.shown,false);
  assert.equal(workspace.presenting,true);
  assert.equal(workspace.open(model,true,'actions',1,false),undefined);
  assert.equal(workspace.finish(first),true);
  assert.equal(workspace.presenting,false);
  const next=workspace.open(model,true,'actions',1,false);
  assert.ok(next);
});

test('stale close and finish callbacks cannot affect a newer session',()=>{
  const {model,workspace}=fixture();
  const first=workspace.open(model,true,'actions',2,false);
  assert.ok(first);
  workspace.begin(first);
  workspace.close(first);
  assert.equal(workspace.finish(first),true);

  const next=workspace.open(model,true,'attachments',3,true);
  assert.ok(next);
  const ticket=workspace.ticket(next);
  assert.equal(workspace.close(first),false);
  assert.equal(workspace.finish(first),false);
  assert.equal(workspace.shown,true);
  assert.equal(workspace.session,next);
  assert.equal(ticket(),true);
});

test('revocation before the first native frame does not strand the presentation lock',()=>{
  const {state,model,workspace}=fixture();
  const first=workspace.open(model,true,'attachments',0,true);
  assert.ok(first);
  state.target='conversation/next';
  workspace.sync(model,true);
  workspace.begin(first);
  assert.equal(workspace.open(model,true,'actions',1,false)?.request,1);
});

test('retire before the first frame releases every lease and makes old callbacks inert',()=>{
  const {model,workspace}=fixture({connected:true});
  const first=workspace.open(model,true,'attachments',0,true);
  assert.ok(first);
  const remote=workspace.ticket(first,true);
  assert.equal(remote(),true);
  assert.equal(workspace.presenting,false);
  workspace.retire();
  assert.equal(workspace.shown,false);
  assert.equal(workspace.session,undefined);
  assert.equal(workspace.presenting,false);
  assert.equal(remote(),false);
  workspace.begin(first);
  assert.equal(workspace.finish(first),false);
  assert.equal(workspace.close(first),false);

  const next=workspace.open(model,true,'actions',1,false);
  assert.ok(next);
  workspace.begin(first);
  assert.equal(workspace.finish(first),false);
  assert.equal(workspace.close(first),false);
  assert.equal(workspace.shown,true);
  assert.equal(workspace.presenting,false);
  assert.equal(workspace.session,next);
});

test('retire after native presentation clears presenting and isolates the next lease from stale callbacks',()=>{
  const {model,workspace}=fixture({connected:true});
  const first=workspace.open(model,true,'attachments',0,true);
  assert.ok(first);
  const remote=workspace.ticket(first,true);
  assert.equal(remote(),true);
  workspace.begin(first);
  assert.equal(workspace.presenting,true);
  workspace.retire();
  assert.equal(workspace.shown,false);
  assert.equal(workspace.session,undefined);
  assert.equal(workspace.presenting,false);
  assert.equal(remote(),false);

  const next=workspace.open(model,true,'actions',4,false);
  assert.ok(next);
  workspace.begin(next);
  assert.equal(workspace.presenting,true);
  workspace.begin(first);
  assert.equal(workspace.finish(first),false);
  assert.equal(workspace.close(first),false);
  assert.equal(workspace.shown,true);
  assert.equal(workspace.presenting,true);
  assert.equal(workspace.session,next);
});
