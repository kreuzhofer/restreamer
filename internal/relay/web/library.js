'use strict';
(() => {
  const el = id => document.getElementById(id);
  let snapshot, connected = false, catalog = {files: []}, fetching = false, lastFetch = 0, pending = false, seeking = false, upload, error = '', catalogError = '';
  const clock = seconds => { const n = Math.max(0, Math.floor(seconds || 0)); return `${Math.floor(n / 60)}:${String(n % 60).padStart(2, '0')}`; };
  const selected = () => catalog.files.find(file => file.id === el('library-select').value);

  function render() {
    if (!snapshot) return;
    el('library-panel').hidden = !snapshot.library_enabled;
    if (!snapshot.library_enabled) return;
    const p = snapshot.playback || {}, file = selected(), usable = connected && !pending;
    el('library-count').textContent = `${catalog.files.length} ${catalog.files.length === 1 ? 'file' : 'files'}`;
    el('library-error').hidden = !(error || catalogError || catalog.error || p.error);
    el('library-error').textContent = error || catalogError || catalog.error || p.error || '';
    el('library-file-info').textContent = file ? `${file.state === 'discovering' ? 'Waiting for file copy to settle' : file.state === 'preparing' ? `Preparing · ${file.progress}%` : file.state} · ${clock(file.duration)} · ${(file.bytes / 1048576).toFixed(1)} MiB${file.error ? ` · ${file.error}` : ''}` : 'No MP4 files discovered yet.';
    el('library-once').disabled = el('library-loop').disabled = !usable || !snapshot.forwarding || file?.state !== 'ready' || !!catalog.error;
    el('library-prepare').disabled = !usable || !file || ['preparing', 'discovering', 'queued'].includes(file.state);
    el('library-upload-button').disabled = !connected || !!upload;
    el('library-file').disabled = !!upload;
    el('library-upload-cancel').hidden = !upload;
    el('playback-status').textContent = !snapshot.forwarding ? 'Master forwarding is off. Enable it to broadcast a video.' : p.id ? `${p.state === 'paused' ? p.pause_reason === 'manual_brb' ? 'Paused by manual BRB' : 'Paused · BRB on air' : 'Playing'}: ${p.name}${p.loop ? ' · looping' : ' · once'}` : `No file playing · ${p.source === 'obs' ? 'OBS on air' : 'BRB on air'}`;
    el('playback-pause').disabled = !usable || p.state !== 'playing';
    el('playback-resume').disabled = !usable || p.state !== 'paused' || snapshot.brb.manual;
    el('playback-stop').disabled = !usable || !p.id;
    const seek = el('playback-seek');
    seek.disabled = !usable || !p.id;
    seek.max = Math.max(0, Math.ceil(p.duration || 0) - 1);
    const preview = window.broadcastPreview?.position();
    const followsPreview = p.state === 'playing' && preview?.ready && preview.epoch === p.epoch;
    let position = followsPreview ? (preview.timeMS - p.timeline_base_ms) / 1000 : p.position || 0;
    if (followsPreview && p.loop && p.duration > 0) position = (position % p.duration + p.duration) % p.duration;
    position = Math.max(0, Math.min(p.duration || 0, position));
    if (!seeking) seek.value = position;
    el('playback-position').textContent = `${clock(seeking ? Number(seek.value) : position)} / ${clock(p.duration)} · ${seeking ? 'Release to seek' : followsPreview ? 'Preview position' : 'Server position'}`;
  }
  async function loadCatalog() {
    if (fetching || !connected || !snapshot?.library_enabled) return;
    fetching = true; lastFetch = Date.now();
    try {
      const response = await fetch(`${location.origin}/api/library`, {cache: 'no-store', signal: AbortSignal.timeout(8000)});
      if (!response.ok) throw new Error('Video library unavailable. Retrying…');
      catalog = await response.json();
      catalogError = '';
      const select = el('library-select'), previous = select.value;
      const options = catalog.files.map(file => new Option(`${file.name} — ${file.state}${file.state === 'preparing' ? ` ${file.progress}%` : ''}`, file.id));
      select.replaceChildren(...(options.length ? options : [new Option('No videos yet', '')]));
      if (catalog.files.some(file => file.id === previous)) select.value = previous;
    } catch (e) { catalogError = e.message; }
    finally { fetching = false; render(); }
  }
  async function command(action, extra = {}) {
    if (pending || !connected) return;
    pending = true; error = ''; render();
    try {
      const response = await fetch(`${location.origin}/api/playback`, {method: 'PUT', headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: JSON.stringify({action, ...extra}), signal: AbortSignal.timeout(10000)});
      if (!response.ok) throw new Error((await response.text()).trim() || 'Playback command failed');
      await refresh();
      await loadCatalog();
    } catch (e) { error = e.message; }
    finally { pending = false; seeking = false; render(); }
  }
  el('library-select').addEventListener('change', render);
  el('library-once').addEventListener('click', () => command('play', {id: selected()?.id, loop: false}));
  el('library-loop').addEventListener('click', () => command('play', {id: selected()?.id, loop: true}));
  el('library-prepare').addEventListener('click', () => command('prepare', {id: selected()?.id}));
  for (const action of ['pause', 'resume', 'stop']) el(`playback-${action}`).addEventListener('click', () => command(action));
  el('playback-seek').addEventListener('input', () => { seeking = true; render(); });
  el('playback-seek').addEventListener('change', () => command('seek', {position: Number(el('playback-seek').value)}));
  el('broadcast-video').addEventListener('timeupdate', render);
  el('library-upload').addEventListener('submit', event => {
    event.preventDefault();
    const file = el('library-file').files[0];
    if (!file || upload) return;
    if (file.size > catalog.upload_limit) {error = `Upload limit is ${(catalog.upload_limit / 1073741824).toFixed(1)} GiB`;render();return;}
    const data = new FormData();data.append('file', file);
    const xhr = new XMLHttpRequest();upload = xhr;error = '';
    xhr.open('POST', `${location.origin}/api/library/upload`);xhr.setRequestHeader('X-Restreamer-Control', '1');xhr.timeout = 7200000;
    xhr.upload.onprogress = e => { el('library-upload-status').textContent = e.lengthComputable ? `Uploading · ${Math.floor(e.loaded / e.total * 100)}%` : 'Uploading…'; };
    xhr.onload = () => {
      if (xhr.status === 202) {el('library-upload-status').textContent = 'Upload saved. Preparation will begin automatically.';el('library-file').value = '';}
      else error = xhr.responseText.trim() || 'Upload failed';
    };
    xhr.onerror = () => {error = 'Upload connection failed. Check the library before retrying.';};
    xhr.ontimeout = () => {error = 'Upload timed out.';};
    xhr.onabort = () => {el('library-upload-status').textContent = 'Upload cancelled. Check the library if the server had already finished receiving it.';};
    xhr.onloadend = () => {upload = undefined;loadCatalog();render();};
    el('library-upload-status').textContent = 'Uploading…';xhr.send(data);render();
  });
  el('library-upload-cancel').addEventListener('click', () => upload?.abort());
  window.updateLibrary = (data, online) => {
    snapshot = data; connected = online;
    window.broadcastPreview?.update(online && data.library_enabled && data.forwarding, data.playback?.epoch);
    if (Date.now() - lastFetch > 2500) loadCatalog();
    render();
  };
})();
