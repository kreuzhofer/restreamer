(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  let draft = null;
  let templates = [];
  let themes = [];
  let editingTheme = null;
  let themeDirty = false;
  let themeSaving = false;
  let templateSaving = false;
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
  let stageStatus = {}, mediaRevisions = [];
  let assets = [];
  let assetsFetching = false;
  let assetUploading = false;
  const fields = {
    ending_fade_seconds: ['ending-fade', 'ending-fade-error'],
    'soundtrack.asset': ['soundtrack-asset', 'soundtrack-asset-error'],
    'soundtrack.mode': ['soundtrack-mode', 'soundtrack-mode-error'],
    'soundtrack.volume_percent': ['soundtrack-volume', 'soundtrack-volume-error'],
    'soundtrack.fade_in_seconds': ['soundtrack-fade-in', 'soundtrack-fade-in-error'],
    'soundtrack.fade_out_seconds': ['soundtrack-fade-out', 'soundtrack-fade-out-error'],
    'loop_transition.kind': ['loop-transition', 'loop-kind-error'], 'loop_transition.duration_seconds': ['loop-duration', 'loop-duration-error'],
    name: ['design-name', 'name-error'], text: ['scene-text', 'text-error'],
    font: ['scene-font', 'font-error'], font_size: ['scene-size', 'size-error'],
    'transition.kind': ['scene-transition', 'transition-kind-error'], 'transition.duration_seconds': ['transition-duration', 'transition-duration-error'],
    duration_seconds: ['scene-duration', 'duration-error'], content_region: ['region-width', 'region-error'],
    video: ['scene-video', 'video-error'], 'video.trim': ['video-trim-start', 'video-trim-error'], 'video.audio_enabled': ['video-audio', 'video-audio-error'], 'video.audio_volume_percent': ['video-volume', 'video-volume-error'], alignment: ['scene-alignment', 'alignment-error'], image: ['scene-image', 'image-error'], items: ['list-content', 'items-error']
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
    $('loop-controls').hidden = draft.stage !== 'prestream';
    $('loop-transition').value = draft.loop_transition?.kind || 'cut';
    $('loop-duration').value = draft.loop_transition?.duration_seconds || 0.5;
    $('loop-duration-controls').hidden = $('loop-transition').value !== 'crossfade';
    renderScenes(); renderSelectedScene(); renderMusicPicker(); renderDesignTheme();
    $('ending-finish').hidden = draft.stage !== 'ending';
    $('ending-fade').value = draft.ending_fade_seconds ?? 1;
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
    $('scene-media-kind').value = scene.media_kind || 'image';
    $('media-kind-controls').hidden = scene.layout !== 'media';
    renderVideoPicker();
    $('scene-text').value = scene.text;
    $('scene-font').value = scene.font || '';
    $('scene-size').value = scene.font_size || '';
    $('scene-duration').value = scene.duration_seconds;
    $('scene-transition').value = scene.transition?.kind || 'cut';
    $('transition-duration').value = scene.transition?.duration_seconds || 0.5;
    renderTransitionControls();
    $('scene-alignment').value = scene.alignment || '';
    $('region-width').value = scene.content_region?.width_percent || '';
    $('region-height').value = scene.content_region?.height_percent || '';
    $('list-content').hidden = scene.layout !== 'list';
    document.querySelectorAll('[data-scene-text-controls]').forEach(control => { control.hidden = scene.layout === 'media'; });
    renderImagePicker();
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
  function renderTransitionControls() {
    const last = selectedScene === draft.scenes.length - 1;
    $('transition-duration-controls').hidden = $('scene-transition').value !== 'crossfade';
    $('transition-help').textContent = last ? 'This is the last scene. Its outgoing transition is saved for reordering but is not applied here.' : 'Crossfades overlap the end of this scene with the beginning of the next, including enabled video audio. Overlaps shorten the total duration. Generate to inspect the exact transition; the quick preview shows one scene.';
  }
  function selectScene(index) { selectedScene = index; renderScenes(); renderSelectedScene(); schedulePreview(); }
  function changed() {
    dirty = true; editSequence++;
    if (!conflict) { saveState('Unsaved local changes'); clearTimeout(saveTimer); saveTimer = setTimeout(() => saveDraft(), 700); }
    renderScenes(); schedulePreview();
  }
  function captureFields() {
    draft.name = $('design-name').value;
    if (draft.stage === 'prestream') {
      draft.loop_transition = $('loop-transition').value === 'crossfade' ? {kind: 'crossfade', duration_seconds: Number($('loop-duration').value)} : {kind: 'cut'};
      $('loop-duration-controls').hidden = $('loop-transition').value !== 'crossfade';
    }
    if (draft.stage === 'ending') draft.ending_fade_seconds = Number($('ending-fade').value);
    const musicValue = $('soundtrack-asset').value;
    if (musicValue) {
      const [id, revision] = musicValue.split(':');
      draft.soundtrack = {asset: {id, revision: Number(revision)}, mode: $('soundtrack-mode').value, volume_percent: Number($('soundtrack-volume').value), fade_in_seconds: Number($('soundtrack-fade-in').value), fade_out_seconds: Number($('soundtrack-fade-out').value)};
    } else delete draft.soundtrack;
    const scene = draft.scenes[selectedScene]; if (!scene) return;
    scene.layout = $('scene-layout').value;
    scene.media_kind = $('scene-media-kind').value;
    if (scene.video) { scene.video.trim_start_seconds = Number($('video-trim-start').value); scene.video.trim_end_seconds = Number($('video-trim-end').value); scene.video.repeat = $('video-repeat').checked; scene.video.audio_enabled = $('video-audio').checked; scene.video.audio_volume_percent = Number($('video-volume').value); }
    const imageValue = $('scene-image').value;
    if (imageValue) { const [id, revision] = imageValue.split(':'); scene.image = {id, revision: Number(revision)}; } else delete scene.image;
    scene.text = $('scene-text').value; scene.font = $('scene-font').value;
    scene.font_size = Number($('scene-size').value); scene.duration_seconds = Number($('scene-duration').value);
    scene.transition = $('scene-transition').value === 'crossfade' ? {kind: 'crossfade', duration_seconds: Number($('transition-duration').value)} : {kind: 'cut'};
    renderTransitionControls();
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
    stopSourcePreview();
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
      $('sequence-duration').textContent = `${draft.scenes.length} scene${draft.scenes.length === 1 ? '' : 's'} · ${result.duration_seconds.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')} seconds ${draft.stage === 'prestream' ? 'per complete cycle' : 'total'} after transition overlaps at ${result.profile.fps} fps`;
      if (draft.stage === 'ending') {
        const fade = Math.round((draft.ending_fade_seconds ?? 1) * result.profile.fps) / result.profile.fps;
        $('ending-fade-effective').textContent = fade === 0 ? 'Final fade disabled: the final image and audio are preserved.' : `Final ${fade.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')} seconds fade to black and silence after rounding to video frames, within the total above.`;
      }
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
      const response = await request(`/api/generator/preview?scene=${selectedScene}&frame=${Math.round(Math.min(600, Math.max(0, Number($('preview-time').value) || 0)) * result.profile.fps)}`, 'POST', snapshot, previewAbort.signal);
      const bitmap = await createImageBitmap(await response.blob());
      if (sequence !== previewSequence) { bitmap.close(); return; }
      const canvas = $('scene-preview');
      canvas.width = bitmap.width; canvas.height = bitmap.height;
      canvas.getContext('2d').drawImage(bitmap, 0, 0); bitmap.close();
      canvas.hidden = false;
      canvas.setAttribute('aria-label', `Quick preview: ${snapshot.scenes[selectedScene].text || 'Blank title scene'}`);
      $('preview-state').textContent = `Current draft preview · ${snapshot.scenes[selectedScene].duration_seconds} seconds · ${snapshot.scenes[selectedScene].transition?.kind === 'crossfade' && selectedScene + 1 < snapshot.scenes.length ? 'crossfade shown in generated preview' : 'individual scene'} · not on air`;
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
      loadAssets();
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
    if (event.target.id === 'design-theme') return;
    captureFields();
    if (event.target.id === 'scene-layout' || event.target.id === 'scene-media-kind') renderSelectedScene();
    if (event.target.id === 'scene-image') renderImagePicker();
    if (event.target.id === 'soundtrack-asset' || event.target.id === 'soundtrack-mode') renderMusicPicker();
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
      const template = templates.find(item => item.id === $('new-template').value);
      const [themeID, themeRevision] = $('new-theme').value.split(':');
      const theme = {id: themeID, revision: Number(themeRevision)};
      const path = template ? `/api/generator/templates/${template.id}/designs` : '/api/generator/designs';
      const body = template ? {name: $('new-name').value, version: template.version, theme} : {name: $('new-name').value, stage: $('new-stage').value, theme};
      const created = await (await request(path, 'POST', body)).json();
      if (sequence !== editSequence || saving) { notify('New show design saved. Your current edits were retained; open the new design from the list.'); await listDesigns(); return; }
      openDraft(created);
    } catch (error) { notify(error.message); }
    finally { button.disabled = false; }
  });

  function renderTemplateChoices() {
    const chosen = $('new-template').value;
    const target = $('update-template').value;
    $('new-template').replaceChildren(new Option('Blank design', ''));
    $('update-template').replaceChildren(new Option('Choose a saved template', ''));
    for (const template of templates) {
      if (template.content.stage === $('new-stage').value) $('new-template').add(new Option(`${template.name}${template.builtin ? ' · starter' : ` · v${template.version}`}`, template.id));
      if (!template.builtin) $('update-template').add(new Option(`${template.name} · v${template.version} · ${template.content.stage}`, template.id));
    }
    if ([...$('new-template').options].some(option => option.value === chosen)) $('new-template').value = chosen;
    if ([...$('update-template').options].some(option => option.value === target)) $('update-template').value = target;
    $('update-template-button').disabled = !$('update-template').value || templateSaving;
  }
  async function loadTemplates() {
    templates = await (await request('/api/generator/templates')).json();
    renderTemplateChoices();
  }
  $('new-stage').addEventListener('change', renderTemplateChoices);
  $('update-template').addEventListener('change', () => {
    const template = templates.find(item => item.id === $('update-template').value);
    if (template) $('template-name').value = template.name;
    $('update-template-button').disabled = !template;
    $('reload-template').hidden = true;
    $('template-status').textContent = template ? `Explicit update will replace template v${template.version} with this draft’s content. Existing show designs remain independent.` : '';
  });
  async function saveTemplate(update) {
    if (!draft || templateSaving) return;
    const selected = templates.find(item => item.id === $('update-template').value);
    if (update && !selected) return;
    if (!$('template-name').value.trim()) { $('template-status').textContent = 'Enter a template name first.'; $('template-name').focus(); return; }
    templateSaving = true;
    $('save-template').disabled = $('update-template-button').disabled = true;
    const snapshot = structuredClone(draft);
    const name = $('template-name').value;
    $('template-status').textContent = 'Saving explicit template snapshot…';
    try {
      const saved = await (await request(update ? `/api/generator/templates/${selected.id}` : '/api/generator/templates', update ? 'PUT' : 'POST', {name, version: update ? selected.version : 0, content: snapshot})).json();
      await loadTemplates();
      await loadThemes();
      $('update-template').value = saved.id;
      $('template-status').textContent = `Saved ${saved.name} · template v${saved.version}. Existing designs and generated revisions are unchanged.`;
      $('reload-template').hidden = true;
    } catch (error) {
      $('template-status').textContent = `${error.message} Your current draft and local edits are retained.`;
      $('reload-template').hidden = !(update && error.status === 409);
    } finally {
      templateSaving = false;
      $('save-template').disabled = false;
      $('update-template-button').disabled = !$('update-template').value;
    }
  }
  $('save-template').addEventListener('click', () => saveTemplate(false));
  $('update-template-button').addEventListener('click', () => saveTemplate(true));
  $('reload-template').addEventListener('click', async () => {
    if (!canLeave()) return;
    const id = $('update-template').value;
    const sequence = editSequence;
    try {
      const latest = await (await request(`/api/generator/templates/${id}`)).json();
      const created = await (await request(`/api/generator/templates/${id}/designs`, 'POST', {name: latest.name, version: latest.version, theme: structuredClone(draft.theme)})).json();
      await loadTemplates();
      await loadThemes();
      if (sequence !== editSequence || saving) { notify('Latest template copied to a new design. Your current edits were retained; open the new design from the list.'); await listDesigns(); return; }
      openDraft(created);
      $('template-status').textContent = `Loaded template v${latest.version} into an independent design.`;
      $('reload-template').hidden = true;
    } catch (error) { $('template-status').textContent = `${error.message} Your current draft is retained.`; }
  });

  function renderJobs() {
    if (!draft) return;
    const outstanding = jobs.filter(job => ['queued', 'running', 'cancelling'].includes(job.state));
    const busy = outstanding.length >= 8;
    $('generate-design').disabled = dirty || saving || conflict || jobRequest || busy;
    $('generation-save-hint').textContent = dirty || saving || conflict
      ? 'Save and resolve conflicts before generating. Your local edits are retained.'
      : `Generate saved draft v${draft.version}. Later edits leave the captured revision unchanged.`;
    const active = jobs.find(job => ['running', 'cancelling'].includes(job.state));
    $('generation-status').textContent = `${outstanding.length}/8 outstanding · ${active ? `${active.design_snapshot.name}: ${active.state} · ${active.progress}%` : 'Renderer available'} · one render at a time`;
    const relevant = jobs;
    $('generation-jobs').replaceChildren();
    for (const job of relevant) {
      const row = document.createElement('article'); row.className = 'generator-job'; row.dataset.jobId = job.id;
      const title = document.createElement('strong'); title.textContent = `${job.design_snapshot.name} · ${job.design_snapshot.stage.toUpperCase()} · ${job.state}${job.queue_position ? ` #${job.queue_position} in queue` : ''} · captured draft v${job.design_snapshot.version} · ${job.duration.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')} seconds`;
      const identity = document.createElement('p'); identity.textContent = `Design revision ${job.design_revision.slice(0, 12)} · ${job.profile.width} × ${job.profile.height} · ${job.profile.fps} fps`;
      row.append(title, identity);
      if (job.mix_gain != null) {
        const gain = document.createElement('p');
        gain.textContent = `Whole-mix gain: ${(job.mix_gain * 100).toFixed(2)}%${job.mix_gain < 1 ? ' · fixed peak protection; relative levels preserved' : ' · requested levels retained'}`;
        row.append(gain);
      }
      if (job.message) { const message = document.createElement('p'); message.textContent = job.message; row.append(message); }
      if (['failed', 'interrupted', 'cancelled'].includes(job.state)) {
        const retry = document.createElement('button'); retry.type = 'button'; retry.className = 'preview-button'; retry.textContent = 'Retry captured revision'; retry.disabled = busy;
        retry.addEventListener('click', async () => { retry.disabled = true; try { await request(`/api/generator/jobs/${job.id}/retry`, 'POST', {}); await loadJobs(); } catch (error) { generationError(error.message); retry.disabled = false; } }); row.append(retry);
      }
      if (job.error) { const error = document.createElement('p'); error.textContent = job.error; row.append(error); }
      if (['queued', 'running', 'cancelling'].includes(job.state)) {
        const progress = document.createElement('progress'); progress.max = 100; progress.value = job.progress; progress.setAttribute('aria-label', 'Generation progress'); row.append(progress);
        const cancel = document.createElement('button'); cancel.type = 'button'; cancel.className = 'preview-button'; cancel.textContent = job.state === 'cancelling' ? 'Cancelling…' : 'Cancel generation'; cancel.disabled = job.state === 'cancelling';
        cancel.addEventListener('click', async () => { cancel.disabled = true; try { await request(`/api/generator/jobs/${job.id}/cancel`, 'POST', {}); await loadJobs(); } catch (error) { generationError(error.message); cancel.disabled = false; } }); row.append(cancel);
      }
      if (job.state === 'ready') {
        const detail = document.createElement('p');
        const media = mediaRevisions.find(item => item.id === job.media_revision);
        const selected = ['prestream', 'ending'].filter(kind => stageSelections[kind] === job.media_revision).map(kind => kind.toUpperCase());
        const onAir = stageStatus.source === 'file' && stageStatus.media?.revision === job.media_revision;
        const returning = stageStatus.return_media?.revision === job.media_revision;
        const newer = job.design_snapshot.id === draft.id && job.design_snapshot.version < draft.version;
        detail.textContent = `Media ${job.media_revision} · ${media?.state || 'unavailable'} · ${selected.length ? `selected for next ${selected.join(' / ')}` : 'not selected for a stage'} · ${onAir ? stageStatus.mode === 'preview_only' ? 'playing in rehearsal' : 'on-air source' : 'not the on-air source'}${returning ? ` · retained for return to ${stageStatus.return_stage}` : ''}${newer ? ' · newer saved draft exists' : ''}${job.design_snapshot.id === draft.id && dirty ? ' · unsaved local edits' : ''}${media?.error ? ` · ${media.error}` : ''}`;
        const preview = document.createElement('button'); preview.type = 'button'; preview.className = 'preview-button'; preview.textContent = 'Preview exact revision'; preview.disabled = media?.state !== 'ready'; preview.addEventListener('click', () => previewGenerated(job)); row.append(detail, preview);
        if (media?.state === 'ready' && job.design_snapshot.stage === 'prestream') {
          const actions = document.createElement('div'); actions.className = 'generator-actions';
          const addLink = (action, text) => {
            const link = document.createElement('a'); link.className = 'preview-button'; link.target = '_blank'; link.rel = 'noopener'; link.textContent = text;
            const params = {media_revision: job.media_revision, media_stage: 'prestream', media_action: action};
            if (action !== 'select_next') Object.assign(params, {server_id: stageStatus.server_id, context: stageStatus.context});
            link.href = `/?${new URLSearchParams(params)}#media-intent`; actions.append(link);
          };
          addLink('select_next', 'Select for next PRESTREAM');
          if (stageStatus.stage === 'PRESTREAM' && stageStatus.media?.revision && stageStatus.media.revision !== job.media_revision) addLink('replace_now', 'Review Replace PRESTREAM now');
          if (stageStatus.return_stage === 'PRESTREAM' && stageStatus.return_media?.revision && stageStatus.return_media.revision !== job.media_revision) addLink('replace_on_return', 'Review Replace PRESTREAM on return');
          row.append(actions);
        }
      }
      $('generation-jobs').append(row);
    }
  }
  function generationError(message) { $('generation-error').textContent = message; $('generation-error').hidden = !message; }
  async function loadJobs() {
    if (jobsFetching) return;
    jobsFetching = true;
    try {
      const [nextJobs, selections, stage, catalog] = await Promise.all(['/api/generator/jobs', '/api/stage-media', '/api/stage', '/api/library'].map(async path => (await request(path)).json()));
      jobs = nextJobs; stageSelections = selections; stageStatus = stage; mediaRevisions = catalog.revisions || [];
      if ($('generation-error').textContent.startsWith('Generation status unavailable:')) generationError('');
      renderJobs();
    } catch (error) { generationError(`Generation status unavailable: ${error.message}`); }
    finally { jobsFetching = false; }
  }
  function previewGenerated(job) {
    reviewedRevision = job.media_revision;
    $('generated-preview').hidden = false;
    const passes = job.design_snapshot.stage === 'prestream' && job.exact_timing ? 2 : 1;
    $('generated-identity').textContent = `${passes === 2 ? `Two passes · ${job.duration}s per complete cycle. ` : 'One pass. '}Captured draft v${job.design_snapshot.version} · media revision ${reviewedRevision}. Preview does not change the broadcast.`;
    const video = $('generated-video');
    video.src = `/api/library/revisions/${encodeURIComponent(reviewedRevision)}/preview${passes === 2 ? "?passes=2" : ""}`;
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


  function assetError(message) { $('asset-error').textContent = message; $('asset-error').hidden = !message; }
  function renderImagePicker() {
    const scene = draft?.scenes[selectedScene]; if (!scene) return;
    $('scene-image-controls').hidden = scene.layout === 'media' && scene.media_kind === 'video' || !scene.image && !['text-image', 'media'].includes(scene.layout);
    const value = scene.image ? `${scene.image.id}:${scene.image.revision}` : '';
    const options = [new Option('No image selected', '')];
    for (const asset of assets.filter(asset => asset.kind === 'image')) for (const revision of asset.revisions) options.push(new Option(`${asset.name} · revision ${revision.revision}${revision.revision === asset.revision ? ' · latest' : ''}`, `${asset.id}:${revision.revision}`));
    if (value && !options.some(option => option.value === value)) options.push(new Option(`Unavailable image · revision ${scene.image.revision}`, value));
    $('scene-image').replaceChildren(...options); $('scene-image').value = value;
    const asset = assets.find(asset => asset.id === scene.image?.id);
    $('adopt-image-revision').hidden = !asset || asset.revision === scene.image.revision;
  }
  function renderAssets() {
    const target = $('asset-upload-target').value;
    $('asset-upload-target').replaceChildren(new Option(`New independent ${$('asset-kind').value}`, ''), ...assets.filter(asset => asset.kind === $('asset-kind').value).map(asset => new Option(`New revision of ${asset.name}`, asset.id)));
    $('asset-upload-target').value = assets.some(asset => asset.id === target) ? target : '';
    $('asset-list').replaceChildren();
    for (const asset of assets) {
      const card = document.createElement('details'); card.className = 'generator-asset'; card.dataset.assetId = asset.id;
      const summary = document.createElement('summary'); summary.textContent = `${asset.name} · revision ${asset.revision} · ${asset.uses.length} use(s)`;
      const img = document.createElement(asset.kind === 'video' ? 'video' : asset.kind === 'audio' ? 'audio' : 'img'); if (asset.kind !== 'image') { img.controls = true; img.muted = asset.kind === 'video'; img.playsInline = true; img.preload = 'none'; } else { img.alt = asset.name; img.loading = 'lazy'; } img.src = `/api/generator/assets/${asset.id}/revisions/${asset.revision}`;
      const list = document.createElement('ul');
      for (const use of asset.uses) { const item = document.createElement('li'); item.textContent = `${use.kind}: ${use.name} · revision ${use.revision}`; list.append(item); }
      const remove = document.createElement('button'); remove.type = 'button'; remove.className = 'preview-button'; remove.textContent = `Delete unused ${asset.kind}`; remove.disabled = asset.uses.length > 0;
      remove.addEventListener('click', async () => {
        if (!window.confirm(`Delete unused asset “${asset.name}” and all its revisions?`)) return;
        remove.disabled = true;
        try { await request(`/api/generator/assets/${asset.id}`, 'DELETE', {}); assetError(''); await loadAssets(); }
        catch (error) { assetError(error.message); await loadAssets(); }
      });
      card.append(summary, img, list, remove); $('asset-list').append(card);
    }
    renderImagePicker(); renderVideoPicker(); renderMusicPicker(); renderThemeAssets();
  }
  async function loadAssets() {
    if (assetsFetching) return;
    assetsFetching = true;
    try { assets = await (await request('/api/generator/assets')).json(); renderAssets(); }
    catch (error) { assetError(`Asset library unavailable: ${error.message}`); }
    finally { assetsFetching = false; }
  }
  function renderMusicPicker() {
    if (!draft) return;
    const track = draft.soundtrack;
    const value = track ? `${track.asset.id}:${track.asset.revision}` : '';
    const options = [new Option('No soundtrack', '')];
    for (const asset of assets.filter(asset => asset.kind === 'audio')) for (const revision of asset.revisions) options.push(new Option(`${asset.name} · revision ${revision.revision}${revision.revision === asset.revision ? ' · latest' : ''}`, `${asset.id}:${revision.revision}`));
    if (value && !options.some(option => option.value === value)) options.push(new Option('Unavailable selected music', value));
    $('soundtrack-asset').replaceChildren(...options); $('soundtrack-asset').value = value;
    $('soundtrack-settings').hidden = !track;
    $('soundtrack-mode').value = track?.mode || 'end'; $('soundtrack-volume').value = track?.volume_percent ?? 100;
    $('soundtrack-fade-in').value = track?.fade_in_seconds || 0; $('soundtrack-fade-out').value = track?.fade_out_seconds || 0;
    const asset = assets.find(asset => asset.id === track?.asset.id);
    const meta = asset?.revisions.find(revision => revision.revision === track.asset.revision);
    $('soundtrack-info').textContent = meta ? `Track: ${meta.duration_seconds.toFixed(3)} seconds · pinned revision ${track.asset.revision}` : 'Select an available music revision.';
    $('adopt-music-revision').hidden = !asset || asset.revision === track.asset.revision;
  }
  $('adopt-music-revision').addEventListener('click', () => {
    const track = draft?.soundtrack, asset = assets.find(asset => asset.id === track?.asset.id);
    if (!asset) return; track.asset.revision = asset.revision; renderMusicPicker(); changed();
  });
  function selectedVideoMeta() {
    const ref = draft?.scenes[selectedScene]?.video?.asset;
    return assets.find(asset => asset.id === ref?.id)?.revisions.find(revision => revision.revision === ref.revision);
  }
  function renderVideoPicker() {
    const scene = draft?.scenes[selectedScene]; if (!scene) return;
    const active = scene.layout === 'media' && scene.media_kind === 'video';
    $('scene-video-controls').hidden = !active; $('source-video-preview').hidden = !active;
    const value = scene.video ? `${scene.video.asset.id}:${scene.video.asset.revision}` : '';
    const choices = [new Option('No video selected', '')];
    for (const asset of assets.filter(asset => asset.kind === 'video')) for (const revision of asset.revisions) choices.push(new Option(`${asset.name} · revision ${revision.revision}${revision.revision === asset.revision ? ' · latest' : ''}`, `${asset.id}:${revision.revision}`));
    if (value && !choices.some(option => option.value === value)) choices.push(new Option('Unavailable selected video', value));
    $('scene-video').replaceChildren(...choices); $('scene-video').value = value;
    $('video-trim-start').value = scene.video?.trim_start_seconds || 0;
    $('video-trim-end').value = scene.video?.trim_end_seconds || '';
    $('video-repeat').checked = !!scene.video?.repeat; $('video-audio').checked = !!scene.video?.audio_enabled;
    $('video-volume').value = scene.video?.audio_volume_percent ?? 100;
    const meta = selectedVideoMeta();
    $('video-source-info').textContent = meta ? `Source: ${meta.duration_seconds.toFixed(3)} seconds · ${meta.width} × ${meta.height} · ${meta.has_audio ? 'audio available; muted unless enabled' : 'no audio track; keep source audio disabled'}` : 'Select a video revision.';
    const asset = assets.find(asset => asset.id === scene.video?.asset.id);
    $('adopt-video-revision').hidden = !asset || asset.revision === scene.video.asset.revision;
  }
  $('scene-video').addEventListener('change', () => {
    const scene = draft?.scenes[selectedScene]; if (!scene) return;
    const [id, revision] = $('scene-video').value.split(':');
    if (!id) delete scene.video;
    else {
      const fresh = scene.video?.asset.id !== id;
      scene.video = {...(fresh ? {} : scene.video), asset: {id, revision: Number(revision)}};
      if (fresh) { scene.video.audio_volume_percent = 100; const meta = selectedVideoMeta(); if (meta) scene.duration_seconds = meta.duration_seconds; }
    }
    renderSelectedScene(); changed();
  });
  $('adopt-video-revision').addEventListener('click', () => {
    const scene = draft?.scenes[selectedScene]; const asset = assets.find(asset => asset.id === scene?.video?.asset.id);
    if (!asset) return; scene.video.asset.revision = asset.revision; renderVideoPicker(); changed();
  });
  let sourcePreview;
  function stopSourcePreview() { const video = $('source-video'); video.pause(); video.onloadedmetadata = null; sourcePreview = null; $('source-video-status').textContent = 'Source preview stopped. Play to review the current settings.'; }
  $('source-video-stop').addEventListener('click', stopSourcePreview);
  $('source-video-play').addEventListener('click', async () => {
    stopSourcePreview(); const scene = draft?.scenes[selectedScene], meta = selectedVideoMeta();
    if (!scene?.video || !meta || validationIssues.some(issue => issue.field.startsWith(`scenes.${selectedScene}.`))) { $('source-video-status').textContent = 'Resolve the video scene validation before previewing.'; return; }
    const video = $('source-video'); const start = scene.video.trim_start_seconds || 0;
    sourcePreview = {start, end: scene.video.trim_end_seconds || meta.duration_seconds, remaining: scene.duration_seconds, last: start, repeat: scene.video.repeat};
    video.src = `/api/generator/assets/${scene.video.asset.id}/revisions/${scene.video.asset.revision}`;
    video.muted = !scene.video.audio_enabled; video.volume = (scene.video.audio_volume_percent ?? 100) / 100;
    video.onloadedmetadata = () => { if (sourcePreview) video.currentTime = start; };
    video.play().catch(() => { stopSourcePreview(); $('source-video-status').textContent = 'Source preview could not start.'; });
    $('source-video-status').textContent = 'Playing selected source timing and audio. Generate to review exact composition and frame timing.';
  });
  function advanceSourcePreview() {
    const video = $('source-video'), state = sourcePreview;
    if (!state || (video.paused && !video.ended)) return;
    const position = video.currentTime; state.remaining -= Math.max(0, position - state.last); state.last = position;
    if (state.remaining <= 0.01) { stopSourcePreview(); $('source-video-status').textContent = 'Source range preview finished.'; }
    else if (position >= state.end - 0.01 || video.ended) {
      if (state.repeat) { video.currentTime = state.start; state.last = state.start; video.play().catch(() => { stopSourcePreview(); $('source-video-status').textContent = 'Source preview could not repeat.'; }); }
      else { stopSourcePreview(); $('source-video-status').textContent = 'Selected source range finished.'; }
    }
  }
  $('source-video').addEventListener('timeupdate', advanceSourcePreview);
  $('source-video').addEventListener('ended', advanceSourcePreview);
  if ($('source-video').requestVideoFrameCallback) {
    const frame = () => { advanceSourcePreview(); $('source-video').requestVideoFrameCallback(frame); }; $('source-video').requestVideoFrameCallback(frame);
  }
  $('asset-kind').addEventListener('change', () => {
    const kind = $('asset-kind').value;
    $('asset-file').accept = kind === 'video' ? 'video/mp4,.mp4' : kind === 'audio' ? 'audio/mpeg,audio/wav,.mp3,.wav' : 'image/png,image/jpeg';
    $('asset-file-label').textContent = kind === 'video' ? 'H.264/AAC MP4 video' : kind === 'audio' ? 'MP3 or PCM WAV · up to 32 MiB and 10 minutes' : 'PNG or JPEG';
    $('asset-file').value = ''; renderAssets();
  });
  $('asset-upload').addEventListener('submit', async event => {
    event.preventDefault(); if (assetUploading) return;
    const file = $('asset-file').files[0]; if (!file) return;
    const kind = $('asset-kind').value;
    if (file.size > (kind === 'video' ? 512 : kind === 'audio' ? 32 : 10) * 1048576) { assetError(`Choose a ${kind} up to ${kind === 'video' ? 512 : kind === 'audio' ? 32 : 10} MiB.`); return; }
    const target = assets.find(asset => asset.id === $('asset-upload-target').value);
    const data = new FormData(); data.append('file', file);
    assetUploading = true; $('asset-upload-button').disabled = true; assetError(''); $('asset-status').textContent = 'Uploading asset; preparation waits for current media work…';
    try {
      const path = target ? `/api/generator/assets/${target.id}/revisions?version=${target.revision}&kind=${kind}` : `/api/generator/assets?kind=${kind}`;
      const response = await fetch(path, {method: 'POST', credentials: 'same-origin', headers: {'X-Restreamer-Control': '1'}, body: data});
      if (!response.ok) throw new Error(await response.text());
      const asset = await response.json(); $('asset-file').value = '';
      $('asset-status').textContent = `${asset.name} saved as revision ${asset.revision}. Choose its revision in a scene or soundtrack to use it.`;
      await loadAssets();
    } catch (error) { assetError(error.message); $('asset-status').textContent = 'Asset was not acknowledged as saved; previous revisions remain unchanged.'; }
    finally { assetUploading = false; $('asset-upload-button').disabled = false; }
  });
  $('adopt-image-revision').addEventListener('click', () => {
    const scene = draft?.scenes[selectedScene]; const asset = assets.find(asset => asset.id === scene?.image?.id);
    if (!asset) return; scene.image = {id: asset.id, revision: asset.revision}; renderImagePicker(); changed();
  });
  $('asset-refresh').addEventListener('click', loadAssets);
  loadAssets();


  const themeFields = [
    ['font', 'Default font', ['go-sans', 'go-mono']], ['font_size', 'Font size at 1080p', 24, 120],
    ['background_color', 'Background color', 'color'], ['text_color', 'Text color', 'color'], ['accent_color', 'Accent color', 'color'],
    ['background', 'Background image revision', 'asset'], ['logo', 'Logo image revision', 'asset'],
    ['logo_position', 'Logo corner', ['top-left', 'top-right', 'bottom-left', 'bottom-right']], ['logo_height_percent', 'Logo height (%)', 2, 8],
    ['width_percent', 'Default content width (%)', 30, 90], ['height_percent', 'Default content height (%)', 30, 90],
    ['line_spacing_percent', 'Line spacing (%)', 100, 180], ['list_spacing_percent', 'List item spacing (%)', 0, 100],
    ['border_style', 'Border', ['none', 'line', 'pixel']], ['border_width', 'Border width at 1080p', 1, 12],
    ['effect', 'Decorative effect', ['none', 'pixel-trail']], ['effect_speed', 'Effect steps per second', 1, 12]
  ];
  for (const [key, title, kind, maximum] of themeFields) {
    const label = document.createElement('label'); label.textContent = title;
    const input = document.createElement(Array.isArray(kind) || kind === 'asset' ? 'select' : 'input'); input.id = `theme-${key}`;
    if (Array.isArray(kind)) input.replaceChildren(...kind.map(value => new Option(value.replaceAll('-', ' '), value)));
    else if (typeof kind === 'number') { input.type = 'number'; input.min = kind; input.max = maximum; input.step = '1'; input.required = true; }
    else if (kind === 'color') input.type = 'color';
    label.append(input); $('theme-fields').append(label);
  }
  function themeKey(theme) { return `${theme.id}:${theme.revision}`; }
  function themeRef(value) { const [id, revision] = value.split(':'); return {id, revision: Number(revision)}; }
  function themeError(message) { $('theme-error').textContent = message; $('theme-error').hidden = !message; }
  function renderThemeAssets() {
    for (const key of ['background', 'logo']) {
      const select = $(`theme-${key}`); const value = select.options.length ? select.value : (editingTheme?.style[key] ? themeKey(editingTheme.style[key]) : '');
      const options = [new Option('None', '')];
      for (const asset of assets.filter(asset => asset.kind === 'image')) for (const rev of asset.revisions) options.push(new Option(`${asset.name} · revision ${rev.revision}`, `${asset.id}:${rev.revision}`));
      if (value && !options.some(option => option.value === value)) options.push(new Option('Unavailable pinned image', value));
      select.replaceChildren(...options); select.value = value;
    }
  }
  function renderDesignTheme() {
    const value = draft ? themeKey(draft.theme) : '';
    $('design-theme').replaceChildren(...themes.map(theme => new Option(`${theme.name} · revision ${theme.revision}`, themeKey(theme))));
    if (value && !themes.some(theme => themeKey(theme) === value)) $('design-theme').add(new Option('Unavailable pinned theme', value));
    $('design-theme').value = value;
    const selected = themes.find(theme => themeKey(theme) === value);
    const latest = themes.filter(theme => theme.id === draft?.theme.id).sort((a,b) => b.revision-a.revision)[0];
    $('apply-theme-update').hidden = !latest || latest.revision <= draft.theme.revision;
    $('design-theme-status').textContent = selected ? `${selected.name} · revision ${selected.revision}. Theme edits leave this draft pinned until you apply an update.` : 'Choose an available exact theme revision.';
    $('preview-time-control').hidden = selected?.style.effect !== 'pixel-trail';
    if (selected) {
      $('scene-font').options[0].textContent = `Theme default · ${selected.style.font === 'go-mono' ? 'Go Mono' : 'Go Sans'}`;
      $('scene-size').placeholder = `Theme default · ${selected.style.font_size}`;
      $('region-width').placeholder = `Theme default · ${selected.style.content_region.width_percent}`;
      $('region-height').placeholder = `Theme default · ${selected.style.content_region.height_percent}`;
    }
  }
  function editTheme(theme) {
    editingTheme = structuredClone(theme); themeDirty = false; themeError('');
    $('theme-name').value = theme.name; $('theme-library').value = themeKey(theme);
    for (const key of ['background', 'logo']) $(`theme-${key}`).replaceChildren();
    renderThemeAssets();
    for (const [key] of themeFields) {
      const value = ['width_percent','height_percent'].includes(key) ? theme.style.content_region[key] : theme.style[key];
      $(`theme-${key}`).value = value && typeof value === 'object' ? themeKey(value) : value ?? '';
    }
    $('theme-publish').disabled = theme.builtin;
    $('theme-status').textContent = `${theme.name} · revision ${theme.revision}${theme.builtin ? '. Duplicate to create an editable variant.' : '. Publish changes explicitly; existing designs keep their pinned revision.'}`;
  }
  function localTheme() {
    const theme = structuredClone(editingTheme); theme.name = $('theme-name').value;
    for (const [key, , kind] of themeFields) {
      const value = $(`theme-${key}`).value;
      if (['width_percent','height_percent'].includes(key)) theme.style.content_region[key] = Number(value);
      else if (kind === 'asset') { if (value) theme.style[key] = themeRef(value); else delete theme.style[key]; }
      else theme.style[key] = typeof kind === 'number' ? Number(value) : value;
    }
    return theme;
  }
  async function loadThemes() {
    themes = await (await request('/api/generator/themes')).json();
    const selection = $('new-theme').value;
    $('new-theme').replaceChildren(...themes.map(theme => new Option(`${theme.name} · revision ${theme.revision}`, themeKey(theme))));
    $('new-theme').value = themes.some(theme => themeKey(theme) === selection) ? selection : 'retro:2';
    $('theme-library').replaceChildren(...themes.map(theme => new Option(`${theme.name} · revision ${theme.revision}`, themeKey(theme))));
    if (!editingTheme) editTheme(themes.find(theme => theme.id === 'retro' && theme.revision === 2));
    else $('theme-library').value = themeKey(editingTheme);
    renderDesignTheme();
  }
  $('theme-library').addEventListener('change', () => {
    if (themeSaving || (themeDirty && !window.confirm('Discard unpublished theme settings?'))) { $('theme-library').value = themeKey(editingTheme); return; }
    editTheme(themes.find(theme => themeKey(theme) === $('theme-library').value));
  });
  $('theme-form').addEventListener('input', () => { themeDirty = true; $('theme-status').textContent = 'Unpublished theme settings. Save as a new theme or publish a revision.'; });
  async function publishTheme(duplicate) {
    if (!editingTheme || themeSaving) return;
    if (!$('theme-form').reportValidity()) return;
    const local = localTheme(); themeSaving = true; themeError('');
    $('theme-form').querySelectorAll('input, select, button').forEach(control => { control.disabled = true; });
    $('theme-library').disabled = true;
    try {
      const body = duplicate ? {name: local.name, base: themeRef(themeKey(local)), style: local.style} : local;
      const saved = await (await request(duplicate ? '/api/generator/themes' : `/api/generator/themes/${local.id}`, duplicate ? 'POST' : 'PUT', body)).json();
      editTheme(saved); await loadThemes(); await loadAssets();
      $('theme-status').textContent = `${saved.name} · revision ${saved.revision} saved. Select it in a design or use Apply updated theme to adopt it.`;
    } catch (error) { themeError(`${error.message} Your local settings are retained. Reload latest revision to discard them, or save settings as a new theme.`); }
    finally { themeSaving = false; $('theme-form').querySelectorAll('input, select, button').forEach(control => { control.disabled = false; }); $('theme-library').disabled = false; $('theme-publish').disabled = editingTheme.builtin; }
  }
  $('theme-form').addEventListener('submit', event => { event.preventDefault(); publishTheme(false); });
  $('theme-duplicate').addEventListener('click', () => publishTheme(true));
  $('theme-refresh').addEventListener('click', async () => {
    if (themeSaving || (themeDirty && !window.confirm('Discard unpublished settings and reload the latest theme revision?'))) return;
    try { await loadThemes(); editTheme(themes.filter(theme => theme.id === editingTheme.id).sort((a,b) => b.revision-a.revision)[0]); } catch (error) { themeError(error.message); }
  });
  $('design-theme').addEventListener('change', event => { event.stopPropagation(); if (!draft) return; draft.theme = themeRef($('design-theme').value); renderDesignTheme(); changed(); });
  $('apply-theme-update').addEventListener('click', () => {
    const latest = themes.filter(theme => theme.id === draft?.theme.id).sort((a,b) => b.revision-a.revision)[0];
    if (!latest) return; draft.theme = themeRef(themeKey(latest)); renderDesignTheme(); changed();
  });
  $('preview-time').addEventListener('input', schedulePreview);

  window.addEventListener('beforeunload', event => { if (dirty || saving || themeDirty || themeSaving) { event.preventDefault(); event.returnValue = ''; } });
  (async () => {
    try {
      await listDesigns();
      await loadTemplates();
      await loadThemes();
      const id = new URLSearchParams(location.search).get('id');
      if (id) openDraft(await (await request(`/api/generator/designs/${encodeURIComponent(id)}`)).json());
    } catch (error) { notify(error.message); }
  })();
})();
