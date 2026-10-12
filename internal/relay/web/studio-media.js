'use strict';
(() => {
  const el = id => document.getElementById(id);
  const panel = el('studio-media');
  if (!panel) return;
  let catalog, stage, fetching = false, upload, mutation = false, online = false, rendered = '';
  const desktop = window.matchMedia('(min-width: 601px)');
  const error = message => { el('media-library-error').textContent = message; el('media-library-error').hidden = !message; };
  const status = message => { el('media-library-status').textContent = message; };
  async function request(path, method = 'GET', body) {
    const response = await fetch(new URL(path, location.origin), {method, cache: 'no-store', headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, ...(body ? {body: JSON.stringify(body)} : {}), signal: AbortSignal.timeout(10000)});
    if (!response.ok) throw new Error((await response.text()).trim() || 'Media request failed.');
    return response;
  }
  function uses(file) {
    const revisions = (catalog.revisions || []).filter(item => item.library_id === file.id || item.id === file.revision);
    const refs = [];
    for (const revision of revisions) {
      const label = revision.id.slice(0, 12);
      for (const name of ['prestream', 'ending']) if (catalog.selections?.[name] === revision.id) refs.push(`${name.toUpperCase()} selection · ${label}`);
      for (const shortcut of catalog.selections?.shortcuts || []) if (shortcut.revision === revision.id) refs.push(`Clip shortcut “${shortcut.name}” · ${label}`);
      if (stage?.media?.revision === revision.id) refs.push(`Current ${stage.stage} media · ${label}`);
      if (stage?.return_media?.revision === revision.id) refs.push(`Return to ${stage.return_stage} · ${label}`);
      for (const other of catalog.files) if (other.id !== file.id && other.revision === revision.id) refs.push(`Shared with “${other.name}” · ${label}`);
    }
    return {revisions, refs};
  }
  function controls() {
    el('media-library-upload-button').disabled = !online || !!upload || !desktop.matches;
    el('media-library-file').disabled = !!upload || !desktop.matches;
    el('media-library-upload-cancel').hidden = !upload;
    panel.querySelectorAll('[data-media-mutation]').forEach(button => { button.disabled = !online || mutation || !desktop.matches || button.dataset.blocked === 'true'; });
  }
  function render() {
    controls();
    if (!catalog) return;
    const signature = JSON.stringify([catalog, stage?.media, stage?.return_media, stage?.stage, stage?.return_stage]);
    if (signature === rendered) return;
    const focusedRow = document.activeElement?.closest('.media-library-item');
    const focusedID = focusedRow?.dataset.id, focusedText = document.activeElement?.textContent;
    rendered = signature;
    const list = el('media-library-list');
    const opened = new Set([...list.querySelectorAll('details[open]')].map(row => row.dataset.id));
    list.replaceChildren();
    el('media-library-empty').hidden = catalog.files.length > 0;
    el('media-library-count').textContent = `${catalog.files.length} uploaded original${catalog.files.length === 1 ? '' : 's'}`;
    for (const file of catalog.files) {
      const {revisions, refs} = uses(file);
      const row = document.createElement('details'); row.className = 'generator-asset media-library-item'; row.dataset.id = file.id; row.open = opened.has(file.id);
      const heading = document.createElement('summary'); heading.textContent = `${file.name} · ${file.state === 'preparing' ? `Preparing ${file.progress}%` : file.state}`;
      const info = document.createElement('p'); info.className = 'stage-help'; info.textContent = `${(file.bytes / 1048576).toFixed(1)} MiB · ${file.duration.toFixed(1)} seconds${file.message ? ` · ${file.message}` : ''}`;
      const failure = document.createElement('p'); failure.className = 'notice'; failure.textContent = file.error || ''; failure.hidden = !file.error;
      const progress = document.createElement('progress'); progress.max = 100; progress.value = file.progress; progress.hidden = file.state !== 'preparing'; progress.setAttribute('aria-label', `Preparation of ${file.name}`);
      const reference = document.createElement('p'); reference.className = 'stage-help'; reference.textContent = refs.length ? `Protected: ${refs.join('; ')}` : 'No current selection or playback references. The server checks all protections again before deletion.';
      const actions = document.createElement('div'); actions.className = 'generator-actions';
      const prepare = document.createElement('button'); prepare.type = 'button'; prepare.className = 'preview-button studio-authoring'; prepare.textContent = file.state === 'failed' ? 'Retry preparation' : 'Prepare again'; prepare.dataset.mediaMutation = ''; prepare.dataset.blocked = String(['preparing', 'discovering', 'queued'].includes(file.state));
      prepare.addEventListener('click', () => mutate('/api/library/prepare', 'PUT', {id: file.id}, 'Preparation queued. Existing ready revisions remain available.'));
      const remove = document.createElement('button'); remove.type = 'button'; remove.className = 'preview-button studio-authoring'; remove.textContent = 'Delete uploaded video'; remove.dataset.mediaMutation = ''; remove.dataset.blocked = String(refs.length > 0 || file.state === 'preparing');
      remove.addEventListener('click', () => {
        if (!window.confirm(`Permanently delete uploaded video “${file.name}”, its original file and eligible prepared revisions? Selected, playing, return and shared media are protected. Designs and their generated results stay.`)) return;
        mutate(`/api/library/files/${encodeURIComponent(file.id)}`, 'DELETE', {version: file.version}, `${file.name} and its eligible files deleted.`);
      });
      actions.append(prepare, remove);
      const ready = document.createElement('div'); ready.className = 'media-ready-revisions';
      if (!revisions.length) { const waiting = document.createElement('p'); waiting.className = 'stage-help'; waiting.textContent = 'No prepared revision yet.'; ready.append(waiting); }
      for (const revision of revisions) {
        const item = document.createElement('div'); item.className = 'media-revision-row';
        const label = document.createElement('span'); label.textContent = `Revision ${revision.id.slice(0, 12)} · ${revision.state}${revision.error ? ` · ${revision.error}` : ''}`; label.title = revision.id;
        const preview = document.createElement('button'); preview.type = 'button'; preview.className = 'preview-button'; preview.textContent = 'Preview exact revision'; preview.disabled = revision.state !== 'ready';
        preview.addEventListener('click', () => {
          const video = el('media-library-preview'); video.src = `/api/library/revisions/${encodeURIComponent(revision.id)}/preview`; video.hidden = false;
          el('media-library-preview-label').textContent = `${file.name} · exact revision ${revision.id}`; video.focus();
        });
        item.append(label, preview);
        for (const target of ['prestream', 'ending']) {
          const link = document.createElement('a'); link.className = 'preview-button'; link.textContent = `Select for ${target}`;
          link.href = `/live?${new URLSearchParams({media_revision: revision.id, media_stage: target, media_action: 'select_next'})}`;
          if (revision.state === 'ready') item.append(link);
        }
        ready.append(item);
      }
      row.append(heading, info, failure, progress, reference, ready, actions); list.append(row);
    }
    controls();
    if (focusedID) {
      const row = [...list.children].find(item => item.dataset.id === focusedID);
      const nextFocus = row && [...row.querySelectorAll('summary, button, a')].find(item => item.textContent === focusedText);
      (nextFocus || row?.querySelector('summary') || el('media-library-refresh')).focus({preventScroll: true});
    }
  }
  async function load() {
    if (fetching) return;
    fetching = true;
    try {
      const responses = await Promise.all(['/api/library', '/api/stage'].map(path => request(path)));
      [catalog, stage] = await Promise.all(responses.map(response => response.json())); online = true;
      el('media-library-load-error').hidden = !catalog.error; el('media-library-load-error').textContent = catalog.error || '';
      el('media-library-loading').hidden = true;
    } catch (e) {
      online = false; el('media-library-load-error').textContent = `Media status unavailable. Last loaded data may be stale. ${e.message}`; el('media-library-load-error').hidden = false; el('media-library-loading').hidden = true;
    } finally { fetching = false; render(); }
  }
  async function mutate(path, method, body, message) {
    if (mutation || !desktop.matches || !online) return;
    mutation = true; error(''); controls();
    try {
      await request(path, method, body); status(message);
      if (method === 'DELETE') {
        const video = el('media-library-preview'); video.pause(); video.removeAttribute('src'); video.load(); video.hidden = true;
        el('media-library-preview-label').textContent = '';
      }
    }
    catch (e) { error(e.message); }
    finally { mutation = false; await load(); controls(); }
  }
  el('media-library-upload').addEventListener('submit', event => {
    event.preventDefault();
    const file = el('media-library-file').files[0];
    if (!file || upload || !desktop.matches || !online) return;
    if (file.size > catalog.upload_limit) { error(`Choose an MP4 up to ${(catalog.upload_limit / 1048576).toFixed(0)} MiB.`); return; }
    const form = new FormData(); form.append('file', file);
    const xhr = new XMLHttpRequest(); upload = xhr; error('');
    xhr.open('POST', new URL('/api/library/upload', location.origin)); xhr.setRequestHeader('X-Restreamer-Control', '1'); xhr.timeout = 7200000;
    xhr.upload.onprogress = event => { status(event.lengthComputable ? `Uploading video · ${Math.floor(event.loaded / event.total * 100)}%` : 'Uploading video…'); };
    xhr.upload.onload = () => status('Upload transferred. Waiting for the server to save it…');
    xhr.onload = () => {
      if (xhr.status === 202) { status('Upload saved. Preparation starts automatically; progress appears below.'); el('media-library-file').value = ''; }
      else error(xhr.responseText.trim() || 'Upload failed.');
    };
    xhr.onerror = () => error('Upload connection failed. Refresh Media before retrying; the server may have saved the upload.');
    xhr.ontimeout = () => error('Upload timed out. Refresh Media before retrying.');
    xhr.onabort = () => status('Upload cancelled. Refresh Media to check whether it had already been saved.');
    xhr.onloadend = () => { upload = undefined; load(); controls(); };
    status('Uploading video…'); xhr.send(form); controls();
  });
  el('media-library-upload-cancel').addEventListener('click', () => upload?.abort());
  el('media-library-refresh').addEventListener('click', load);
  el('media-library-preview').addEventListener('error', () => error('This exact revision cannot be previewed. Refresh Media to check its readiness.'));
  document.querySelectorAll('[data-media-view]').forEach(button => button.addEventListener('click', () => {
    document.querySelectorAll('[data-media-view]').forEach(item => item.setAttribute('aria-pressed', String(item === button)));
    document.querySelectorAll('[data-media-panel]').forEach(item => { item.hidden = item.dataset.mediaPanel !== button.dataset.mediaView; });
    if (button.dataset.mediaView === 'library') load();
  }));
  desktop.addEventListener('change', controls);
  load();
  setInterval(() => { if (!panel.hidden) load(); }, 3000);
})();
