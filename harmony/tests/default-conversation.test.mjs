import test from 'node:test';
import assert from 'node:assert/strict';
import { mainConversationData } from '../entry/src/main/ets/core/DefaultConversation.ts';

test('a migrated main conversation uses the machine named by the same response', () => {
  const response={main:'main',main_machine_id:'second',machine_id:'first',conversations:[{id:'main',machine_id:'first'},{id:'main',machine_id:'second'}]};
  assert.equal(mainConversationData(response).machine_id,'second');
});
test('a legacy main response supplies its inventory machine without using cached selection', () => {
  assert.deepEqual(mainConversationData({main:'main',machine_id:'device',conversations:[{id:'main',title:'Conversation'}]}),
    {id:'main',title:'Conversation',machine_id:'device',session_id:'main'});
});
test('missing or ambiguous main identities do not select an existing unrelated row', () => {
  for(const response of [{},{main:'new',machine_id:'a',conversations:[{id:'old',main:true}]},
    {main:'main',conversations:[{id:'main',machine_id:'a'}]},
    {main:'main',machine_id:'a',conversations:[{id:'main'},{id:'main'}]}]) assert.equal(mainConversationData(response),undefined);
});
