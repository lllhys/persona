import test from 'node:test';
import assert from 'node:assert/strict';
import {registerHooks, stripTypeScriptTypes} from 'node:module';
import {readFileSync} from 'node:fs';

registerHooks({
  resolve(specifier, context, next) {
    if (context.parentURL?.endsWith('.ets') && specifier.startsWith('.')) {
      return {url: new URL(specifier + '.ets', context.parentURL).href, shortCircuit: true};
    }
    return next(specifier, context);
  },
  load(url, context, next) {
    if (url.endsWith('.ets')) {
      const source = readFileSync(new URL(url), 'utf8')
        .replace(/@ObservedV2\s*/g, '')
        .replace(/@Trace\s+/g, '');
      return {format: 'module', source: stripTypeScriptTypes(source), shortCircuit: true};
    }
    return next(url, context);
  }
});

const {AssistantFeedModel} = await import('../entry/src/main/ets/models/AssistantFeed.ets');
const {ResourceRow} = await import('../entry/src/main/ets/models/Wire.ets');

const conversationId = 'conversation-1';
const managerId = 'work';
const timestamp = '2026-11-11T10:00:00.000Z';

function conversation(id = conversationId, manager = managerId) {
  const row = new ResourceRow(id, 'Conversation', 'conversation');
  row.manager = manager;
  return row;
}

function feedItem(id, kind, options = {}) {
  return {
    id,
    kind,
    ref: options.ref || '',
    conversation: options.conversation || conversationId,
    manager: options.manager || managerId,
    at: options.at || timestamp,
    body: options.body || {}
  };
}

function fixture(handler, options = {}) {
  const selected = options.row || conversation();
  const state = {
    epoch: 7,
    manager: options.manager || managerId,
    canApprove: true,
    selected,
    blocked: new Set(),
    repository: undefined, handlers: []
  };
  const repository = {
    owner: state.manager,
    calls: [],
    async call(path, method = 'GET', body = {}, op = '', admit) {
      const call = {path, method, body, op, admit, repository: this};
      this.calls.push(call);
      return handler(call, state, admit);
    }
  };
  state.repository = repository;
  const model = {
    profile: {manager: state.manager},
    sessions: options.sessions || [],
    machines: [],
    scope() { return 'hub:' + state.manager; },
    connectionGeneration() { return state.epoch; },
    owns(generation, scope) { return generation === state.epoch && scope === this.scope(); },
    target(row = state.selected) { return JSON.stringify([this.scope(), row.kind, row.machine, row.id]); },
    repo() { return state.repository; },
    canApprove() { return state.canApprove; },
    operationBlocked(path) { return state.blocked.has(path); },
    machineName(id) { return id; },
    observeEvents(handler) { state.handlers.push(handler); return () => { state.handlers = state.handlers.filter(value => value !== handler); }; }
  };
  return {model, repository, state, row: selected};
}

function plan(id = 'plan-1', options = {}) {
  return {
    id,
    conversation: conversationId,
    state: options.state || 'open',
    created_at: timestamp,
    updated_at: timestamp,
    tasks: options.tasks || [{title: 'Read source', prompt: 'Inspect files', machine: 'machine-1', cwd: '/repo', permission: 'plan'}]
  };
}

function feedPage(items = [], plans = {}, extra = {}) {
  return {items, plans, task_cards: [], views: {}, view_actions: {}, view_blocks: [], next_cursor: '', ...extra};
}

function receipt(id, todoId, state, resolution = '') {
  return feedItem(id, 'receipt', {ref: todoId, body: {state, ...(resolution ? {resolution} : {})}});
}

function receiptTodo(options = {}) {
  return {
    id: options.id || 'todo-receipt', manager: managerId, origin: options.origin || conversationId,
    plan_id: options.plan_id === undefined ? 'plan-receipt' : options.plan_id,
    title: 'Receipt task', done_at: options.done_at === undefined ? '' : options.done_at,
    start_error: options.start_error === undefined ? '' : options.start_error,
    agents: options.agents || []
  };
}

function listedSession(options = {}) {
  const row = new ResourceRow(options.session || 'session-receipt', 'Receipt Agent', 'session');
  row.manager = managerId;
  row.machine = options.machine || 'machine-receipt';
  row.session = options.session || 'session-receipt';
  row.state = options.state || 'idle';
  row.raw = JSON.stringify({
    id: row.id, manager: row.manager, machine_id: row.machine, session_id: row.session,
    state: row.state, turn_error: options.turn_error || 'rate_limited',
    ...(options.turn_error_kind ? {turn_error_kind: options.turn_error_kind} : {}),
    ...(options.turn_error_resumes !== undefined ? {turn_error_resumes: options.turn_error_resumes} : {}),
    ...(options.turn_error_resets_at ? {turn_error_resets_at: options.turn_error_resets_at} : {}),
    ...(options.work ? {work: options.work} : {})
  });
  return row;
}

async function opened(handler, options = {}) {
  const f = fixture(handler, options);
  const feed = new AssistantFeedModel();
  await feed.open(f.model, f.row);
  return {...f, feed};
}

test('the feed and plan start retain the conversation owner and captured repository', async () => {
  let completeStart, signalStart;
  const startReached = new Promise((resolve) => { signalStart = resolve; });
  const repoA = fixture((call) => {
    if (call.path.includes('/feed')) return feedPage([feedItem('feed-plan', 'plan', {ref: 'plan-1'})], {'plan-1': plan()});
    if (call.path.startsWith('/api/v1/task-plans/plan-1?')) return {plan: plan()};
    if (call.path.includes('/start')) return new Promise((resolve) => { completeStart = resolve; signalStart(); });
    return {};
  });
  const repoB = fixture(async () => ({}));
  const state = repoA.state;
  state.repository = repoA.repository;
  const feed = new AssistantFeedModel();
  await feed.open(repoA.model, repoA.row);
  assert.match(repoA.repository.calls[0].path, /feed\?paginate=1&manager=work$/);
  const started = feed.startPlan('plan-1');
  await startReached;
  const startCall = repoA.repository.calls.find((call) => call.path.endsWith('/task-plans/plan-1/start'));
  assert.ok(startCall);
  assert.equal(startCall.repository.owner, 'work');

  state.epoch++;
  state.selected = conversation('other-conversation');
  state.repository = repoB.repository;
  completeStart({plan: {...plan(), state: 'started', tasks: [{...plan().tasks[0], todo_id: 'todo-1'}]}});
  assert.equal(await started, false);
  assert.equal(feed.planState('plan-1'), 'open', 'a reply for the old target is ignored');
  assert.equal(repoB.repository.calls.length, 0, 'a replacement repository is never used for the old write');
});

test('feed items and todos from another manager or conversation never enter the scoped projection', async () => {
  const {feed} = await opened(async (call) => {
    if (call.path.includes('/feed')) return feedPage([
      feedItem('wrong-manager', 'task', {ref: 'todo-1', manager: 'personal'}),
      feedItem('owner-task', 'task', {ref: 'todo-2'}),
      feedItem('wrong-conversation', 'plan', {ref: 'plan-foreign', conversation: 'other-conversation'})
    ], {'plan-foreign': {...plan('plan-foreign'), conversation: 'other-conversation'}});
    if (call.path.startsWith('/api/v1/todos')) return {todos: [
      {id: 'todo-1', manager: 'personal', origin: conversationId, title: 'Foreign manager'},
      {id: 'todo-2', manager: managerId, origin: 'other-conversation', title: 'Foreign conversation'}
    ]};
    return {};
  });

  assert.deepEqual(feed.items.map((item) => item.id), ['owner-task']);
  assert.deepEqual(Object.keys(feed.plans), []);
  assert.deepEqual(feed.todos, []);
  assert.equal(feed.openTodo('todo-1'), undefined);
  assert.equal(feed.openTodo('todo-2'), undefined);
});

test('an unknown message send stays pending through refresh and cannot send again', async () => {
  const message = feedItem('draft-card', 'message', {ref: 'inbox-item', body: {
    todo_id: 'todo-1', source: 'mail', sender: 'sender@example.test', chat: 'Subject', text: 'Question', draft: 'Approved reply'
  }});
  let replyCalls = 0;
  const {feed} = await opened(async (call) => {
    if (call.path.includes('/feed')) return feedPage([message]);
    if (call.path.includes('/inbox/inbox-item/reply')) {
      replyCalls++;
      const error = new Error('result_unknown');
      error.status = 502;
      error.resultUnknown = true;
      throw error;
    }
    return {};
  });

  assert.equal(feed.messageCanSend(feed.items[0]), true);
  assert.equal(await feed.sendMessage('draft-card', 'Approved reply'), false);
  assert.equal(feed.messageState(feed.items[0]), 'pending');
  await feed.refresh();
  assert.equal(feed.messageState(feed.items[0]), 'pending');
  assert.equal(feed.messageCanSend(feed.items[0]), false);
  assert.equal(await feed.sendMessage('draft-card', 'Approved reply'), false);
  assert.equal(replyCalls, 1, 'verification never causes an automatic resend');
});

test('an unknown plan start is reconciled and blocked from a second start', async () => {
  const openPlan = plan('plan-unknown');
  let startCalls = 0;
  const {feed} = await opened(async (call, state) => {
    if (call.path.includes('/feed')) return feedPage([feedItem('feed-plan', 'plan', {ref: openPlan.id})], {[openPlan.id]: openPlan});
    if (call.path === '/api/v1/task-plans/plan-unknown/start') {
      startCalls++;
      state.blocked.add(call.path);
      const error = new Error('result_unknown');
      error.status = 502;
      error.resultUnknown = true;
      throw error;
    }
    if (call.path.startsWith('/api/v1/task-plans/plan-unknown?')) { assert.equal(new URL(call.path,'http://fixture').searchParams.get('manager'),managerId);return {plan: openPlan}; }
    return {};
  });

  assert.equal(await feed.startPlan(openPlan.id), false);
  assert.equal(feed.planState(openPlan.id), 'open', 'the unknown response is not presented as success');
  assert.equal(feed.error, 'result_unknown');
  assert.equal(feed.planCanStart(openPlan.id), false);
  assert.equal(await feed.startPlan(openPlan.id), false);
  assert.equal(startCalls, 1, 'an unknown start is never submitted again automatically');
});

test('an already started plan has no Start action', async () => {
  const startedPlan = plan('plan-started', {state: 'started', tasks: [{
    title: 'Read source', prompt: 'Inspect files', machine: 'machine-1', cwd: '/repo', permission: 'plan', todo_id: 'todo-1'
  }]});
  const {feed} = await opened(async (call) => {
    if (call.path.includes('/feed')) return feedPage([feedItem('feed-plan', 'plan', {ref: startedPlan.id})], {[startedPlan.id]: startedPlan});
    if (call.path.startsWith('/api/v1/todos')) return {todos: [{id: 'todo-1', manager: managerId, origin: conversationId, title: 'Read source'}]};
    return {};
  });

  assert.equal(feed.planState(startedPlan.id), 'started');
  assert.equal(feed.planCanStart(startedPlan.id), false);
  assert.equal(feed.planCanDismiss(startedPlan.id), false);
});

test('editing a read-only task keeps plan permission and explicit Start sends no escalation', async () => {
  const readonlyPlan = plan('plan-readonly', {tasks: [{
    title: 'Read source', prompt: 'Inspect files', machine: 'machine-1', cwd: '/repo', permission: 'plan', readonly: true
  }]});
  let startBody, state = 'open';
  const {feed} = await opened(async (call) => {
    if (call.path.includes('/feed')) return feedPage([feedItem('feed-plan', 'plan', {ref: readonlyPlan.id})], {
      [readonlyPlan.id]: {...readonlyPlan, state}
    });
    if (call.path.startsWith('/api/v1/task-plans/plan-readonly?')) return {plan: {...readonlyPlan, state}};
    if (call.path.endsWith('/task-plans/plan-readonly/start')) {
      startBody = call.body;
      state = 'started';
      return {plan: {...readonlyPlan, state}};
    }
    return {};
  });

  assert.equal(feed.beginEditPlan(readonlyPlan.id), false, 'a feed snapshot alone cannot authorize editing');
  assert.equal(await feed.refreshPlan(readonlyPlan.id), true);
  assert.equal(feed.beginEditPlan(readonlyPlan.id), true);
  feed.updateEditTask(0, 'permission', 'bypassPermissions');
  assert.equal(feed.editTasks[0].permission, 'plan');
  assert.equal(await feed.startEditedPlan(), true);
  assert.ok(Array.isArray(startBody.tasks));
  assert.equal(Object.hasOwn(startBody.tasks[0], 'permission'), false);
  assert.equal(feed.planState(readonlyPlan.id), 'started');
});

test('writable plan edits can submit only the server-required permission mode', async () => {
  const writablePlan = plan('plan-writable', {tasks: [{
    title: 'Change source', prompt: 'Update implementation', machine: 'machine-1', cwd: '/repo', permission: 'default', readonly: false
  }]});
  let startBody, state = 'open';
  const {feed} = await opened(async (call) => {
    if (call.path.includes('/feed')) return feedPage([feedItem('feed-plan', 'plan', {ref: writablePlan.id})], {
      [writablePlan.id]: {...writablePlan, state}
    });
    if (call.path.startsWith('/api/v1/task-plans/plan-writable?')) return {plan: {...writablePlan, state}};
    if (call.path.endsWith('/task-plans/plan-writable/start')) {
      startBody = call.body;
      state = 'started';
      return {plan: {...writablePlan, state}};
    }
    return {};
  });

  assert.equal(feed.beginEditPlan(writablePlan.id), false, 'a feed snapshot alone cannot authorize editing');
  assert.equal(await feed.refreshPlan(writablePlan.id), true);
  assert.equal(feed.beginEditPlan(writablePlan.id), true);
  feed.updateEditTask(0, 'permission', 'auto');
  assert.equal(feed.editTasks[0].permission, 'default');
  assert.equal(await feed.startEditedPlan(), false);
  assert.equal(startBody, undefined);
  feed.updateEditTask(0, 'permission', 'bypassPermissions');
  assert.equal(await feed.startEditedPlan(), true);
  assert.equal(startBody.tasks[0].permission, 'bypassPermissions');
});

test('paged refresh advances the older cursor while merging each feed identity once', async () => {
  const cursors = new Map([
    ['cursor-1', feedPage([
      feedItem('feed-1', 'notice', {body: {text: 'Updated card'}}),
      feedItem('feed-2', 'notice')
    ], {}, {next_cursor: 'cursor-2'})],
    ['cursor-2', feedPage([feedItem('feed-3', 'notice')], {}, {next_cursor: 'cursor-3'})],
    ['cursor-3', feedPage([feedItem('feed-4', 'notice')], {}, {next_cursor: 'cursor-4'})],
    ['cursor-4', feedPage([feedItem('feed-5', 'notice')])]
  ]);
  const {feed, repository} = await opened(async (call) => {
    if (!call.path.includes('/feed')) return {};
    if (call.path.includes('cursor=')) return cursors.get(new URL('https://hub.test' + call.path).searchParams.get('cursor'));
    return feedPage([feedItem('feed-1', 'notice', {body: {text: 'Original card'}})], {}, {next_cursor: 'cursor-1'});
  });

  assert.deepEqual(feed.items.map((item) => item.id), ['feed-1', 'feed-2', 'feed-3', 'feed-4']);
  assert.equal(feed.items.find((item) => item.id === 'feed-1').body.text, 'Updated card');
  assert.equal(feed.nextCursor, 'cursor-4');
  await feed.loadMore();
  assert.deepEqual(feed.items.map((item) => item.id), ['feed-1', 'feed-2', 'feed-3', 'feed-4', 'feed-5']);
  assert.equal(feed.nextCursor, '');
  assert.ok(repository.calls.some((call) => call.path.includes('cursor=cursor-4')));
});



test('live assistant card events update without transcript messages and cannot be downgraded by a raced feed',async()=>{
 const f=fixture(async()=>feedPage()),feed=new AssistantFeedModel();await feed.open(f.model,f.row);
 const emit=frame=>f.state.handlers.forEach(handler=>handler(JSON.stringify({manager:managerId,conversation:conversationId,...frame})));
 emit({type:'assistant_view',view:{id:'view_live',conversation:conversationId,version:2,view:{title:'Current',sections:[]}}});
 emit({type:'assistant_view_block',key:'item#0/0'});
 emit({type:'assistant_view_action',action:{key:'view_live',action:'choice',client_msg_id:'reply',status:'sent',revision:2,version:2,values:{a:'b'}}});
 await feed.refresh();assert.equal(feed.views.view_live.version,2);assert.equal(feed.viewActions.view_live.status,'sent');assert.ok(feed.viewBlocks.includes('item#0/0'));
 emit({type:'assistant_view',view:{id:'view_live',conversation:conversationId,version:1,view:{title:'Old',sections:[]}}});
 emit({type:'assistant_view_action',action:{key:'view_live',action:'choice',client_msg_id:'reply',status:'pending',revision:1,version:2,values:{a:'b'}}});
 emit({type:'assistant_view_action',action:{key:'view_live',action:'another-choice',client_msg_id:'other',status:'sent',revision:9,version:2,values:{a:'b'}}});
 assert.equal(feed.views.view_live.version,2);assert.equal(feed.viewActions.view_live.status,'sent');assert.equal(feed.viewActions.view_live.client_msg_id,'reply');
 const callback=f.state.handlers[0];feed.close();assert.equal(f.state.handlers.length,0);callback(JSON.stringify({type:'assistant_view_block',manager:managerId,conversation:conversationId,key:'late/0'}));assert.deepEqual(feed.viewBlocks,[]);
});

test('assistant card events retain their exact manager and conversation scope',async()=>{
 const f=fixture(async()=>feedPage()),feed=new AssistantFeedModel();await feed.open(f.model,f.row);
 for(const frame of [{manager:'another',conversation:conversationId},{manager:managerId,conversation:'another'}]) f.state.handlers[0](JSON.stringify({type:'assistant_view_block',key:'foreign/0',...frame}));
 assert.deepEqual(feed.viewBlocks,[]);feed.close();
});

test('a plan write retired while its mutation journal is pending never reaches transport', async () => {
  let routeActive = true, signalJournal;
  const journalStarted = new Promise((resolve) => { signalJournal = resolve; });
  let releaseJournal;
  const journalWrite = new Promise((resolve) => { releaseJournal = resolve; });
  const f = fixture(async (call) => {
    if (call.path.includes('/feed')) return feedPage([feedItem('feed-plan', 'plan', {ref: 'plan-journal'})], {'plan-journal': plan('plan-journal')});
    if (call.path.startsWith('/api/v1/task-plans/plan-journal?')) return {plan: plan('plan-journal')};
    return {};
  });
  const repository = f.repository;
  const transportPosts = [];
  repository.call = async (path, method = 'GET', body = {}, op = '', admit) => {
    const call = {path, method, body, op, admit, repository};
    repository.calls.push(call);
    if (method === 'POST') {
      signalJournal();
      await journalWrite;
      if (admit && !admit()) {
        const error = new Error('request_updated');
        error.status = 409;
        throw error;
      }
      transportPosts.push(call);
      return {plan: {...plan('plan-journal'), state: 'started'}};
    }
    return call.path.includes('/feed') ?
      feedPage([feedItem('feed-plan', 'plan', {ref: 'plan-journal'})], {'plan-journal': plan('plan-journal')}) :
      {plan: plan('plan-journal')};
  };
  const feed = new AssistantFeedModel();
  await feed.open(f.model, f.row, false, () => routeActive);
  await feed.refresh();

  const started = feed.startPlan('plan-journal');
  await journalStarted;
  routeActive = false;
  f.state.epoch++;
  f.state.selected = conversation('replacement-conversation');
  releaseJournal();

  assert.equal(await started, false);
  assert.equal(transportPosts.length, 0, 'the admission callback is rechecked after journal completion and before transport');
});

test('direct Start and Dismiss stop when the fresh plan differs from the displayed plan', async () => {
  for (const action of ['start', 'dismiss']) {
    const id = 'plan-stale-' + action;
    const displayed = plan(id);
    const current = {...displayed, updated_at: '2026-11-12T10:00:00.000Z', tasks: [{...displayed.tasks[0], prompt: 'New unseen prompt'}]};
    let posts = 0;
    const {feed} = await opened(async (call) => {
      if (call.path.includes('/feed')) return feedPage([feedItem('feed-' + id, 'plan', {ref: id})], {[id]: displayed});
      if (call.path.startsWith('/api/v1/task-plans/' + id + '?')) return {plan: current};
      if (call.method === 'POST') posts++;
      return {};
    });

    const admitted = action === 'start' ? await feed.startPlan(id) : await feed.dismissPlan(id);
    assert.equal(admitted, false);
    assert.equal(posts, 0, action + ' never submits the unseen plan edit');
    assert.equal(feed.error, 'request_updated');
    assert.equal(feed.plan(id).tasks[0].prompt, 'New unseen prompt', 'the fresh plan replaces the stale display');
  }
});

test('Edit fetches the current plan before opening and retains the write capability check', async () => {
  let signalPlanRead, releasePlanRead;
  const planRead = new Promise((resolve) => { releasePlanRead = resolve; });
  const planReadStarted = new Promise((resolve) => { signalPlanRead = resolve; });
  const current = plan('plan-edit-current');
  const f = fixture(async (call) => {
    if (call.path.includes('/feed')) return feedPage([feedItem('feed-edit-current', 'plan', {ref: current.id})], {[current.id]: current});
    if (call.path.startsWith('/api/v1/task-plans/plan-edit-current?')) {
      signalPlanRead();
      await planRead;
      return {plan: current};
    }
    return {};
  });
  const feed = new AssistantFeedModel();
  await feed.open(f.model, f.row);
  const editing = feed.editPlan(current.id);
  await planReadStarted;
  assert.equal(feed.editing, false, 'the editor waits for the current Hub plan');
  f.state.canApprove = false;
  releasePlanRead();
  assert.equal(await editing, false, 'a capability downgrade while reading cannot open a writable editor');
  assert.equal(feed.editing, false);
});

test('plan Start and Dismiss recheck write capability after their mutation journal completes', async () => {
  for (const action of ['start', 'dismiss']) {
    let signalJournal, releaseJournal;
    const journalWrite = new Promise((resolve) => { releaseJournal = resolve; });
    const journalStarted = new Promise((resolve) => { signalJournal = resolve; });
    const current = plan('plan-capability-' + action);
    const f = fixture(async (call, state, admit) => {
      if (call.path.includes('/feed')) return feedPage([feedItem('feed-' + current.id, 'plan', {ref: current.id})], {[current.id]: current});
      if (call.method === 'POST') {
        signalJournal();
        await journalWrite;
        if (admit && !admit()) {
          const error = new Error('request_updated');
          error.status = 409;
          throw error;
        }
        return {plan: {...current, state: action === 'start' ? 'started' : 'dismissed'}};
      }
      if (call.path.startsWith('/api/v1/task-plans/' + current.id + '?')) return {plan: current};
      return {};
    });
    const feed = new AssistantFeedModel();
    await feed.open(f.model, f.row);
    const writing = action === 'start' ? feed.startPlan(current.id) : feed.dismissPlan(current.id);
    await journalStarted;
    f.state.canApprove = false;
    releaseJournal();
    assert.equal(await writing, false);
    assert.equal(feed.error, 'request_updated');
    assert.equal(f.repository.calls.filter((call) => call.method === 'POST').length, 1, 'only the journaled request was attempted');
  }
});

test('only a real failed-start receipt with the server retry facts can be retried', async () => {
  const failedReceipt = receipt('receipt-failed-start', 'todo-receipt', 'failed');
  const failedTodo = receiptTodo({done_at: '2026-11-12T09:00:00.000Z', start_error: 'Launcher unavailable'});
  const {feed} = await opened(async (call) => {
    if (call.path.includes('/feed')) return feedPage([failedReceipt]);
    if (call.path.startsWith('/api/v1/todos')) return {todos: [failedTodo]};
    return {};
  });

  assert.equal(feed.canRetryReceipt(failedReceipt.id, failedTodo.id), true);
  for (const invalid of [
    {...failedTodo, done_at: ''},
    {...failedTodo, start_error: ''},
    {...failedTodo, plan_id: ''},
    {...failedTodo, origin: 'another-conversation'},
    {...failedTodo, manager: 'personal'}
  ]) {
    feed.todos = [invalid];
    assert.equal(feed.canRetryReceipt(failedReceipt.id, failedTodo.id), false);
  }
  assert.equal(feed.canRetryReceipt('not-the-clicked-receipt', failedTodo.id), false);
});

test('receipt recovery admits retry and continue after their own pending journal but rejects an existing one', async () => {
  for (const action of ['retry', 'continue']) {
    let currentTodo = receiptTodo(action === 'retry' ? {done_at: '2026-11-12T09:00:00.000Z', start_error: 'Launcher unavailable'} : {
      agents: [{machine_id: 'machine-receipt', session_id: 'session-receipt'}]
    });
    const currentReceipt = receipt('receipt-' + action, currentTodo.id, action === 'retry' ? 'failed' : 'interrupted', 'pending');
    let signalJournal, releaseJournal;
    const journalWrite = new Promise((resolve) => { releaseJournal = resolve; });
    const journalStarted = new Promise((resolve) => { signalJournal = resolve; });
    let transported = 0;
    const session = action === 'continue' ? listedSession({state: 'idle', turn_error: 'rate_limited'}) : undefined;
    const f = fixture(async (call, state, admit) => {
      if (call.path.includes('/feed')) return feedPage([currentReceipt]);
      if (call.path.startsWith('/api/v1/todos') && call.method === 'GET') return {todos: [currentTodo]};
      if (call.method === 'POST') {
        signalJournal();
        await journalWrite;
        state.blocked.add(call.path); // the local write journal is pending before transport admission
        if (admit && !admit()) {
          const error = new Error('request_updated');
          error.status = 409;
          throw error;
        }
        transported++;
        currentTodo = {...currentTodo, done_at: '', start_error: ''};
        return {todo: currentTodo};
      }
      return {};
    }, {sessions: session ? [session] : []});
    const feed = new AssistantFeedModel();
    await feed.open(f.model, f.row);
    const writing = feed.retryReceipt(currentReceipt.id, currentTodo.id, action);
    await journalStarted;
    releaseJournal();
    assert.equal(await writing, true, action + ' admits its own journal row at transport');
    assert.equal(transported, 1);

    const blockedTodo = receiptTodo(action === 'retry' ? {
      done_at: '2026-11-12T09:00:00.000Z', start_error: 'Launcher unavailable'
    } : {agents: [{machine_id: 'machine-receipt', session_id: 'session-receipt'}]});
    const blocked = fixture(async (call) => {
      if (call.path.includes('/feed')) return feedPage([currentReceipt]);
      if (call.path.startsWith('/api/v1/todos')) return {todos: [blockedTodo]};
      return {};
    }, {sessions: session ? [session] : []});
    const blockedFeed = new AssistantFeedModel();
    await blockedFeed.open(blocked.model, blocked.row);
    const blockedPath = '/api/v1/todos/' + encodeURIComponent(blockedTodo.id) + '/retry?manager=' + encodeURIComponent(managerId);
    blocked.state.blocked.add(blockedPath);
    assert.equal(await blockedFeed.retryReceipt(currentReceipt.id, blockedTodo.id, action), false,
      'a pre-existing unresolved journal still blocks ' + action);
    assert.equal(blocked.repository.calls.filter((call) => call.method === 'POST').length, 0);
  }
});

test('a missing listed session never qualifies as continued or as a manual continuation target', async () => {
  const interrupted = receipt('receipt-no-session', 'todo-receipt', 'interrupted', 'pending');
  const missingSessionTodo = receiptTodo({agents: [{machine_id: 'machine-receipt', session_id: 'not-listed'}]});
  const {feed} = await opened(async (call) => {
    if (call.path.includes('/feed')) return feedPage([interrupted]);
    if (call.path.startsWith('/api/v1/todos')) return {todos: [missingSessionTodo]};
    return {};
  });
  assert.deepEqual(feed.receiptLeadAgent(missingSessionTodo.id), {});
  assert.equal(feed.canContinueReceipt(interrupted.id, missingSessionTodo.id), false);
});

test('an unrecognized recovery acknowledgment blocks another write while reconciling authoritative cards', async () => {
 for(const action of ['retry','continue'])for(const mismatch of ['missing','id','origin','manager']){
  const task=receiptTodo(action==='retry'?{done_at:timestamp,start_error:'Launcher unavailable'}:{agents:[{machine_id:'machine-receipt',session_id:'session-receipt'}]});
  const card=receipt('receipt-uncertain',task.id,action==='retry'?'failed':'interrupted','pending');
  const session=listedSession();let posts=0,readsAfterPost=0;
  const {feed}=await opened(async(call)=>{
   if(call.path.includes('/feed')){if(posts)readsAfterPost++;return feedPage([card]);}
   if(call.method==='GET'&&call.path.startsWith('/api/v1/todos'))return {todos:[task]};
   if(call.method==='POST'){
    posts++;const todo={...task};
    if(mismatch==='missing')return {};
    todo[mismatch]='another-'+mismatch;return {todo};
   }
   return {};
  },{sessions:[session]});
  assert.equal(await feed.retryReceipt(card.id,task.id,action),false);
  assert.equal(feed.error,'result_unknown',action+'/'+mismatch);
  assert.ok(readsAfterPost>0,'reconcile feed after the unrecognized acknowledgment');
  assert.equal(feed.busyKeys.length,0);
  assert.equal(action==='retry'?feed.canRetryReceipt(card.id,task.id):feed.canContinueReceipt(card.id,task.id),false);
  assert.equal(await feed.retryReceipt(card.id,task.id,action),false);assert.equal(posts,1,'uncertainty cannot dispatch a second recovery');
 }
});

test('uncertain recovery reads feed and todos after POST even while an earlier refresh is in flight',async()=>{
 const task=receiptTodo({done_at:timestamp,start_error:'Synthetic failure'}),card=receipt('receipt-refresh-race',task.id,'failed');
 let holdNext=false,posts=0,postFeedReads=0,postTodoReads=0,entered,release;
 const started=new Promise(resolve=>entered=resolve),gate=new Promise(resolve=>release=resolve);
 const {feed}=await opened(async(call)=>{
  if(call.path.includes('/feed')){
   if(holdNext){holdNext=false;entered();await gate;}
   else if(posts)postFeedReads++;
   return feedPage([card]);
  }
  if(call.method==='GET'&&call.path.startsWith('/api/v1/todos')){if(posts)postTodoReads++;return {todos:[task]};}
  if(call.method==='POST'){posts++;return {};}
  return {};
 });
 holdNext=true;const stale=feed.refresh();await started;
 try{
  assert.equal(await feed.retryReceipt(card.id,task.id,'retry'),false);
  assert.ok(postFeedReads>0&&postTodoReads>0,'fresh reconciliation cannot coalesce into the pre-POST read');
  assert.equal(feed.canRetryReceipt(card.id,task.id),false);
 }finally{release();await stale;}
});

test('a receipt updated to resumed cannot act on a later interruption', async () => {
  const oldReceipt = receipt('receipt-old', 'todo-receipt', 'interrupted', 'pending');
  const laterReceipt = receipt('receipt-later', 'todo-receipt', 'interrupted', 'pending');
  const currentTodo = receiptTodo({agents: [{machine_id: 'machine-receipt', session_id: 'session-receipt'}]});
  const session = listedSession({state: 'idle', turn_error: 'rate_limited'});
  let feedReads = 0, posts = 0;
  const {feed} = await opened(async (call) => {
    if (call.path.includes('/feed')) {
      feedReads++;
      return feedReads === 1 ? feedPage([oldReceipt]) : feedPage([
        {...oldReceipt, body: {...oldReceipt.body, resolution: 'resumed'}}, laterReceipt
      ]);
    }
    if (call.path.startsWith('/api/v1/todos') && call.method === 'GET') return {todos: [currentTodo]};
    if (call.method === 'POST') posts++;
    return {};
  }, {sessions: [session]});

  assert.equal(feed.canContinueReceipt(oldReceipt.id, currentTodo.id), true, 'the opened snapshot looks pending');
  assert.equal(await feed.retryReceipt(oldReceipt.id, currentTodo.id, 'continue'), false);
  assert.equal(feed.error, 'request_updated');
  assert.equal(feed.canContinueReceipt(oldReceipt.id, currentTodo.id), false);
  assert.equal(feed.canContinueReceipt(laterReceipt.id, currentTodo.id), true, 'only the later pending receipt may act');
  assert.equal(posts, 0);
});

test('cold detail lookup follows real cursors past four pages and reaches the requested receipt', async () => {
  const pages = new Map();
  let cursor = '';
  for (let index = 1; index <= 6; index++) {
    const next = index < 6 ? 'receipt-cursor-' + index : 'older-history';
    pages.set(cursor, feedPage([feedItem('item-' + index, index === 6 ? 'receipt' : 'notice')], {}, {next_cursor: next}));
    cursor = 'receipt-cursor-' + index;
  }
  const {feed, repository} = await opened(async (call) => {
    if (!call.path.includes('/feed')) return {};
    const pageCursor = new URL('https://hub.test' + call.path).searchParams.get('cursor') || '';
    return pages.get(pageCursor) || feedPage();
  });

  assert.equal(await feed.locateItem('item-6', 'receipt'), 'found');
  assert.ok(repository.calls.some((call) => call.path.includes('cursor=receipt-cursor-5')));
  assert.equal(feed.nextCursor, 'older-history');
});

test('detail lookup reports withdrawal only at the authoritative end and treats cursor loops as read errors', async () => {
  let calls = 0;
  const missingFixture = fixture(async (call) => {
    if (!call.path.includes('/feed')) return {};
    calls++;
    if (calls === 1) return feedPage([feedItem('head', 'notice')], {}, {next_cursor: 'cursor-a'});
    if (calls === 2) return feedPage([feedItem('middle', 'notice')], {}, {next_cursor: 'cursor-b'});
    return feedPage([feedItem('tail', 'notice')]);
  });
  const missingFeed = new AssistantFeedModel();
  await missingFeed.open(missingFixture.model, missingFixture.row, false);
  assert.equal(await missingFeed.locateItem('withdrawn-item', 'receipt'), 'missing');
  assert.equal(calls, 3, 'the old first four pages alone never claim an item is missing');

  const loopFixture = fixture(async (call) => {
    if (!call.path.includes('/feed')) return {};
    const pageCursor = new URL('https://hub.test' + call.path).searchParams.get('cursor') || '';
    const next = pageCursor === 'cursor-a' ? 'cursor-b' : 'cursor-a';
    return feedPage([feedItem('loop-' + (pageCursor || 'head'), 'notice')], {}, {next_cursor: next});
  });
  const loopFeed = new AssistantFeedModel();
  await loopFeed.open(loopFixture.model, loopFixture.row, false);
  assert.equal(await loopFeed.locateItem('absent', 'receipt'), 'error');
  assert.equal(loopFeed.error, 'failed');
});


test('legacy API-error receipts continue only the current interruption without a newer receipt or reroute',async()=>{
 const old=receipt('legacy','todo-receipt','done');old.body.summary='API Error: service unavailable';
 const task=receiptTodo({agents:[{machine_id:'machine-receipt',session_id:'session-receipt'}]});
 const {feed}=await opened(async call=>call.path.includes('/feed')?feedPage([old]):call.path.startsWith('/api/v1/todos')?{todos:[task]}:{},{sessions:[listedSession({state:'idle',turn_error:'overloaded'})]});
 assert.equal(feed.canContinueReceipt(old.id,task.id),true);
 feed.items=[old,{...receipt('newer',task.id,'interrupted'),at:'2026-11-12T00:00:00Z'}];assert.equal(feed.canContinueReceipt(old.id,task.id),false);
 feed.items=[old];task.agents[0].linked_at='2026-11-12T00:00:00Z';assert.equal(feed.canContinueReceipt(old.id,task.id),false);
});


test('a successful page retry reports its current success and a repeated cursor remains retryable',async()=>{
 let fail=true,repeat=false;const {feed}=await opened(async call=>{if(call.path.includes('cursor=tail')){if(fail)throw Error('offline');return feedPage([],{},{next_cursor:repeat?'tail':''});}if(call.path.includes('/feed'))return feedPage([]);if(call.path.startsWith('/api/v1/todos'))return {todos:[]};return {};});
 feed.nextCursor='tail';assert.equal(await feed.loadMore(),false);assert.ok(feed.error);assert.equal(feed.nextCursor,'tail');
 fail=false;repeat=true;assert.equal(await feed.loadMore(),false);assert.equal(feed.nextCursor,'tail','a cursor loop is not authoritative exhaustion');
 repeat=false;assert.equal(await feed.loadMore(),true);assert.equal(feed.error,'');assert.equal(feed.nextCursor,'');
});
