import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const script = readFileSync(new URL('../internal/relay/web/event.js', import.meta.url), 'utf8');
const thumbnailIndex = {duration: 12, poster: 'poster.jpg', frames: [{time: 0, resource: 'thumbnail-00000000.jpg'}, {time: 6, resource: 'thumbnail-00000001.jpg'}]};
const live = {name: 'Test event', state: 'live', delivering: true, recorded: true, published: false};

// Execute the shipped viewer against browser HTTP/media boundaries. Decoding,
// actual autoplay policy and visual layout are intentionally outside this seam.
async function viewer(initial = live, {playError, native = true, thumbnails = thumbnailIndex, playGate} = {}) {
  let status = initial;
  const nodes = new Map(), timers = [], requests = [], hlsInstances = [];
  const element = id => {
    if (!nodes.has(id)) {
      const attributes = new Map(), listeners = new Map();
      let resourceEpoch = 0;
      const node = {hidden: false, textContent: '', value: '', disabled: false, dataset: {}, style: {},
        setAttribute: (key, value) => attributes.set(key, String(value)),
        getBoundingClientRect: () => ({left: 0, width: 600}),
        requestFullscreen: async () => {node.fullscreenRequested = true;},
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
  Object.assign(video, {paused: true, muted: false, volume: 1, currentTime: 0, duration: 12, readyState: 1, plays: [], canPlayType: () => native ? 'probably' : '',
    pause() {const wasPaused = this.paused; this.paused = true; if (!wasPaused) this.dispatch('pause');}, load() {this.cancelMediaEvents(); this.paused = true;},
    async play() {this.plays.push({muted: this.muted, src: this.src}); if (playError) throw playError; if (playGate) await playGate; this.paused = false; this.dispatch('play');}});
  class Hls {
    static Events = {ERROR: 'error'};
    static isSupported() {return true;}
    constructor() {this.handlers = {}; hlsInstances.push(this);}
    on(name, handler) {this.handlers[name] = handler;}
    loadSource(source) {this.source = source;}
    attachMedia(media) {this.media = media;}
    destroy() {this.destroyed = true;}
  }
  const context = {document: {getElementById: element, addEventListener() {}}, location: {pathname: '/watch/test-event'},
    window: {Hls}, Hls, AbortSignal,
    fetch: async (url, options) => {requests.push({url, options}); return {ok: true, status: 200, json: async () => url.endsWith('/thumbnails.json') ? thumbnails : status};},
    setTimeout, clearTimeout,
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
  assert.equal(page.element('event-mute').getAttribute('aria-label'), 'Unmute audio');
  assert.equal(page.element('event-play-overlay').hidden, true);
  assert.equal(page.element('event-speed').hidden, true);
  assert.equal(page.video.playbackRate, 1);
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
  assert.equal(page.element('event-play').getAttribute('aria-label'), 'Resume playback');
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
  assert.equal(page.element('event-mute').getAttribute('aria-label'), 'Mute audio');
  await page.element('event-retry').onclick();
  await new Promise(setImmediate);
  assert.equal(page.video.plays.length, 2);
  assert.equal(page.video.muted, false, 'keep the viewer audio choice within the page');
});

test('autoplay rejection leaves an enabled manual fallback without repeated attempts', async () => {
  const page = await viewer(live, {playError: Object.assign(new Error('blocked'), {name: 'NotAllowedError'})});
  assert.match(page.element('playback-error').textContent, /browser blocked automatic playback/i);
  assert.equal(page.element('event-play').disabled, false);
  assert.equal(page.element('event-play').getAttribute('aria-label'), 'Watch live');
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

test('replay has a prominent play action before playback and when paused', async () => {
  const page = await viewer({...live, state: 'ended', published: true, delivering: false});
  const overlay = page.element('event-play-overlay');
  assert.equal(overlay.hidden, false);
  assert.equal(overlay.getAttribute('aria-label'), 'Play replay');
  assert.equal(page.element('event-poster').hidden, false);
  await overlay.onclick();
  assert.equal(page.video.paused, false);
  assert.equal(overlay.hidden, true);
  await page.element('event-play').onclick();
  assert.equal(overlay.hidden, false);
  assert.equal(overlay.getAttribute('aria-label'), 'Resume replay');
  await overlay.onclick();
  assert.equal(page.video.paused, false);
  assert.equal(overlay.hidden, true);
});

test('replay play overlay returns after a failed start and at the end', async () => {
  const replay = {...live, state: 'ended', published: true, delivering: false};
  const failed = await viewer(replay, {playError: new Error('decode failed')});
  await failed.element('event-play-overlay').onclick();
  assert.equal(failed.element('event-play-overlay').hidden, false);
  assert.match(failed.element('playback-error').textContent, /could not start/);
  const page = await viewer(replay);
  await page.element('event-play-overlay').onclick();
  page.video.paused = true; page.video.ended = true; page.video.dispatch('ended');
  await new Promise(setImmediate);
  assert.equal(page.element('event-play-overlay').hidden, false);
  assert.equal(page.element('event-play-overlay').getAttribute('aria-label'), 'Play replay again');
});

test('replay speed supports quarter steps from half to double speed and survives pause and polling', async () => {
  const page = await viewer({...live, state: 'ended', published: true, delivering: false});
  const speed = page.element('event-speed');
  assert.equal(speed.hidden, false);
  assert.equal(speed.value, '1');
  for (const rate of [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]) {
    speed.value = String(rate); speed.onchange();
    assert.equal(page.video.playbackRate, rate);
  }
  await page.element('event-play-overlay').onclick();
  await page.element('event-play').onclick();
  await page.poll();
  await page.element('event-play-overlay').onclick();
  assert.equal(page.video.playbackRate, 2);
  assert.equal(speed.value, '2');
  await page.element('event-retry').onclick();
  await page.element('event-play-overlay').onclick();
  assert.equal(page.video.playbackRate, 2, 'refreshing the media keeps the selected speed');
  for (const invalid of ['0', '3', '1.1', 'NaN']) {
    speed.value = invalid; speed.onchange();
    assert.equal(page.video.playbackRate, 2);
  }
  page.setStatus(live); await page.poll();
  assert.equal(speed.hidden, true);
  assert.equal(page.video.playbackRate, 1, 'replay speed must not carry over into live playback');
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


test('replay opens on its first frame with thumbnail seeking before playback', async () => {
  const page = await viewer({...live, state: 'ended', published: true, delivering: false});
  assert.equal(page.video.plays.length, 0);
  assert.equal(page.element('event-poster').src, '/watch/test-event/poster.jpg');
  assert.equal(page.element('event-poster').hidden, false);
  assert.equal(page.element('event-timeline').disabled, false);
  page.element('event-timeline').onpointermove({clientX: 450});
  await new Promise(resolve => setTimeout(resolve, 120));
  assert.equal(page.element('event-thumbnail').src, '/watch/test-event/thumbnail-00000001.jpg');
  assert.equal(page.element('event-thumbnail-time').textContent, '0:09');
  assert.equal(page.video.currentTime, 0, 'hovering must not seek the main video');
  const timeline = page.element('event-timeline');
  timeline.value = '7.5'; timeline.oninput(); await timeline.onchange();
  assert.equal(page.video.currentTime, 7.5);
  assert.equal(page.video.plays.length, 1);
  assert.equal(page.element('event-poster').hidden, true);
});

test('live controls stay in the player without a replay timeline or thumbnail requests', async () => {
  const page = await viewer();
  assert.equal(page.element('event-seek').hidden, true);
  assert.equal(page.requests.some(request => request.url.endsWith('/thumbnails.json')), false);
  page.element('event-volume').value = '0.3'; page.element('event-volume').oninput();
  assert.equal(page.video.volume, 0.3);
  assert.equal(page.video.muted, false);
  await page.element('event-fullscreen').onclick();
  assert.equal(page.element('event-screen').fullscreenRequested, true);
});

test('an expired thumbnail response cannot revive a withdrawn replay', async () => {
  let finish;
  const thumbnails = new Promise(resolve => {finish = resolve;});
  const page = await viewer({...live, state: 'ended', published: true, delivering: false}, {thumbnails});
  page.setStatus({...live, state: 'ended', published: false, delivering: false}); await page.poll();
  finish(thumbnailIndex); await new Promise(setImmediate);
  assert.equal(page.element('event-poster').hidden, true);
  assert.equal(page.element('event-seek').hidden, true);
  assert.equal(page.element('event-play-overlay').hidden, true);
});


test('a delayed seek cannot move the replacement live stream', async () => {
  let resume;
  const playGate = new Promise(resolve => {resume = resolve;});
  const page = await viewer({...live, state: 'ended', published: true, delivering: false}, {playGate});
  const timeline = page.element('event-timeline'); timeline.value = '7.5'; timeline.oninput();
  const seek = timeline.onchange();
  assert.equal(page.element('event-play-overlay').hidden, true, 'hide the action while playback is starting');
  page.setStatus(live); await page.poll();
  resume(); await seek; await new Promise(setImmediate);
  assert.equal(page.video.currentTime, 0, 'stale replay seek must not rewind live media');
});

test('the latest replay seek wins while playback is starting', async () => {
  let resume;
  const playGate = new Promise(resolve => {resume = resolve;});
  const page = await viewer({...live, state: 'ended', published: true, delivering: false}, {playGate});
  const timeline = page.element('event-timeline');
  timeline.value = '3'; timeline.oninput(); const first = timeline.onchange();
  timeline.value = '9'; timeline.oninput(); await timeline.onchange();
  resume(); await first;
  assert.equal(page.video.currentTime, 9);
});

test('returning to the displayed thumbnail cancels a pending different preview', async () => {
  const page = await viewer({...live, state: 'ended', published: true, delivering: false});
  const timeline = page.element('event-timeline'), image = page.element('event-thumbnail');
  timeline.onpointermove({clientX: 100}); await new Promise(resolve => setTimeout(resolve, 120));
  image.complete = true; image.naturalWidth = 192; image.onload();
  timeline.onpointermove({clientX: 450}); timeline.onpointermove({clientX: 100});
  await new Promise(resolve => setTimeout(resolve, 120));
  assert.equal(image.src, '/watch/test-event/thumbnail-00000000.jpg');
  assert.equal(image.hidden, false);
  assert.equal(page.element('event-thumbnail-time').textContent, '0:02');
});
