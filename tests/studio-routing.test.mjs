import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';
const window = {};
vm.runInNewContext(readFileSync(new URL('../internal/relay/web/studio-routing.js', import.meta.url),'utf8'), {window, URL});
const route = path => window.studioRouting.parse(new URL(path, 'http://localhost'));
test('four destinations have direct, refreshable routes', () => {
  for (const page of ['live','setup','library','settings']) assert.equal(route('/'+page).page,page);
});
test('legacy studio bookmarks retain their task context', () => {
  assert.equal(route('/studio').page,'setup');
  assert.equal(route('/studio?collection=music').collection,'music');
  assert.equal(route('/studio?collection=music').page,'library');
  assert.equal(route('/studio?id=abc').design,'abc');
  assert.equal(route('/studio?editor=brb').editor,'brb');
});
test('editor returns only to supported local destinations', () => {
  assert.equal(route('/editor?id=abc&return=%2Fsetup').back,'/setup');
  assert.equal(route('/editor?id=abc&return=https%3A%2F%2Fevil.test').back,'/setup');
  assert.equal(route('/library?collection=unknown').collection,'designs');
});

test('editor retains a local Library filter without accepting external return paths',()=>{
 assert.equal(route('/editor?id=abc&return='+encodeURIComponent('/library?collection=designs&q=workshop')).back,'/library?collection=designs&q=workshop');
 assert.equal(route('/editor?id=abc&return='+encodeURIComponent('//evil.test/library')).back,'/setup');
});
