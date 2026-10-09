'use strict';
(() => {
  const el = id => document.getElementById(id);
  let snapshot, connected = false, catalog = {files: [], revisions: []}, fetching = false, lastFetch = 0, pending = false, seeking = false, upload, error = '', catalogError = '';
  let saved = {prestream: '', ending: '', shortcuts: []}, draft, saving = false, previewRevision = '';
  const clock = seconds => { const n = Math.max(0, Math.floor(seconds || 0)); return `${Math.floor(n / 60)}:${String(n % 60).padStart(2, '0')}`; };
  const selected = () => catalog.files.find(file => file.id === el('library-select').value);
  const revision = () => catalog.revisions.find(item => item.id === el('library-revision').value);
  const ready = item => item?.state === 'ready';
  const copy = value => JSON.parse(JSON.stringify(value));

  function render() {
    if (!snapshot) return;
    el('library-panel').hidden = !snapshot.library_enabled;
    if (!snapshot.library_enabled) return;
    const stage = snapshot.stage || {}, p = stage.playback || {}, file = selected(), candidate = revision();
    const usable = connected && !pending && !window.broadcastStages?.busy(), clip = stage.stage === 'CLIP', ending = window.broadcastStages?.locked();
    el('library-count').textContent = `${catalog.files.length} ${catalog.files.length === 1 ? 'file' : 'files'}`;
    el('library-error').hidden = !(error || catalogError || catalog.error);
    el('library-error').textContent = error || catalogError || catalog.error || '';
    el('library-file-info').textContent = file ? `${file.state === 'discovering' ? 'Waiting for file copy to settle' : file.state === 'preparing' ? `Preparing next revision · ${file.progress}%` : file.state} · ${clock(file.duration)} · ${(file.bytes / 1048576).toFixed(1)} MiB${file.message ? ` · ${file.message}` : ''}${file.error ? ` · ${file.error}` : ''}` : 'No MP4 files discovered yet.';
    el('library-once').disabled = el('library-loop').disabled = !usable || stage.stage === 'OFF' || ending || !ready(candidate) || !!catalog.error;
    el('library-preview').disabled = !connected || !ready(candidate);
    el('library-prepare').disabled = !usable || !file || ['preparing', 'discovering', 'queued'].includes(file.state);
    el('library-upload-button').disabled = !connected || !!upload;
    el('library-file').disabled = !!upload;
    el('library-upload-cancel').hidden = !upload;
    el('playback-status').textContent = stage.stage === 'OFF' ? 'Start Prestream or Go live first. Clip shortcuts cannot start a broadcast.' : clip ? `${p.state === 'failed' ? 'Playback failed · BRB on air' : p.state === 'paused' ? 'Paused · BRB on air' : 'Playing'}: ${p.name || stage.media?.name}${p.loop ? ' · looping' : ' · once'} · returns to ${stage.return_stage || 'the recorded stage'}` : `${stage.stage} selected. Detailed playback controls are available only for CLIP.`;
    el('playback-pause').disabled = !usable || !clip || p.state !== 'playing';
    el('playback-resume').disabled = !usable || !clip || p.state !== 'paused';
    el('playback-stop').disabled = !usable || !clip;
    el('playback-loop').disabled = !usable || !clip || p.state === 'failed';
    el('playback-loop').checked = !!p.loop;
    const seek = el('playback-seek');
    seek.disabled = !usable || !clip || !p.id || p.state === 'failed';
    seek.max = Math.max(0, Math.ceil(p.duration || 0) - 1);
    const preview = window.broadcastPreview?.position();
    const followsPreview = clip && p.state === 'playing' && preview?.ready && preview.epoch === p.epoch;
    let position = followsPreview ? (preview.timeMS - p.timeline_base_ms) / 1000 : p.position || 0;
    if (followsPreview && p.loop && p.duration > 0) position = (position % p.duration + p.duration) % p.duration;
    position = Math.max(0, Math.min(p.duration || 0, position));
    if (!seeking) seek.value = position;
    el('playback-position').textContent = `${clock(seeking ? Number(seek.value) : position)} / ${clock(p.duration)} · ${seeking ? 'Release to seek' : followsPreview ? 'Preview position' : 'Server position'}`;
    const reviewed = ready(candidate) && previewRevision === candidate.id;
    el('library-replace-now').disabled = !usable || !reviewed || !['PRESTREAM', 'CLIP', 'ENDING'].includes(stage.stage) || stage.ending?.draining || stage.media?.revision === candidate?.id;
    el('library-replace-return').disabled = !usable || !reviewed || !stage.return_media?.revision || ending || stage.return_media.revision === candidate?.id;
    el('selection-save').disabled = !connected || saving || !draft;
    el('selection-cancel').disabled = saving || !draft;
    el('selection-add-shortcut').disabled = !connected || saving;
    for (const field of el('stage-selections-form').querySelectorAll('select,input')) field.disabled = !connected || saving;
    el('selection-save').textContent = saving ? 'Saving…' : 'Save selections';
  }
  function options(select, selectedValue, allowEmpty = true) {
    const values = catalog.revisions.map(item => new Option(`${item.name} · ${item.id.slice(0, 12)} · ${item.state}`, item.id));
    if (selectedValue && !catalog.revisions.some(item => item.id === selectedValue)) values.push(new Option(`Unavailable revision · ${selectedValue}`, selectedValue));
    select.replaceChildren(...(allowEmpty ? [new Option('Not selected', ''), ...values] : values));
    select.value = selectedValue || '';
  }
  function renderSelections() {
    const value = draft || saved;
    options(el('selection-prestream'), value.prestream);
    options(el('selection-ending'), value.ending);
    const rows = (value.shortcuts || []).map((shortcut, index) => {
      const row = document.createElement('div'); row.className = 'shortcut-row';
      const nameLabel = document.createElement('label'); nameLabel.textContent = 'Shortcut name';
      const nameInput = document.createElement('input'); nameInput.type = 'text'; nameInput.maxLength = 80; nameInput.value = shortcut.name; nameInput.required = true;
      const mediaLabel = document.createElement('label'); mediaLabel.textContent = 'Exact media revision';
      const select = document.createElement('select'); options(select, shortcut.revision); select.required = true;
      const remove = document.createElement('button'); remove.type = 'button'; remove.className = 'preview-button'; remove.textContent = 'Remove'; remove.setAttribute('aria-label', `Remove shortcut ${shortcut.name || index + 1}`);
      nameInput.addEventListener('input', () => { draft ||= copy(saved); draft.shortcuts[index].name = nameInput.value; render(); });
      select.addEventListener('change', () => { draft ||= copy(saved); draft.shortcuts[index].revision = select.value; render(); });
      remove.addEventListener('click', () => { draft ||= copy(saved); draft.shortcuts.splice(index, 1); renderSelections(); render(); });
      nameLabel.append(nameInput); mediaLabel.append(select); row.append(nameLabel, mediaLabel, remove); return row;
    });
    el('selection-shortcuts').replaceChildren(...rows);
    const unavailable = [value.prestream, value.ending, ...(value.shortcuts || []).map(item => item.revision)].filter(id => id && !ready(catalog.revisions.find(item => item.id === id)));
    el('selection-warning').textContent = unavailable.length ? 'Some selected revisions are missing, changed, incompatible, or need preparation. Explicitly choose a ready revision; changed files are never substituted automatically.' : 'Selections identify exact ready content. Saving does not start playback or replace media already playing or suspended.';
  }
  function renderRevisions() {
    const file = selected(), select = el('library-revision'), previous = select.value;
    const available = catalog.revisions.filter(item => item.library_id === file?.id);
    select.replaceChildren(...(available.length ? available.map(item => new Option(`${item.name} · ${item.id.slice(0, 12)} · ${item.state}`, item.id)) : [new Option('No prepared revision yet', '')]));
    select.value = available.some(item => item.id === previous) ? previous : file?.revision || available[0]?.id || '';
  }
  async function loadCatalog() {
    if (fetching || !connected || !snapshot?.library_enabled) return;
    fetching = true; lastFetch = Date.now();
    try {
      const responses = await Promise.all(['/api/library', '/api/stage-media'].map(path => fetch(`${location.origin}${path}`, {cache: 'no-store', signal: AbortSignal.timeout(8000)})));
      if (responses.some(response => !response.ok)) throw new Error('Video library or saved selections unavailable. Retrying…');
      const [nextCatalog, nextSelections] = await Promise.all(responses.map(response => response.json()));
      catalog = {...nextCatalog, revisions: nextCatalog.revisions || []}; saved = {...nextSelections, shortcuts: nextSelections.shortcuts || []}; catalogError = '';
      const select = el('library-select'), previous = select.value;
      const values = catalog.files.map(file => new Option(`${file.name} — ${file.state}${file.state === 'preparing' ? ` ${file.progress}%` : ''}`, file.id));
      select.replaceChildren(...(values.length ? values : [new Option('No videos yet', '')]));
      if (catalog.files.some(file => file.id === previous)) select.value = previous;
      renderRevisions();
      // Preserve an operator's edits and focus while background preparation progresses.
      if (!draft && !el('stage-selections-form').contains(document.activeElement)) renderSelections();
      window.broadcastStages?.setSelections(saved, catalog.revisions);
    } catch (e) { catalogError = e.message; }
    finally { fetching = false; render(); }
  }
  function command(action, extra = {}, confirmation = true) {
    if (pending || !connected) return;
    error = ''; seeking = false;
    window.broadcastStages?.request({action, ...extra}, confirmation);
    render();
  }
  el('library-select').addEventListener('change', () => { renderRevisions(); render(); });
  el('library-revision').addEventListener('change', render);
  el('library-once').addEventListener('click', () => command('play_clip', {revision: revision()?.id, loop: false}));
  el('library-loop').addEventListener('click', () => command('play_clip', {revision: revision()?.id, loop: true}));
  el('library-preview').addEventListener('click', () => {
    const candidate = revision(); if (!ready(candidate)) return;
    previewRevision = candidate.id;
    el('candidate-video').hidden = false;
    window.previewCandidate?.(candidate.id); render();
  });
  el('library-replace-now').addEventListener('click', () => command('replace_now', {revision: revision()?.id}));
  el('library-replace-return').addEventListener('click', () => command('replace_on_return', {revision: revision()?.id}));
  el('library-prepare').addEventListener('click', async () => {
    if (pending || !selected()) return;
    pending = true; error = ''; render();
    try {
      const response = await fetch(`${location.origin}/api/library/prepare`, {method: 'PUT', headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: JSON.stringify({id: selected().id}), signal: AbortSignal.timeout(8000)});
      if (!response.ok) throw new Error((await response.text()).trim() || 'Could not queue preparation.');
    } catch (e) { error = e.message; }
    finally { pending = false; await loadCatalog(); render(); }
  });
  for (const action of ['pause', 'resume']) el(`playback-${action}`).addEventListener('click', () => command(action, {}, false));
  el('playback-stop').addEventListener('click', () => command('stop_clip'));
  el('playback-loop').addEventListener('change', () => command('set_loop', {loop: el('playback-loop').checked}, false));
  el('playback-seek').addEventListener('input', () => { seeking = true; render(); });
  el('playback-seek').addEventListener('change', () => command('seek', {position: Number(el('playback-seek').value)}, false));
  el('broadcast-video').addEventListener('timeupdate', render);
  for (const field of ['prestream', 'ending']) el(`selection-${field}`).addEventListener('change', () => { draft ||= copy(saved); draft[field] = el(`selection-${field}`).value; render(); });
  el('selection-add-shortcut').addEventListener('click', () => { draft ||= copy(saved); draft.shortcuts.push({name: '', revision: ''}); renderSelections(); render(); });
  el('selection-cancel').addEventListener('click', () => { draft = null; renderSelections(); render(); });
  el('stage-selections-form').addEventListener('submit', async event => {
    event.preventDefault(); if (!draft || saving || !connected) return;
    saving = true; render();
    try {
      const response = await fetch(`${location.origin}/api/stage-media`, {method: 'PUT', headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: JSON.stringify(draft), signal: AbortSignal.timeout(8000)});
      if (!response.ok) throw new Error((await response.text()).trim() || 'Selections were not saved. Previous selections remain in use.');
      saved = copy(draft); draft = null;
      el('selection-status').textContent = 'Selections saved for subsequent use. Current and suspended playback are unchanged.';
      window.broadcastStages?.setSelections(saved, catalog.revisions);
      await loadCatalog(); renderSelections();
    } catch (e) { el('selection-status').textContent = `${e.message} Previous saved selections remain in use.`; }
    finally { saving = false; render(); }
  });
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
  document.addEventListener('stage-command-state', render);
  window.updateLibrary = (data, online) => {
    snapshot = data; connected = online;
    if (Date.now() - lastFetch > 2500) loadCatalog();
    render();
  };
})();
