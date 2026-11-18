import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {TranscriptReducer, TranscriptSync} from '../entry/src/main/ets/core/Domain.ts';

test('native paged fixture spans advance the production transcript reducer without another fetch',()=>{
 const fixture=fileURLToPath(new URL('native-ui-fixture.py',import.meta.url));
 const pages=JSON.parse(execFileSync('python3',['-I','-c',`
import json, runpy, sys
fixture = runpy.run_path(sys.argv[1])
control = dict(reading_case=True, reading_prepend=True, reading_paged=True)
print(json.dumps([fixture['transcript'](after_seq=40, control=control),
                 fixture['transcript'](after_seq=59, control=dict(control, reading_append=True))]))
`,fixture],{encoding:'utf8'}));
 const reducer=new TranscriptReducer();
 reducer.restore({items:[],history:pages[0].history,covered:40,hasMore:true,loadedOnce:true});
 const sync=new TranscriptSync(reducer);
 for(const [index,cursor] of [40,59].entries()){
  assert.deepEqual(sync.continueFill(),[{kind:'fetch_after',afterSeq:cursor}]);
  const page=pages[index];assert.ok(page.items[0].seq>cursor);
  const result=sync.after({items:page.items,history:page.history,fromSeq:page.from_seq,toSeq:page.to_seq,
   hasMore:page.has_more,moreAfter:page.more_after,backlog:page.backlog});
  assert.deepEqual(result.actions,[],'an exclusive continuation boundary must not manufacture a gap');
  assert.equal(reducer.covered,page.to_seq);
 }
});
