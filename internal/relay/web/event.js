'use strict';
(() => {
  const $ = id => document.getElementById(id);
  const base = location.pathname.replace(/\/$/, '');
  const video = $('event-video'), play = $('event-play');
  let source = '', hls, replay = false, busy = false, starting = false, playbackEpoch = 0;
  let pausedByViewer = false, audioChosen = false;
  function setMuted(muted) { video.muted = muted; audioControls(); }
  const screen = $('event-screen'), controls = $('event-controls'), poster = $('event-poster');
  const timeline = $('event-timeline'), preview = $('event-seek-preview'), thumbnail = $('event-thumbnail');
  let thumbnailIndex, indexLoading = false, scrubbing = false, previewTimer, pendingSeek, posterAttempts = 0, seekOperation = 0, previewResource = '';
  function label(button, text) { button.setAttribute('aria-label', text); button.title = text; }
  function playLabel(text) { label(play, text); play.dataset.playing = String(text === 'Pause playback'); screen.dataset.playing = String(!video.paused && !video.hidden); }
  function audioControls() {
    const muted = video.muted || video.volume === 0;
    label($('event-mute'), muted ? 'Unmute audio' : 'Mute audio'); $('event-mute').dataset.muted = String(muted);
    $('event-volume').value = String(video.volume);
  }
  function timestamp(seconds) {
    seconds = Math.max(0, Math.floor(Number(seconds) || 0));
    const hours = Math.floor(seconds / 3600), minutes = Math.floor(seconds / 60) % 60;
    return (hours ? hours + ':' + String(minutes).padStart(2, '0') : minutes) + ':' + String(seconds % 60).padStart(2, '0');
  }
  function duration() { return Number.isFinite(video.duration) && video.duration > 0 ? video.duration : thumbnailIndex?.duration || 0; }
  function updateTime() {
    const length = duration(), position = scrubbing ? Number(timeline.value) : video.currentTime || 0;
    timeline.max = String(length); timeline.disabled = !replay || !source || !length;
    if (!scrubbing) timeline.value = String(Math.min(length, position));
    timeline.setAttribute('aria-valuetext', timestamp(position) + ' of ' + timestamp(length));
    $('event-time').textContent = timestamp(position) + ' / ' + timestamp(length);
  }
  function hidePreview() { clearTimeout(previewTimer); previewResource = ''; preview.hidden = true; }
  function previewAt(position, fraction) {
    if (!replay || !source) return;
    preview.hidden = false; $('event-thumbnail-time').textContent = timestamp(position);
    const width = timeline.getBoundingClientRect().width, half = Math.min(96, width / 2);
    preview.style.left = Math.max(half, Math.min(width - half, width * fraction)) + 'px';
    const frames = thumbnailIndex?.frames;
    if (!frames?.length) { thumbnail.hidden = true; $('event-thumbnail-status').hidden = false; $('event-thumbnail-status').textContent = indexLoading ? 'Loading preview…' : 'Preview unavailable'; return; }
    let low = 0, high = frames.length - 1;
    while (low < high) { const mid = Math.ceil((low + high) / 2); if (frames[mid].time <= position) low = mid; else high = mid - 1; }
    const path = base + '/' + frames[low].resource;
    clearTimeout(previewTimer); previewResource = path;
    if (thumbnail.getAttribute('src') === path) { thumbnail.hidden = !thumbnail.complete || !thumbnail.naturalWidth; $('event-thumbnail-status').hidden = !thumbnail.hidden; return; }
    thumbnail.hidden = true; $('event-thumbnail-status').hidden = false; $('event-thumbnail-status').textContent = 'Loading preview…';
    clearTimeout(previewTimer); const epoch = playbackEpoch;
    previewTimer = setTimeout(() => { if (epoch === playbackEpoch && !preview.hidden) thumbnail.src = path; }, 100);
  }
  async function loadThumbnailIndex() {
    if (indexLoading || thumbnailIndex || !replay || !source) return;
    indexLoading = true; const epoch = playbackEpoch;
    try {
      const response = await fetch(base + '/thumbnails.json', {cache: 'no-store', signal: AbortSignal.timeout(10000)});
      if (!response.ok) throw new Error('Preview unavailable');
      const index = await response.json();
      if (!Number.isFinite(index.duration) || index.duration <= 0 || !Array.isArray(index.frames) || !index.frames.length || index.frames.some(frame => !Number.isFinite(frame.time) || frame.time < 0 || !/^thumbnail-[0-9]{8}\.jpg$/.test(frame.resource))) throw new Error('Preview unavailable');
      if (epoch !== playbackEpoch) return;
      thumbnailIndex = index; updateTime();
    } catch (_) { if (epoch === playbackEpoch) { $('event-thumbnail-status').textContent = 'Preview unavailable'; $('event-thumbnail-status').hidden = false; } }
    finally { if (epoch === playbackEpoch) indexLoading = false; }
  }
  thumbnail.onload = () => { if (thumbnail.getAttribute('src') !== previewResource || !thumbnail.complete) return; thumbnail.hidden = false; $('event-thumbnail-status').hidden = true; };
  thumbnail.onerror = () => { thumbnail.hidden = true; thumbnail.removeAttribute('src'); $('event-thumbnail-status').textContent = 'Preview unavailable'; $('event-thumbnail-status').hidden = false; };
  poster.onerror = () => {
    if (!replay || !source || !video.hidden) return;
    if (posterAttempts++ < 2) { const epoch = playbackEpoch; setTimeout(() => { if (epoch === playbackEpoch && video.hidden) { poster.removeAttribute('src'); poster.src = base + '/poster.jpg'; } }, 1000); }
    else { poster.hidden = true; $('event-placeholder').hidden = false; $('event-message').textContent = 'Preview unavailable. Select Play to watch the recording.'; }
  };
  timeline.onpointermove = event => { if (timeline.disabled) return; const rect = timeline.getBoundingClientRect(), fraction = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)); previewAt(fraction * duration(), fraction); };
  timeline.onpointerleave = () => { if (!scrubbing) hidePreview(); };
  timeline.onpointercancel = () => { scrubbing = false; hidePreview(); updateTime(); };
  timeline.onblur = () => { scrubbing = false; hidePreview(); updateTime(); };
  timeline.oninput = () => { if (!replay) return; scrubbing = true; const position = Number(timeline.value); previewAt(position, position / duration()); updateTime(); };
  timeline.onchange = async () => {
    if (!replay || !source) return;
    const epoch = playbackEpoch, operation = ++seekOperation, position = Math.max(0, Math.min(duration(), Number(timeline.value)));
    scrubbing = false; hidePreview(); pendingSeek = position;
    if (video.hidden) await startPlayback();
    if (epoch !== playbackEpoch || operation !== seekOperation || !replay || !source) return;
    if (video.readyState >= 1) { video.currentTime = position; pendingSeek = undefined; }
    updateTime();
  };
  video.addEventListener('loadedmetadata', () => { if (replay && pendingSeek !== undefined) { video.currentTime = pendingSeek; pendingSeek = undefined; } updateTime(); });
  video.addEventListener('durationchange', updateTime);
  video.addEventListener('timeupdate', updateTime);
  video.addEventListener('volumechange', audioControls);
  $('event-volume').oninput = () => { audioChosen = true; video.volume = Number($('event-volume').value); setMuted(video.volume === 0); };

  function stop() {
    // load() cancels queued media events. Capture a native pause before resetting
    // the resource, including a pause whose event has not been dispatched yet.
    if (source && !video.hidden && video.paused && !starting && !video.ended && !$('playback-error').textContent) pausedByViewer = true;
    playbackEpoch++; starting = false; play.disabled = false;
    thumbnailIndex = undefined; indexLoading = false; pendingSeek = undefined; scrubbing = false; posterAttempts = 0; hidePreview();
    poster.hidden = true; poster.removeAttribute('src'); thumbnail.removeAttribute('src');
    controls.hidden = true; $('event-seek').hidden = true; screen.dataset.playing = 'false';
    $('playback-error').textContent = '';
    if (hls) { hls.destroy(); hls = null; }
    video.removeAttribute('src'); video.load(); video.hidden = true;
    $('event-placeholder').hidden = false;
    $('event-mute').hidden = $('event-fullscreen').hidden = true;
  }
  function playbackError(message) { $('playback-error').textContent = message; playLabel('Retry playback'); controls.hidden = !source; $('event-retry').hidden = false; }
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
      const changed = next !== source;
      if (changed) { stop(); source = next; }
      play.hidden = !source; controls.hidden = !source;
      $('event-mute').hidden = $('event-fullscreen').hidden = !source;
      $('event-seek').hidden = !replay || !source; $('event-time').hidden = !replay; $('event-live').hidden = replay;
      if (replay && source) {
        if (video.hidden && !poster.getAttribute('src')) { poster.src = base + '/poster.jpg'; poster.hidden = false; $('event-placeholder').hidden = true; }
        loadThumbnailIndex(); updateTime();
      }
      if (video.paused && !$('playback-error').textContent) playLabel(pausedByViewer ? 'Resume playback' : replay ? 'Watch replay' : 'Watch live');
      video.controls = false;
      const message = live ? 'Live · playback follows the broadcast with a short delay.' : replay ? (event.incomplete ? 'Replay · this recording is incomplete.' : 'Replay available') : event.state === 'waiting' ? 'Waiting for the event to start.' : event.state === 'live' ? 'Broadcast temporarily unavailable. We’ll check again automatically.' : event.incomplete ? 'The broadcast was interrupted. No replay is currently available.' : 'This event has ended. No replay is currently available.';
      $('event-status').textContent = event.preview ? 'Private recording preview · ' + message : message; $('event-message').textContent = message;
      if (live && changed && !pausedByViewer) {
        if (!audioChosen) setMuted(true);
        startPlayback(true);
      }
    } catch (error) { $('event-status').textContent = error.message; $('event-retry').hidden = false; if (error.message.includes('no longer')) { stop(); source = ''; play.hidden = true; $('event-message').textContent = error.message; } }
    finally { busy = false; }
  }
  async function startPlayback(automatic = false) {
    if (!source || starting) return;
    const epoch = playbackEpoch;
    starting = true; play.disabled = true;
    $('playback-error').textContent = ''; $('event-retry').hidden = true;
    poster.hidden = true; controls.hidden = false;
    video.hidden = false; $('event-placeholder').hidden = true;
    $('event-mute').hidden = $('event-fullscreen').hidden = false;
    if (!video.getAttribute('src') && !hls) {
      if (video.canPlayType('application/vnd.apple.mpegurl')) video.src = source;
      else if (window.Hls?.isSupported()) {
        hls = new Hls({enableWorker: !base.startsWith('/events/'), liveSyncDurationCount: 3, maxBufferLength: 24, backBufferLength: replay ? 30 : 0});
        hls.on(Hls.Events.ERROR, (_event, data) => { if (data.fatal && epoch === playbackEpoch) { stop(); playbackError('Playback was interrupted. Try playback again.'); } });
        hls.loadSource(source); hls.attachMedia(video);
      } else { stop(); playbackError('This browser cannot play this event. Use a current version of Safari, Chrome, Firefox, or Edge.'); return; }
    }
    try { await video.play(); if (epoch === playbackEpoch) playLabel('Pause playback'); }
    catch (error) {
      if (epoch !== playbackEpoch) return;
      if (automatic && error.name === 'NotAllowedError') {
        playbackError('Your browser blocked automatic playback. Select Play to start.');
        playLabel('Watch live');
      } else playbackError('Playback could not start. Try playback again, or refresh the event.');
    } finally { if (epoch === playbackEpoch) { starting = false; play.disabled = false; } }
  }
  play.onclick = () => {
    if (!source || starting) return;
    if (!video.paused) { pausedByViewer = true; video.pause(); playLabel('Resume playback'); return; }
    pausedByViewer = false;
    return startPlayback();
  };
  video.addEventListener('pause', () => {
    if (source && !video.hidden && video.paused) { pausedByViewer = true; playLabel('Resume playback'); }
  });
  video.addEventListener('play', () => {
    if (source && !video.hidden && !video.paused) { pausedByViewer = false; playLabel('Pause playback'); }
  });
  video.addEventListener('error', () => playbackError('Playback failed. Refresh the event and try again.'));
  $('event-mute').onclick = () => { audioChosen = true; if (video.volume === 0) video.volume = 1; setMuted(!video.muted); };
  $('event-fullscreen').onclick = () => { if (document.fullscreenElement) return document.exitFullscreen().catch(() => playbackError('Full screen is unavailable.')); if (screen.requestFullscreen) return screen.requestFullscreen().catch(() => playbackError('Full screen is unavailable.')); if (video.webkitEnterFullscreen) video.webkitEnterFullscreen(); };
  $('event-login').onsubmit = async event => {
    event.preventDefault(); const button = event.target.querySelector('button'); button.disabled = true;
    try {
      const response = await fetch(base + '/login', {method: 'POST', headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: JSON.stringify({password: $('event-password').value}), signal: AbortSignal.timeout(10000)});
      if (!response.ok) throw new Error(await response.text());
      $('event-password').value = ''; $('login-error').textContent = ''; await refresh();
    } catch (error) { $('login-error').textContent = error.message; }
    finally { button.disabled = false; }
  };
  $('event-retry').onclick = () => { stop(); pausedByViewer = false; source = ''; return refresh(); };
  $('event-seek').hidden = true; poster.hidden = true; audioControls();
  refresh(); setInterval(refresh, 3000);
})();
