'use strict';
(() => {
  const $ = id => document.getElementById(id);
  const base = location.pathname.replace(/\/$/, '');
  const video = $('event-video'), play = $('event-play');
  let source = '', hls, replay = false, busy = false;
  function stop() {
    if (hls) { hls.destroy(); hls = null; }
    video.pause(); video.removeAttribute('src'); video.load(); video.hidden = true;
    $('event-placeholder').hidden = false;
    $('event-mute').hidden = $('event-fullscreen').hidden = true;
  }
  function playbackError(message) { $('playback-error').textContent = message; play.textContent = 'Retry playback'; }
  async function refresh() {
    if (busy) return;
    busy = true;
    try {
      const response = await fetch(base + '/status', {cache: 'no-store', signal: AbortSignal.timeout(10000)});
      if (response.status === 401) {
        stop(); source = ''; play.hidden = true; $('event-login').hidden = false;
        $('event-status').textContent = 'Password required'; $('event-message').textContent = 'Enter the event password to watch.'; return;
      }
      if (!response.ok) throw new Error(response.status === 404 ? 'This event is no longer available.' : 'Cannot connect to the event. Try Refresh event.');
      const event = await response.json();
      $('event-login').hidden = true; $('event-name').textContent = event.name; document.title = event.name;
      replay = event.published && event.state !== 'live';
      const live = event.state === 'live' && event.delivering && event.recorded;
      const next = live ? base + '/live.m3u8' : replay ? base + '/replay.m3u8' : '';
      if (next !== source) { stop(); source = next; }
      play.hidden = !source;
      if (video.paused) play.textContent = replay ? 'Watch replay' : 'Watch live';
      video.controls = replay;
      const message = live ? 'Live · playback follows the broadcast with a short delay.' : replay ? (event.incomplete ? 'Replay · this recording is incomplete.' : 'Replay available') : event.state === 'waiting' ? 'Waiting for the event to start.' : event.state === 'live' ? 'Broadcast temporarily unavailable. We’ll check again automatically.' : event.incomplete ? 'The broadcast was interrupted. No replay is currently available.' : 'This event has ended. No replay is currently available.';
      $('event-status').textContent = event.preview ? 'Private recording preview · ' + message : message; $('event-message').textContent = message;
    } catch (error) { $('event-status').textContent = error.message; if (error.message.includes('no longer')) { stop(); source = ''; play.hidden = true; $('event-message').textContent = error.message; } }
    finally { busy = false; }
  }
  play.onclick = async () => {
    if (!source) return;
    if (!video.paused) { video.pause(); play.textContent = 'Resume playback'; return; }
    $('playback-error').textContent = '';
    video.hidden = false; $('event-placeholder').hidden = true;
    $('event-mute').hidden = $('event-fullscreen').hidden = false;
    if (!video.getAttribute('src') && !hls) {
      if (video.canPlayType('application/vnd.apple.mpegurl')) video.src = source;
      else if (window.Hls?.isSupported()) {
        hls = new Hls({enableWorker: !base.startsWith('/events/'), liveSyncDurationCount: 3, maxBufferLength: 24, backBufferLength: replay ? 30 : 0});
        hls.on(Hls.Events.ERROR, (_event, data) => { if (data.fatal) { stop(); playbackError('Playback was interrupted. Try playback again.'); } });
        hls.loadSource(source); hls.attachMedia(video);
      } else { stop(); playbackError('This browser cannot play this event. Use a current version of Safari, Chrome, Firefox, or Edge.'); return; }
    }
    try { await video.play(); play.textContent = 'Pause playback'; }
    catch (_) { playbackError('Select Watch again to start playback, or refresh if the connection was interrupted.'); }
  };
  video.addEventListener('error', () => playbackError('Playback failed. Refresh the event and try again.'));
  $('event-mute').onclick = () => { video.muted = !video.muted; $('event-mute').textContent = video.muted ? 'Unmute audio' : 'Mute audio'; };
  $('event-fullscreen').onclick = () => { if (video.requestFullscreen) video.requestFullscreen().catch(() => playbackError('Full screen is unavailable.')); else if (video.webkitEnterFullscreen) video.webkitEnterFullscreen(); };
  $('event-login').onsubmit = async event => {
    event.preventDefault(); const button = event.target.querySelector('button'); button.disabled = true;
    try {
      const response = await fetch(base + '/login', {method: 'POST', headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: JSON.stringify({password: $('event-password').value}), signal: AbortSignal.timeout(10000)});
      if (!response.ok) throw new Error(await response.text());
      $('event-password').value = ''; $('login-error').textContent = ''; await refresh();
    } catch (error) { $('login-error').textContent = error.message; }
    finally { button.disabled = false; }
  };
  $('event-retry').onclick = () => { stop(); refresh(); };
  refresh(); setInterval(refresh, 3000);
})();
