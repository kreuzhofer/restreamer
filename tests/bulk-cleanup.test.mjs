import test from 'node:test';import assert from 'node:assert/strict';import vm from 'node:vm';import {readFileSync} from 'node:fs';
const window={};vm.runInNewContext(readFileSync('internal/relay/web/bulk-cleanup.js','utf8'),{window});
test('cleanup reports protected failures and continues with remaining selections',async()=>{
 const calls=[];const result=await window.cleanupItems([{id:'a'},{id:'protected'},{id:'b'}],async item=>{calls.push(item.id);if(item.id==='protected')throw new Error('Selected for BRB');});
 assert.deepEqual(calls,['a','protected','b']);assert.equal(result.filter(r=>r.deleted).length,2);assert.equal(result.find(r=>!r.deleted).error,'Selected for BRB');
});
