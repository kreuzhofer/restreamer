import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

// The browser boundary: status responses, rendered DOM, and elapsed time.
async function studio(status) {
  const elements = new Map();
  const element = id => {
    if (!elements.has(id)) elements.set(id, {textContent: '', hidden: false, classList: {toggle() {}}, setAttribute() {}, removeAttribute() {}});
    return elements.get(id);
  };
  let now = 1000, poll;
  const timers = [];
  const context = vm.createContext({
    window: {addEventListener() {}}, document: {body: {dataset: {}}, getElementById: id => id === 'live-workspace' ? null : element(id), querySelectorAll: () => []},
    location: {pathname: '/studio', origin: 'http://localhost'}, history: {},
    Date: {now: () => now}, AbortSignal,
    setInterval(fn) {timers.push(fn);}, setTimeout(fn) {poll = fn;},
    fetch: async () => ({ok: true, json: async () => status})
  });
  vm.runInContext(readFileSync(new URL('../internal/relay/web/workspace.js', import.meta.url), 'utf8'), context);
  await new Promise(resolve => setImmediate(resolve));
  return {element, async receive(next) {status = next; await poll();}, tick(seconds) {now += seconds * 1000; timers.forEach(fn => fn());}};
}
const base = {publishing: false, stage: {stage: 'PRESTREAM', source: 'file', mode: 'real'}, outputs: [{name:'A', enabled:true, can_enable:true, state:'streaming'}]};

test('prepared playback does not report absent OBS as a broadcast failure', async () => {
  const ui = await studio(base);
  assert.equal(ui.element('workspace-alert').hidden, true);
});

test('Studio reports partial destination failure by name while preserving workspace', async () => {
  const ui = await studio({...base, outputs: [...base.outputs, {name:'B', enabled:true, can_enable:true, state:'retrying'}]});
  assert.equal(ui.element('workspace-alert').hidden, false);
  assert.match(ui.element('workspace-alert-message').textContent, /1 of 2 destinations.*B/);
});

test('all destination failures and no enabled destinations are distinct', async () => {
  const failed = await studio({...base, outputs: [{name:'B', enabled:true, can_enable:true, state:'retrying'}]});
  assert.match(failed.element('workspace-alert-message').textContent, /All enabled destinations.*B/);
  const absent = await studio({...base, outputs: []});
  assert.match(absent.element('workspace-alert-message').textContent, /No eligible destination/);
});

test('OFF and rehearsal do not treat saved output failures as real delivery failures', async () => {
  for (const stage of [{stage:'OFF',source:'off',mode:'off'}, {stage:'LIVE',source:'obs',mode:'preview_only'}]) {
    const ui = await studio({...base,stage,outputs:[{name:'B',enabled:true,can_enable:true,state:'retrying'}]});
    assert.equal(ui.element('workspace-alert').hidden, true);
  }
});

test('stale status supersedes previously healthy delivery rather than showing current health', async () => {
  const ui = await studio(base);
  ui.tick(12);
  assert.match(ui.element('workspace-alert-message').textContent, /Status is stale/);
  assert.equal(ui.element('workspace-alert').hidden, false);
});

test('recovering delivery clears an alert without navigating away from Studio', async () => {
  const ui = await studio({...base,outputs:[{name:'A',enabled:true,can_enable:true,state:'retrying'}]});
  assert.equal(ui.element('workspace-alert').hidden, false);
  await ui.receive(base);
  assert.equal(ui.element('workspace-alert').hidden, true);
});

test('pending transitions and fallback BRB identify the actual retained source', async () => {
  const pending = await studio({...base,stage:{...base.stage,pending:{reason:'Waiting for a fresh keyframe'}}});
  assert.match(pending.element('workspace-alert-message').textContent,/Transition pending.*current source remains selected/);
  const fallback = await studio({...base,stage:{stage:'LIVE',mode:'real',source:'brb'}});
  assert.match(fallback.element('workspace-alert-message').textContent,/Fallback BRB is on air/);
});
