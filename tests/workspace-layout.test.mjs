import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

// Actual UI script at the browser DOM/events and storage boundary; no relay state.
function layout({raw = null, blocked = false} = {}) {
  const nodes = [];
  function element(tag = 'div') {
    const events = new Map(), attributes = new Map();
    const node = {tag, children: [], dataset: {}, style: {setProperty() {}}, clientWidth: 1400, hidden: false, textContent: '',
      append(...items) {this.children.push(...items);}, before() {}, after() {},
      insertBefore(item) {this.children.push(item);}, setAttribute(name, value) {attributes.set(name, value);}, getAttribute(name) {return attributes.get(name);},
      addEventListener(name, fn) {events.set(name, fn);}, dispatch(name, event = {}) {events.get(name)?.({preventDefault() {}, ...event});},
      focus() {}, setPointerCapture() {}, getClientRects() {return [{}];}, querySelector() {return element();}};
    nodes.push(node); return node;
  }
  const grid = element(), header = element(), broadcastStatus = element();
  let saved = raw;
  const context = vm.createContext({
    document: {querySelector: selector => selector === '#live-console' ? grid : selector === '.studio-editor-grid' ? null : selector === '.workspace-header' ? header : broadcastStatus, createElement: element},
    window: {addEventListener() {}}, ResizeObserver: class {observe() {}},
    localStorage: {getItem() {if (blocked) throw new Error('denied'); return saved;}, setItem(key, value) {if (blocked) throw new Error('denied'); saved = value;}}
  });
  vm.runInContext(readFileSync(new URL('../internal/relay/web/workspace-layout.js', import.meta.url), 'utf8'), context);
  return {status: nodes.find(node => node.className === 'layout-status'), reset: nodes.find(node => node.textContent === 'Reset layout'), divider: nodes.find(node => node.getAttribute('role') === 'separator'), saved: () => JSON.parse(saved)};
}

test('malformed saved layout restores usable defaults and reset repairs the preference', () => {
  for (const raw of ['broken JSON', '{"version":2}', '{"version":1,"sizes":{"scenes":-20,"inspector":330,"signals":320}}']) {
    const ui = layout({raw});
    assert.equal(ui.status.hidden, false);
    assert.match(ui.status.textContent, /Default sizes restored/);
    assert.equal(ui.divider.getAttribute('aria-valuenow'), '320');
    ui.reset.dispatch('click');
    assert.deepEqual(ui.saved(), {version: 1, sizes: {scenes: 220, inspector: 330, signals: 320}});
    assert.match(ui.status.textContent, /Default layout restored/);
  }
});

test('unavailable storage reports temporary-only resizing and reset remains usable', () => {
  const ui = layout({blocked: true});
  assert.equal(ui.status.hidden, false);
  ui.divider.dispatch('keydown', {key: 'ArrowLeft'});
  assert.equal(ui.divider.getAttribute('aria-valuenow'), '330');
  assert.match(ui.status.textContent, /cannot be restored after reload/);
  ui.reset.dispatch('click');
  assert.equal(ui.divider.getAttribute('aria-valuenow'), '320');
  assert.match(ui.status.textContent, /storage is unavailable/);
});

test('keyboard resizing clamps widths and persists only the approved layout fields', () => {
  const ui = layout({raw: JSON.stringify({version: 1, sizes: {scenes: 220, inspector: 330, signals: 320, unrelated: 'not a layout preference'}})});
  ui.divider.dispatch('keydown', {key: 'End'});
  assert.equal(ui.divider.getAttribute('aria-valuenow'), '440');
  ui.divider.dispatch('keydown', {key: 'ArrowLeft', shiftKey: true});
  assert.equal(ui.divider.getAttribute('aria-valuenow'), '440');
  assert.deepEqual(ui.saved(), {version: 1, sizes: {scenes: 220, inspector: 330, signals: 440}});
  ui.divider.dispatch('keydown', {key: 'Home'});
  assert.equal(ui.divider.getAttribute('aria-valuenow'), '260');
});
