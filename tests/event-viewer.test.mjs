import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const script = readFileSync(new URL('../internal/relay/web/event.js', import.meta.url), 'utf8');
const live = {name: 'Test event', state: 'live', delivering: true, recorded: true, published: false};

// Execute the shipped viewer against browser HTTP/media boundaries. Decoding,
// actual autoplay policy and visual layout are intentionally outside this seam.
async function viewer(initial = live, {playError, native = true} = {}) {
  let status = initial;
  const nodes = new Map(), timers = [], requests = [], hlsInstances = [];
  const element = id => {
    if (!nodes.has(id)) {
      const attributes = new Map(), listeners = new Map();
      let resourceEpoch = 0;
      const node = {hidden: false, textContent: '', value: '', disabled: false,
        getAttribute: key => attributes.get(key) ?? null,
        removeAttribute: key => attributes.delete(key),
        addEventListener: (name, listener) => listeners.set(name, listener),
        cancelMediaEvents: () => {resourceEpoch++;},
        dispatch: name => {const epoch = resourceEpoch; setImmediate(() => {if (epoch === resourceEpoch) listeners.get(name)?.();});},
        querySelector: () => element(id + '-button')};
      Object.defineProperty(node, 'src', {get: () => attributes.get('src') || '', set: value => attributes.set('src', value)});
      nodes.set(id, node);
    }
    return nodes.get(id);
  };
  const video = element('event-video');
  Object.assign(video, {paused: true, muted: false, plays: [], canPlayType: () => native ? 'probably' : '',
    pause() {const wasPaused = this.paused; this.paused = true; if (!wasPaused) this.dispatch('pause');}, load() {this.cancelMediaEvents(); this.paused = true;},
    async play() {this.plays.push({muted: this.muted, src: this.src}); if (playError) throw playError; this.paused = false; this.dispatch('play');}});
  class Hls {
    static Events = {ERROR: 'error'};
    static isSupported() {return true;}
    constructor() {this.handlers = {}; hlsInstances.push(this);}
    on(name, handler) {this.handlers[name] = handler;}
    loadSource(source) {this.source = source;}
    attachMedia(media) {this.media = media;}
    destroy() {this.destroyed = true;}
  }
  const context = {document: {getElementById: element}, location: {pathname: '/watch/test-event'},
    window: {Hls}, Hls, AbortSignal,
    fetch: async (url, options) => {requests.push({url, options}); return {ok: true, status: 200, json: async () => status};},
    setInterval: fn => {timers.push(fn); return timers.length;}};
  vm.runInNewContext(script, context, {filename: 'event.js'});
  await new Promise(setImmediate);
  return {video, element, requests, hlsInstances, setStatus: value => {status = value;}, poll: async () => {await timers[0](); await new Promise(setImmediate);}};
}

test('opening or reloading an already-live event starts video without Watch live', async () => {
  const page = await viewer();
  assert.equal(page.video.plays.length, 1, 'live status must attempt playback without a click');
  assert.equal(page.video.muted, true, 'automatic startup must satisfy muted-autoplay policies');
  assert.equal(page.video.paused, false);
  assert.equal(page.element('event-mute').textContent, 'Unmute audio');
});

test('a waiting viewer starts automatically when live media becomes available', async () => {
  const page = await viewer({...live, state: 'waiting', delivering: false, recorded: false});
  assert.equal(page.video.plays.length, 0);
  page.setStatus({...live, recorded: false}); await page.poll();
  assert.equal(page.video.plays.length, 0, 'wait until there is playable media');
  page.setStatus(live); await page.poll();
  assert.equal(page.video.plays.length, 1);
  await page.poll();
  assert.equal(page.video.plays.length, 1, 'polling must not restart a playing stream');
});

test('automatic startup attaches HLS for browsers without native support', async () => {
  const page = await viewer(live, {native: false});
  assert.equal(page.hlsInstances.length, 1);
  assert.equal(page.hlsInstances[0].source, '/watch/test-event/live.m3u8');
  assert.equal(page.hlsInstances[0].media, page.video);
  assert.equal(page.video.plays.length, 1);
});

test('a viewer pause survives polling and a temporary broadcast gap', async () => {
  const page = await viewer();
  await page.element('event-play').onclick();
  assert.equal(page.video.paused, true);
  await page.poll();
  assert.equal(page.element('event-play').textContent, 'Resume playback');
  page.setStatus({...live, delivering: false}); await page.poll();
  page.setStatus(live); await page.poll();
  assert.equal(page.video.plays.length, 1, 'automatic recovery must respect an intentional pause');
  await page.element('event-play').onclick();
  assert.equal(page.video.paused, false);
});

test('unmuting is explicit and refresh starts live playback again', async () => {
  const page = await viewer();
  page.element('event-mute').onclick();
  assert.equal(page.video.muted, false);
  assert.equal(page.element('event-mute').textContent, 'Mute audio');
  await page.element('event-retry').onclick();
  await new Promise(setImmediate);
  assert.equal(page.video.plays.length, 2);
  assert.equal(page.video.muted, false, 'keep the viewer audio choice within the page');
});

test('autoplay rejection leaves an enabled manual fallback without repeated attempts', async () => {
  const page = await viewer(live, {playError: Object.assign(new Error('blocked'), {name: 'NotAllowedError'})});
  assert.match(page.element('playback-error').textContent, /browser blocked automatic playback/i);
  assert.equal(page.element('event-play').disabled, false);
  assert.equal(page.element('event-play').textContent, 'Watch live');
  await page.poll();
  assert.equal(page.video.plays.length, 1);
});

test('replays retain deliberate playback instead of starting automatically', async () => {
  const page = await viewer({...live, state: 'ended', published: true, delivering: false});
  assert.equal(page.video.plays.length, 0);
  await page.element('event-play').onclick();
  assert.equal(page.video.plays.length, 1);
  assert.equal(page.video.src, '/watch/test-event/replay.m3u8');
});


test('native fullscreen pause and resume preserve the viewer intent across broadcast gaps', async () => {
  const page = await viewer();
  page.video.pause();
  page.setStatus({...live, delivering: false}); await page.poll();
  page.setStatus(live); await page.poll();
  assert.equal(page.video.plays.length, 1, 'a native pause must not be undone by recovery');
  const resumed = await viewer();
  resumed.video.pause();
  await resumed.video.play();
  resumed.setStatus({...live, delivering: false}); await resumed.poll();
  resumed.setStatus(live); await resumed.poll();
  assert.equal(resumed.video.plays.length, 3, 'after a native resume, recovery should start playback again');
});


test('resource resets cannot consume the next genuine native pause', async () => {
  const page = await viewer();
  page.setStatus({...live, delivering: false}); await page.poll();
  page.setStatus(live); await page.poll();
  assert.equal(page.video.plays.length, 2);
  page.video.pause(); await new Promise(setImmediate);
  page.setStatus({...live, delivering: false}); await page.poll();
  page.setStatus(live); await page.poll();
  assert.equal(page.video.plays.length, 2, 'the pause after a resource reset must remain effective');
});
