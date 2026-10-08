import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

// Exercise the actual profile form controller with a small DOM adapter.
function setup() {
  const elements = new Map();
  const el = id => {
    if (!elements.has(id)) elements.set(id, {value: '', options: [], hidden: false, disabled: false, textContent: '', events: {},
      classList: {toggle() {}}, add(option) {this.options.push(option);},
      addEventListener(name, fn) {this.events[name] = fn;}});
    return elements.get(id);
  };
  let data = {forwarding: false, brb: {ready: true}, brb_profile: {width: 1280, height: 720, fps: 25, sample_rate: 48000}};
  let online = true, busy = false, response = {ok: true}, requests = [];
  const context = vm.createContext({window: {}, document: {getElementById: el}, Option: function(text, value) {this.value = value;}, FormData, AbortSignal,
    location: {origin: 'http://localhost'}, fetch: async (url, init) => {requests.push(init);return response;},
    refresh: async () => update(), render: () => update()});
  vm.runInContext(readFileSync(new URL('../internal/relay/web/profile.js', import.meta.url), 'utf8'), context);
  const update = () => context.window.streamProfile.update(data, online, busy);
  update();
  return {el, update, requests, change(id, value) {el(id).value = value;el('profile-form').events.change();},
    cancel() {el('profile-cancel').events.click();}, save() {return el('profile-form').events.submit({preventDefault() {}});},
    state(next) {Object.assign(data, next);update();}, online(value) {online = value;update();}, busy(value) {busy = value;update();},
    respond(value) {response = value;}};
}

test('25 → 30 → 25 clears dirty state without saving or rebuilding', () => {
  const ui = setup();
  assert.equal(ui.el('profile-save').disabled, true);
  ui.change('profile-fps', '30');
  assert.equal(ui.el('profile-save').disabled, false);
  assert.equal(ui.el('profile-unsaved').hidden, false);
  ui.update(); // polling must preserve draft
  assert.equal(ui.el('profile-fps').value, '30');
  ui.change('profile-fps', '25');
  assert.equal(ui.el('profile-save').disabled, true);
  assert.equal(ui.el('profile-cancel').disabled, true);
  assert.equal(ui.el('profile-unsaved').hidden, true);
  assert.equal(ui.requests.length, 0);
});
test('all fields must match; cancel restores the entire saved profile', () => {
  const ui = setup();
  ui.change('profile-resolution', '1920x1080');
  ui.change('profile-sample-rate', '44100');
  ui.change('profile-fps', '30');
  ui.change('profile-fps', '25');
  assert.equal(ui.el('profile-save').disabled, false);
  ui.cancel();
  assert.equal(ui.el('profile-resolution').value, '1280x720');
  assert.equal(ui.el('profile-sample-rate').value, '48000');
  assert.equal(ui.el('profile-save').disabled, true);
  assert.equal(ui.requests.length, 0);
});
test('master on, disconnection and artwork preparation prevent saving', async () => {
  const ui = setup();ui.change('profile-fps', '30');
  for (const block of [() => ui.state({forwarding: true}), () => {ui.state({forwarding:false});ui.online(false);}, () => {ui.online(true);ui.busy(true);}]) {
    block();assert.equal(ui.el('profile-save').disabled, true);await ui.save();
  }
  assert.equal(ui.requests.length, 0);
});
test('profile save sends only profile values; failure preserves draft', async () => {
  const ui = setup();ui.change('profile-fps', '30');
  ui.respond({ok:false,text:async () => 'Preparation failed; previous profile remains active'});
  await ui.save();
  assert.deepEqual(Object.fromEntries(ui.requests[0].body), {width:'1280',height:'720',fps:'30',sample_rate:'48000'});
  assert.equal(ui.el('profile-fps').value, '30');
  assert.equal(ui.el('profile-save').disabled, false);
  assert.match(ui.el('profile-save-status').textContent, /Preparation failed/);
});
test('successful save adopts the refreshed active profile', async () => {
  const ui = setup();ui.change('profile-fps', '30');
  ui.respond({get ok() {ui.state({brb_profile:{width:1280,height:720,fps:30,sample_rate:48000}});return true;}});
  await ui.save();
  assert.equal(ui.el('profile-fps').value, '30');
  assert.equal(ui.el('profile-save').disabled, true);
  assert.equal(ui.el('profile-unsaved').hidden, true);
});
