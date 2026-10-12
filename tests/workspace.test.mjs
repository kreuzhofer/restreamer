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

test('a failed destination remains alerted during reconnect attempts until media resumes', async () => {
  const ui = await studio({...base,outputs:[{name:'A',enabled:true,can_enable:true,state:'retrying',last_error:'connect_failed'}]});
  await ui.receive({...base,outputs:[{name:'A',enabled:true,can_enable:true,state:'connecting',last_error:'connect_failed'}]});
  assert.equal(ui.element('workspace-alert').hidden, false);
  await ui.receive(base);
  assert.equal(ui.element('workspace-alert').hidden, true);
});

function navigation(onDashboard) {
  const events = new EventTarget();
  let answer = false, prompts = 0, changed = false;
  const listeners = {}, link = {dataset: {workspace: 'studio'}, href: '/studio', getAttribute() {return '/' + this.dataset.workspace;}, addEventListener(name, fn) {listeners[name] = fn;}, setAttribute() {}, removeAttribute() {}};
  const element = {textContent:'',classList:{toggle() {}},setAttribute(){}};
  const window = {addEventListener:events.addEventListener.bind(events),dispatchEvent:events.dispatchEvent.bind(events),confirm(){prompts++;return answer;}};
  events.addEventListener('beforeunload', event => {if (changed && !window.workspace?.departureApproved) event.preventDefault();});
  vm.runInNewContext(readFileSync(new URL('../internal/relay/web/workspace.js',import.meta.url),'utf8'),{window,Event,document:{body:{dataset:{}},getElementById:id=>id==='live-workspace'?onDashboard?element:null:element,querySelectorAll:selector=>selector.includes('workspace-nav')?[link]:[],querySelector:()=>null},location:{pathname:onDashboard?'/settings':'/studio'},history:{pushState(){}},setInterval(){},setTimeout(){},fetch:async()=>({ok:false}),AbortSignal});
  return {dirty(){changed=true;},answer(value){answer=value;},prompts:()=>prompts,click(workspace,modifier=false){link.dataset.workspace=workspace;const event={button:0,ctrlKey:modifier,preventDefault(){this.defaultPrevented=true;}};listeners.click(event);return !!event.defaultPrevented;},unload(){const e=new Event('beforeunload',{cancelable:true});events.dispatchEvent(e);return e.defaultPrevented;}};
}

test('cross-document workspace departure explicitly reviews dirty forms and keeps them on cancel',()=>{
 const ui=navigation(true);ui.dirty();assert.equal(ui.click('studio'),true);assert.equal(ui.prompts(),1);assert.equal(ui.unload(),true);
 ui.answer(true);assert.equal(ui.click('studio'),false);assert.equal(ui.unload(),false);
});
test('same-document and new-tab navigation preserve drafts without discard prompts',()=>{
 const ui=navigation(true);ui.dirty();assert.equal(ui.click('live'),true);assert.equal(ui.prompts(),0);assert.equal(ui.click('studio',true),false);assert.equal(ui.prompts(),0);assert.equal(ui.unload(),true);
});
test('Studio drafts are reviewed before leaving for Live or Settings',()=>{
 for(const view of ['live','settings']){const ui=navigation(false);ui.dirty();assert.equal(ui.click(view),true);assert.equal(ui.prompts(),1);}
});
