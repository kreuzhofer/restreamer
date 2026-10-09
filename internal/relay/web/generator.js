(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  let draft = null;
  let selectedScene = 0;
  let itemEditors = [];
  let validationIssues = [];
  let dirty = false;
  let saving = false;
  let conflict = false;
  let editSequence = 0;
  let previewSequence = 0;
  let saveTimer;
  let previewTimer;
  let previewAbort;
  let jobs = [];
  let jobRequest = false;
  let jobsFetching = false;
  let reviewedRevision = '';
  let stageSelections = {};
  let onAirRevision = '';
  const fields = {
    name: ['design-name', 'name-error'], text: ['scene-text', 'text-error'],
    font: ['scene-font', 'font-error'], font_size: ['scene-size', 'size-error'],
    duration_seconds: ['scene-duration', 'duration-error'], content_region: ['region-width', 'region-error'],
    alignment: ['scene-alignment', 'alignment-error'], items: ['list-content', 'items-error']
  };
  async function request(path, method = 'GET', body, signal) {
    const response = await fetch(new URL(path, location.origin), {method, credentials: 'same-origin', signal,
      headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'},
      body: body === undefined ? undefined : JSON.stringify(body)});
    if (!response.ok) {
      const text = await response.text();
      let message = text;
      try { const data = JSON.parse(text); message = data.issues?.map(issue => `${issue.field}: ${issue.message}`).join('\n') || data.error || text; } catch (_) { /* Plain server error. */ }
      const error = new Error(message || `Request failed (${response.status})`);
      error.status = response.status;
      throw error;
    }
    return response;
  }
  const notify = message => { $('generator-notice').textContent = message; $('generator-notice').hidden = !message; };
  function saveState(message, error = '') {
    $('save-state').textContent = message;
    $('save-error').textContent = error;
    $('save-error').hidden = !error;
    $('save-actions').hidden = !error;
    $('save-retry').hidden = conflict;
    renderJobs();
  }
  function canLeave() {
    if (saving) { notify('A save is in progress. Wait for its acknowledgement before opening another draft.'); return false; }
    return !dirty || window.confirm('Discard your unsaved local edits and open another design?');
  }
  async function listDesigns() {
    const designs = await (await request('/api/generator/designs')).json();
    $('design-list').replaceChildren();
    for (const design of designs) {
      const button = document.createElement('button');
      button.type = 'button';
      button.textContent = design.name || 'Untitled design';
      button.setAttribute('aria-current', String(design.id === draft?.id));
      const detail = document.createElement('small');
      detail.textContent = `${design.stage} · draft v${design.version}`;
      button.append(detail);
      button.addEventListener('click', async () => {
        if (!canLeave()) return;
        const sequence = editSequence;
        try {
          const loaded = await (await request(`/api/generator/designs/${design.id}`)).json();
          if (sequence !== editSequence || saving) { notify('Your current edits were retained. Open the saved design again when ready.'); return; }
          openDraft(loaded);
        }
        catch (error) { notify(error.message); }
      });
      $('design-list').append(button);
    }
  }
  function openDraft(value) {
    clearTimeout(saveTimer);
    draft = value; dirty = false; conflict = false; editSequence++;
    selectedScene = 0; validationIssues = [];
    $('design-name').value = draft.name;
    renderScenes(); renderSelectedScene();
    $('design-stage').textContent = `${draft.stage.toUpperCase()} · EDITABLE DRAFT`;
    $('design-editor').hidden = false; $('generator-empty').hidden = true;
    notify(''); saveState(`Saved · version ${draft.version}`);
    history.replaceState(null, '', `/generator?id=${encodeURIComponent(draft.id)}`);
    listDesigns().catch(error => notify(error.message));
    schedulePreview();
    loadJobs();
  }
  function renderScenes() {
    $('scene-list').replaceChildren();
    for (const [index, scene] of draft.scenes.entries()) {
      const button = document.createElement('button'); button.type = 'button';
      const invalid = validationIssues.some(issue => issue.field.startsWith(`scenes.${index}.`));
      button.textContent = `${index + 1}. ${scene.text.split('\n')[0] || (scene.layout === 'list' ? 'List' : 'Untitled')} · ${scene.duration_seconds}s${invalid ? ' · needs attention' : ''}`;
      button.setAttribute('aria-current', String(index === selectedScene));
      button.addEventListener('click', () => selectScene(index)); $('scene-list').append(button);
    }
    $('add-title-scene').disabled = $('add-list-scene').disabled = draft.scenes.length >= 20;
    $('duplicate-scene').disabled = draft.scenes.length >= 20;
    $('move-scene-up').disabled = selectedScene === 0;
    $('move-scene-down').disabled = selectedScene >= draft.scenes.length - 1;
  }
  function renderSelectedScene() {
    const scene = draft.scenes[selectedScene]; itemEditors = [];
    $('selected-scene-fields').hidden = !scene;
    $('editor-heading').textContent = `${draft.scenes.length} scene${draft.scenes.length === 1 ? '' : 's'}`;
    if (!scene) return;
    $('selected-scene-heading').textContent = `Scene ${selectedScene + 1} · ${scene.layout}`;
    $('scene-layout').value = scene.layout;
    $('scene-text').value = scene.text;
    $('scene-font').value = scene.font || '';
    $('scene-size').value = scene.font_size || '';
    $('scene-duration').value = scene.duration_seconds;
    $('scene-alignment').value = scene.alignment || '';
    $('region-width').value = scene.content_region?.width_percent || '';
    $('region-height').value = scene.content_region?.height_percent || '';
    $('list-content').hidden = scene.layout !== 'list';
    $('list-items').replaceChildren();
    for (const [index, text] of (scene.items || []).entries()) {
      const row = document.createElement('div'); row.className = 'generator-list-item';
      const label = document.createElement('label'); label.textContent = `List item ${index + 1}`;
      const textarea = document.createElement('textarea'); textarea.rows = 2; textarea.maxLength = 4096; textarea.value = text;
      const error = document.createElement('small'); error.className = 'field-error'; error.id = `item-error-${index}`;
      textarea.setAttribute('aria-describedby', error.id); label.append(textarea); row.append(label, error);
      const remove = document.createElement('button'); remove.type = 'button'; remove.className = 'preview-button'; remove.textContent = `Remove item ${index + 1}`;
      remove.addEventListener('click', () => { scene.items.splice(index, 1); renderSelectedScene(); changed(); }); row.append(remove);
      $('list-items').append(row); itemEditors.push({input: textarea, error});
    }
    $('add-list-item').disabled = (scene.items || []).length >= 20;
  }
  function selectScene(index) { selectedScene = index; renderScenes(); renderSelectedScene(); schedulePreview(); }
  function changed() {
    dirty = true; editSequence++;
    if (!conflict) { saveState('Unsaved local changes'); clearTimeout(saveTimer); saveTimer = setTimeout(() => saveDraft(), 700); }
    renderScenes(); schedulePreview();
  }
  function captureFields() {
    draft.name = $('design-name').value;
    const scene = draft.scenes[selectedScene]; if (!scene) return;
    scene.layout = $('scene-layout').value;
    scene.text = $('scene-text').value; scene.font = $('scene-font').value;
    scene.font_size = Number($('scene-size').value); scene.duration_seconds = Number($('scene-duration').value);
    scene.alignment = $('scene-alignment').value;
    scene.content_region = {width_percent: Number($('region-width').value), height_percent: Number($('region-height').value)};
    if (scene.layout === 'list' || scene.items) scene.items = itemEditors.map(editor => editor.input.value);
  }
  function addScene(layout) {
    if (!draft || draft.scenes.length >= 20) return;
    const scene = {id: crypto.randomUUID(), layout, text: '', duration_seconds: 10};
    if (layout === 'list') scene.items = ['New list item'];
    draft.scenes.push(scene); selectedScene = draft.scenes.length - 1;
    renderSelectedScene(); changed();
  }
  $('add-title-scene').addEventListener('click', () => addScene('title'));
  $('add-list-scene').addEventListener('click', () => addScene('list'));
  $('duplicate-scene').addEventListener('click', () => {
    if (draft.scenes.length >= 20) return;
    const copy = structuredClone(draft.scenes[selectedScene]); copy.id = crypto.randomUUID();
    draft.scenes.splice(selectedScene + 1, 0, copy); selectedScene++; renderSelectedScene(); changed();
  });
  $('remove-scene').addEventListener('click', () => {
    draft.scenes.splice(selectedScene, 1); selectedScene = Math.max(0, Math.min(selectedScene, draft.scenes.length - 1)); renderSelectedScene(); changed();
  });
  function moveScene(delta) {
    const destination = selectedScene + delta;
    if (destination < 0 || destination >= draft.scenes.length) return;
    [draft.scenes[selectedScene], draft.scenes[destination]] = [draft.scenes[destination], draft.scenes[selectedScene]];
    selectedScene = destination; renderSelectedScene(); changed();
  }
  $('move-scene-up').addEventListener('click', () => moveScene(-1));
  $('move-scene-down').addEventListener('click', () => moveScene(1));
  $('add-list-item').addEventListener('click', () => {
    const scene = draft.scenes[selectedScene]; scene.items ||= [];
    if (scene.items.length >= 20) return;
    scene.items.push('New list item'); renderSelectedScene(); changed();
  });
  function schedulePreview() {
    clearTimeout(previewTimer);
    previewSequence++;
    previewAbort?.abort();
    $('preview-state').textContent = 'Validating the current scene…';
    $('scene-preview').hidden = true;
    previewTimer = setTimeout(updatePreview, 450);
  }
  async function updatePreview() {
    const sequence = previewSequence;
    const snapshot = structuredClone(draft);
    previewAbort = new AbortController();
    try {
      const result = await (await request('/api/generator/validate', 'POST', snapshot, previewAbort.signal)).json();
      if (sequence !== previewSequence) return;
      validationIssues = result.issues;
      renderScenes();
      $('sequence-duration').textContent = `${draft.scenes.length} scene${draft.scenes.length === 1 ? '' : 's'} · ${result.duration_seconds.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')} seconds with cuts at ${result.profile.fps} fps`;
      for (const [input, output] of Object.values(fields)) { $(input).removeAttribute('aria-invalid'); $(output).textContent = ''; }
      for (const editor of itemEditors) { editor.input.removeAttribute('aria-invalid'); editor.error.textContent = ''; }
      $('scene-issues').replaceChildren();
      for (const issue of result.issues) {
        const match = /^scenes\.(\d+)\.(.+)$/.exec(issue.field);
        const sceneIndex = match ? Number(match[1]) : null;
        const fieldName = match ? match[2] : issue.field;
        if (sceneIndex === selectedScene || sceneIndex === null) {
          if (fieldName.startsWith('items.')) {
            const editor = itemEditors[Number(fieldName.split('.')[1])];
            if (editor) { editor.input.setAttribute('aria-invalid', 'true'); editor.error.textContent = issue.message; }
          } else {
            const field = fields[fieldName];
            if (field) { $(field[0]).setAttribute('aria-invalid', 'true'); $(field[1]).textContent = issue.message; }
          }
        }
        const entry = document.createElement(sceneIndex === null ? 'p' : 'button');
        entry.textContent = `${sceneIndex === null ? 'Composition' : `Scene ${sceneIndex + 1}`} · ${issue.message}`;
        if (sceneIndex !== null) { entry.type = 'button'; entry.addEventListener('click', () => selectScene(sceneIndex)); }
        $('scene-issues').append(entry);
      }
      $('preview-profile').textContent = `${result.profile.width} × ${result.profile.height} · ${result.profile.fps} fps`;
      const selectedInvalid = result.issues.some(issue => !issue.field.startsWith('scenes.') || issue.field.startsWith(`scenes.${selectedScene}.`));
      if (!snapshot.scenes[selectedScene] || selectedInvalid) {
        $('preview-state').textContent = 'Resolve this scene’s validation issues to preview. Draft edits are still saved.';
        return;
      }
      const response = await request(`/api/generator/preview?scene=${selectedScene}`, 'POST', snapshot, previewAbort.signal);
      const bitmap = await createImageBitmap(await response.blob());
      if (sequence !== previewSequence) { bitmap.close(); return; }
      const canvas = $('scene-preview');
      canvas.width = bitmap.width; canvas.height = bitmap.height;
      canvas.getContext('2d').drawImage(bitmap, 0, 0); bitmap.close();
      canvas.hidden = false;
      canvas.setAttribute('aria-label', `Quick preview: ${snapshot.scenes[selectedScene].text || 'Blank title scene'}`);
      $('preview-state').textContent = `Current draft preview · ${snapshot.scenes[selectedScene].duration_seconds} seconds · not on air`;
    } catch (error) {
      if (error.name !== 'AbortError' && sequence === previewSequence) $('preview-state').textContent = `Preview unavailable: ${error.message}`;
    }
  }
  async function saveDraft(asCopy = false) {
    clearTimeout(saveTimer);
    if (!draft || saving || (conflict && !asCopy) || (!dirty && !asCopy)) return;
    saving = true;
    const sequence = editSequence;
    const snapshot = structuredClone(draft);
    saveState(asCopy ? 'Saving independent copy…' : 'Saving…');
    try {
      const path = asCopy ? '/api/generator/designs' : `/api/generator/designs/${snapshot.id}`;
      const saved = await (await request(path, asCopy ? 'POST' : 'PUT', snapshot)).json();
      // Keep edits made while this request was in flight, but advance the CAS version.
      draft.id = saved.id; draft.version = saved.version; draft.updated_at = saved.updated_at;
      conflict = false; dirty = editSequence !== sequence;
      saveState(dirty ? 'Unsaved local changes' : `Saved · version ${draft.version}`);
      history.replaceState(null, '', `/generator?id=${encodeURIComponent(draft.id)}`);
      listDesigns().catch(error => notify(error.message));
    } catch (error) {
      conflict = error.status === 409;
      saveState(conflict ? 'Save conflict · local edits retained' : 'Not saved · local edits retained', error.message);
    } finally {
      saving = false;
      renderJobs();
      if (dirty && !conflict && editSequence !== sequence) saveTimer = setTimeout(() => saveDraft(), 700);
    }
  }
  $('scene-form').addEventListener('submit', event => event.preventDefault());
  $('scene-form').addEventListener('input', event => {
    captureFields();
    if (event.target.id === 'scene-layout') renderSelectedScene();
    changed();
  });
  $('save-retry').addEventListener('click', () => saveDraft());
  $('save-copy').addEventListener('click', () => saveDraft(true));
  $('save-reload').addEventListener('click', async () => {
    if (!canLeave()) return;
    const sequence = editSequence;
    try {
      const loaded = await (await request(`/api/generator/designs/${draft.id}`)).json();
      if (sequence !== editSequence || saving) { notify('Your current edits were retained. Reload again when ready.'); return; }
      openDraft(loaded);
    }
    catch (error) { saveState('Reload failed · local edits retained', error.message); }
  });
  $('new-design').addEventListener('submit', async event => {
    event.preventDefault(); if (!canLeave()) return;
    const button = event.submitter; button.disabled = true;
    const sequence = editSequence;
    try {
      const created = await (await request('/api/generator/designs', 'POST', {name: $('new-name').value, stage: $('new-stage').value})).json();
      if (sequence !== editSequence || saving) { notify('New blank design saved. Your current edits were retained; open the new design from the list.'); await listDesigns(); return; }
      openDraft(created);
    } catch (error) { notify(error.message); }
    finally { button.disabled = false; }
  });

  function renderJobs() {
    if (!draft) return;
    const busy = jobs.some(job => ['queued', 'running', 'cancelling'].includes(job.state));
    $('generate-design').disabled = dirty || saving || conflict || jobRequest || busy;
    $('generation-save-hint').textContent = dirty || saving || conflict
      ? 'Save and resolve conflicts before generating. Your local edits are retained.'
      : `Generate saved draft v${draft.version}. Later edits leave the captured revision unchanged.`;
    const active = jobs.find(job => ['queued', 'running', 'cancelling'].includes(job.state));
    $('generation-status').textContent = active ? `${active.design_snapshot.name}: ${active.state} · ${active.progress}%` : 'Renderer available · one job at a time';
    const relevant = jobs.filter(job => job.design_snapshot.id === draft.id);
    $('generation-jobs').replaceChildren();
    for (const job of relevant) {
      const row = document.createElement('article'); row.className = 'generator-job'; row.dataset.jobId = job.id;
      const title = document.createElement('strong'); title.textContent = `${job.state} · captured draft v${job.design_snapshot.version} · ${job.duration.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')} seconds`;
      const identity = document.createElement('p'); identity.textContent = `Design revision ${job.design_revision.slice(0, 12)} · ${job.profile.width} × ${job.profile.height} · ${job.profile.fps} fps`;
      row.append(title, identity);
      if (job.error) { const error = document.createElement('p'); error.textContent = job.error; row.append(error); }
      if (['queued', 'running', 'cancelling'].includes(job.state)) {
        const progress = document.createElement('progress'); progress.max = 100; progress.value = job.progress; progress.setAttribute('aria-label', 'Generation progress'); row.append(progress);
        const cancel = document.createElement('button'); cancel.type = 'button'; cancel.className = 'preview-button'; cancel.textContent = job.state === 'cancelling' ? 'Cancelling…' : 'Cancel generation'; cancel.disabled = job.state === 'cancelling';
        cancel.addEventListener('click', async () => { cancel.disabled = true; try { await request(`/api/generator/jobs/${job.id}/cancel`, 'POST', {}); await loadJobs(); } catch (error) { generationError(error.message); cancel.disabled = false; } }); row.append(cancel);
      }
      if (job.state === 'ready') {
        const detail = document.createElement('p');
        const selected = Object.values(stageSelections).some(value => value === job.media_revision);
        detail.textContent = `Media ${job.media_revision.slice(0, 12)} · ${selected ? 'selected for a stage' : 'not selected for a stage'} · ${onAirRevision === job.media_revision ? 'on air' : 'not on air'}${job.design_snapshot.version !== draft.version ? ' · newer editable draft exists' : ''}`;
        const preview = document.createElement('button'); preview.type = 'button'; preview.className = 'preview-button'; preview.textContent = 'Preview exact revision'; preview.addEventListener('click', () => previewGenerated(job)); row.append(detail, preview);
      }
      $('generation-jobs').append(row);
    }
  }
  function generationError(message) { $('generation-error').textContent = message; $('generation-error').hidden = !message; }
  async function loadJobs() {
    if (jobsFetching) return;
    jobsFetching = true;
    try {
      const [nextJobs, selections, stage] = await Promise.all(['/api/generator/jobs', '/api/stage-media', '/api/stage'].map(async path => (await request(path)).json()));
      jobs = nextJobs; stageSelections = selections; onAirRevision = stage.media?.revision || '';
      if ($('generation-error').textContent.startsWith('Generation status unavailable:')) generationError('');
      renderJobs();
    } catch (error) { generationError(`Generation status unavailable: ${error.message}`); }
    finally { jobsFetching = false; }
  }
  function previewGenerated(job) {
    reviewedRevision = job.media_revision;
    $('generated-preview').hidden = false;
    $('generated-identity').textContent = `Captured draft v${job.design_snapshot.version} · media revision ${reviewedRevision}. Preview does not change the broadcast.`;
    const video = $('generated-video');
    video.src = `/api/library/revisions/${encodeURIComponent(reviewedRevision)}/preview`;
    $('generated-playback-status').textContent = 'Loading exact prepared output…';
    video.play().catch(() => { $('generated-playback-status').textContent = 'Press Play to review the exact prepared output.'; });
  }
  $('generated-video').addEventListener('playing', () => { $('generated-playback-status').textContent = `Playing exact revision ${reviewedRevision.slice(0, 12)}.`; });
  $('generated-video').addEventListener('error', () => { $('generated-playback-status').textContent = 'Exact output preview unavailable. Check the active profile and try Preview exact revision again.'; });
  $('generate-design').addEventListener('click', async () => {
    if (!draft || dirty || saving || conflict || jobRequest) return;
    jobRequest = true; generationError(''); renderJobs();
    try { await request('/api/generator/jobs', 'POST', {design_id: draft.id, version: draft.version}); await loadJobs(); }
    catch (error) { generationError(error.message); }
    finally { jobRequest = false; renderJobs(); }
  });
  setInterval(loadJobs, 2000);

  window.addEventListener('beforeunload', event => { if (dirty || saving) { event.preventDefault(); event.returnValue = ''; } });
  (async () => {
    try {
      await listDesigns();
      const id = new URLSearchParams(location.search).get('id');
      if (id) openDraft(await (await request(`/api/generator/designs/${encodeURIComponent(id)}`)).json());
    } catch (error) { notify(error.message); }
  })();
})();
