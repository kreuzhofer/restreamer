import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
const window = {};
vm.runInNewContext(readFileSync(new URL('../internal/relay/web/media-presentation.js', import.meta.url), 'utf8'), {window, Intl, Date});
const ui = window.mediaPresentation;
const job = (sequence, revision, overrides = {}) => ({sequence, media_revision: revision, state: 'ready', created_at: '2026-10-27T10:00:00Z', design_snapshot: {id:'design-a',name:'Workshop',stage:'prestream',theme:{id:'maker',revision:1},scenes:[{text:'Welcome',duration_seconds:10}],...overrides}});
test('render names are stable across deletion and never expose content hashes', () => {
  const jobs = [job(19, 'hash-a'), job(22, 'hash-b')];
  assert.equal(ui.renderName(jobs[1]), 'Render 22');
  assert.equal(ui.label('hash-b', jobs, []), 'Workshop · Render 22');
  assert.equal(ui.label('hash-b', jobs.slice(1), []), 'Workshop · Render 22');
  assert.equal(ui.label('missing-secret-hash', [], []), 'Unavailable render');
});
test('history describes captured changes rather than current draft contents', () => {
  const previous = job(1, 'one');
  const current = job(2, 'two', {scenes:[{text:'Starting soon',duration_seconds:10}],soundtrack:{asset:{id:'track',revision:1}}});
  assert.match(ui.changes(current, previous), /Scene content/);
  assert.match(ui.changes(current, previous), /Music/);
  assert.equal(ui.changes(previous, null), 'First retained render');
});
test('design chooser groups renders by design and retains unavailable assigned choices', () => {
  const jobs = [job(19,'a'), job(22,'b')];
  const groups = ui.groups('prestream', jobs, [{id:'a',state:'ready'},{id:'b',state:'ready'},{id:'upload',library_id:'file',name:'Uploaded clip',state:'ready'}]);
  assert.equal(groups.length, 2);
  assert.equal(groups[0].renders.length, 2);
  assert.equal(groups[0].renders[0].id, 'b');
  assert.equal(groups[1].name, 'Uploaded clip');
});
test('assignment changes only the chosen stage and preserves clip shortcuts and version', () => {
  const saved = {version:7,prestream:'old',ending:'end',shortcuts:[{name:'clip',revision:'clip'}]};
  const next = ui.selection(saved,'prestream','new');
  assert.equal(next.version,7); assert.equal(next.prestream,'new'); assert.equal(next.ending,'end');
  assert.equal(JSON.stringify(next.shortcuts), JSON.stringify(saved.shortcuts));
  assert.equal(saved.prestream,'old');
  assert.equal(ui.selection(saved,'brb','new').brb,'new');
  assert.equal(ui.selection(saved,'brb','new').ending,'end');
  assert.throws(() => ui.selection(saved,'unknown','new'));
});
test('latest video summary stays tied to the saved design and exposes newer edits',()=>{
 const draft={id:'design-a',version:3};
 const ready=job(3,'ready',{version:3});
 const jobs=[ready,{...job(4,'pending',{version:3}),state:'running'},job(5,'other',{id:'other-design',version:3})];
 assert.equal(ui.latestDesignVideo(draft,jobs,false).job.media_revision,'ready');
 assert.equal(ui.latestDesignVideo(draft,jobs,false).match,'Matches current design');
 assert.equal(ui.latestDesignVideo(draft,jobs,true).match,'Based on earlier changes');
 assert.equal(ui.latestDesignVideo({...draft,version:4},jobs,false).match,'Based on earlier changes');
 assert.equal(ui.latestDesignVideo({id:'empty'},jobs,false),null);
});
