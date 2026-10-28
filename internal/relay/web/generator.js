(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  let draft = null;
  let templates = [];
  let themes = [];
  let editingTheme = null;
  const pinnedThemes = new Map();
  let themePreviewSequence = 0, themePreviewTimer, themePreviewAbort, themePreviewURL = '', themeDownloadName = '';
  let themeView = 'border';
  let themeDirty = false;
  let themeSaving = false;
  let templateSaving = false;
  let selectedScene = 0;
  let itemEditors = [];
  let validationIssues = [];
  let dirty = false;
  let saving = false;
  let deleting = false;
  let conflict = false;
  let editSequence = 0;
  let previewSequence = 0;
  let saveTimer;
  let previewTimer;
  let previewAbort;
  let jobs = [];
  let jobRequest = false;
  let jobsFetching = false;
  let jobsLoaded = false;
  let jobsCurrent = false;
  let lastReadyJob = null;
  let reviewedRevision = '';
  let stageSelections = {};
  let stageStatus = {}, mediaRevisions = [];
  let assets = [];
  let assetsFetching = false;
  let assetUploading = false;
  let assetUploadRequest;
  function showCollection(name, focus = false) {
    if (name !== 'brb') $('brb-theme-video').pause();
    $('generated-video').pause(); $('source-video').pause();
    $('shared-editor-nav').hidden = name !== 'brb';
    document.body.dataset.studioView = name;
    if (name === 'music') loadAssets();
    if (name === 'setup') window.showSetup?.refresh();
    document.querySelectorAll('[data-collection-panel]').forEach(panel => { panel.hidden = panel.dataset.collectionPanel !== name; });
    $('design-editor').hidden = true;
    renderCollectionContext();
    $('resume-design').hidden = !draft;
    document.querySelectorAll('[data-studio-collection]').forEach(button => {
      if (button.dataset.studioCollection === name) button.setAttribute('aria-current', 'page');
      else button.removeAttribute('aria-current');
    });
    if (name === 'themes' && editingTheme) scheduleThemePreview();
    if (focus) document.querySelector(`[data-studio-collection="${name}"]`)?.focus();
    const filter=name === 'music' ? $('music-search') : document.querySelector(`[data-collection-panel="${name}"] [data-filter-list]`);
    const musicView = name === 'music' ? `&music_view=${$('music-playlists').hidden ? 'tracks' : 'playlists'}` : '';
    history.replaceState(null, '', name === 'brb' ? `/editor?editor=brb${requestedDesign?"&id="+encodeURIComponent(requestedDesign):""}&return=${encodeURIComponent(returnPath)}` : name === 'setup' ? '/setup' : `/library?collection=${name}${musicView}${filter?.value?'&q='+encodeURIComponent(filter.value):''}`);
    window.dispatchEvent(new Event('studio-route-changed'));
  }
  function showEditor() {
    $('brb-theme-video').pause();
    $('shared-editor-nav').hidden = false;
    document.body.dataset.studioView = 'editor';
    document.querySelectorAll('[data-collection-panel]').forEach(panel => { panel.hidden = true; });
    $('design-editor').hidden = false;
    $('resume-design').hidden = true;
    document.querySelectorAll('[data-studio-collection]').forEach(button => button.removeAttribute('aria-current'));
  }
  function showInspector(name) {
    document.querySelectorAll('[data-inspector-panel]').forEach(panel => { panel.hidden = panel.dataset.inspectorPanel !== name; });
    document.querySelectorAll('[data-inspector]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.inspector === name)));
  }
  function showEditorPanel(name) {
    document.querySelector('.studio-editor-grid').dataset.panel = name;
    document.querySelectorAll('[data-editor-panel]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.editorPanel === name)));
  }
  function showPreviewPanel(name) {
    $('studio-scene-preview').hidden = name !== 'scene';
    $('studio-results').hidden = name !== 'results';
    document.querySelectorAll('[data-preview-panel]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.previewPanel === name)));
  }
  document.querySelectorAll('[data-studio-collection]').forEach(button => button.addEventListener('click', () => showCollection(button.dataset.studioCollection)));
  const initialParams = new URLSearchParams(location.search);
  const initialRoute = window.studioRouting.parse(new URL(location.href));
  let returnPath = initialRoute.page === 'library' ? `/library?collection=${initialRoute.collection}${initialParams.get('q')?'&q='+encodeURIComponent(initialParams.get('q')):''}` : initialRoute.back;
  const requestedCollection = initialRoute.editor === 'brb' ? 'brb' : initialRoute.page === 'library' ? initialRoute.collection : 'setup';
  // Defer routing until the editor has loaded the requested design.
  const requestedDesign = initialParams.get('id');
  if (['prestream','ending'].includes(initialParams.get('stage'))) $('new-stage').value = initialParams.get('stage');
  document.querySelectorAll('[data-inspector]').forEach(button => button.addEventListener('click', () => showInspector(button.dataset.inspector)));
  document.querySelectorAll('[data-editor-panel]').forEach(button => button.addEventListener('click', () => showEditorPanel(button.dataset.editorPanel)));
  document.querySelectorAll('[data-preview-panel]').forEach(button => button.addEventListener('click', () => showPreviewPanel(button.dataset.previewPanel)));
  $('back-to-designs').addEventListener('click', () => { location.href = returnPath; });
  $('resume-design').addEventListener('click', () => { showEditor(); $('editor-heading').focus(); });
  const mobileStudio = matchMedia('(max-width: 600px)');
  function updateStudioDevice() {
    showPreviewPanel(mobileStudio.matches ? 'results' : 'scene');
  }
  mobileStudio.addEventListener('change', updateStudioDevice);
  updateStudioDevice();
  let animationTiming = null;
  const fields = {
    animation_loops: ['scene-loops', 'loops-error'],
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
    if(namingJob){notify('Save or cancel the video name before opening another design.');return false;}
    if (deleting) { notify('Deletion is in progress. Wait for its result.'); return false; }
    if (saving) { notify('A save is in progress. Wait for its acknowledgement before opening another draft.'); return false; }
    return !dirty || window.confirm('Discard your unsaved local edits and open another design?');
  }
  const selectedDesigns=new Map(),selectedVideos=new Map(),cleanupModes={designs:false,jobs:false};
  let namingJob='';
  function cleanupControl(id,selected,kind,reload) {
    const root=$(id);root.replaceChildren();
    if(!cleanupModes[kind]){const start=document.createElement('button');start.type='button';start.className='preview-button';start.textContent='Select items';start.onclick=()=>{cleanupModes[kind]=true;document.querySelectorAll(`[data-cleanup-kind="${kind}"]`).forEach(box=>{box.hidden=false;});cleanupControl(id,selected,kind,reload);};root.append(start);return;}
    const cancel=document.createElement('button');cancel.type='button';cancel.className='preview-button';cancel.textContent='Cancel selection';cancel.onclick=()=>{cleanupModes[kind]=false;selected.clear();document.querySelectorAll(`[data-cleanup-kind="${kind}"]`).forEach(box=>{box.hidden=true;box.checked=false;});cleanupControl(id,selected,kind,reload);};root.append(cancel);
    const button=document.createElement('button');button.type='button';button.className='preview-button';button.textContent=`Delete selected (${selected.size})`;button.disabled=!selected.size||deleting;root.append(button);
    button.addEventListener('click',async()=>{
      if(deleting||saving||jobRequest)return;
      if(kind==='designs'&&draft&&selected.has(draft.id)&&!canLeave())return;
      const items=[...selected.values()];
      if(!window.confirm(`Delete ${items.length} ${kind==='designs'?'designs and their generated videos':'generated videos'}? Uploaded files stay. Selected, playing and shared media are protected.\n${items.map(item=>item.name||item.design_snapshot?.name||'Video').join('\n')}`))return;
      deleting=true;button.disabled=true;
      const results=await window.cleanupItems(items,item=>request(`/api/generator/${kind}/${item.id}`,'DELETE',kind==='designs'?{version:item.version}:{}));
      for(const result of results)if(result.deleted){selected.delete(result.item.id);if(kind==='designs'&&draft?.id===result.item.id){draft=null;dirty=false;stopGeneratedPreview();$('design-editor').hidden=true;}}
      deleting=false;await reload();
      notify(`${results.filter(r=>r.deleted).length} deleted. Uploaded files kept.${results.some(r=>!r.deleted)?' Some items were kept: '+results.filter(r=>!r.deleted).map(r=>`${r.item.name||r.item.design_snapshot?.name}: ${r.error}`).join('; '):''}`);
      cleanupControl(id,selected,kind,reload);
    });
  }
  function cleanupCheckbox(item,selected,kind) {
    const box=document.createElement('input');box.type='checkbox';box.checked=selected.has(item.id);box.className='cleanup-select studio-authoring';box.dataset.cleanupKind=kind;box.hidden=!cleanupModes[kind];box.setAttribute('aria-label',`Select ${item.name||item.design_snapshot?.name||'video'} for cleanup`);
    box.addEventListener('change',()=>{box.checked?selected.set(item.id,item):selected.delete(item.id);cleanupControl(kind==='designs'?'design-cleanup':'video-cleanup',selected,kind,kind==='designs'?listDesigns:loadJobs);});return box;
  }
  async function listDesigns() {
    const designs = await (await request('/api/generator/designs')).json();
    $('design-list-state').textContent = designs.length ? `${designs.length} saved design${designs.length === 1 ? '' : 's'}` : 'No saved designs yet. Create your first design on desktop.';
    $('design-list').replaceChildren();
    for (const design of designs) {
      const button = document.createElement('button');
      button.type = 'button';
      button.textContent = design.name || 'Untitled design';
      button.setAttribute('aria-current', String(design.id === draft?.id));
      const detail = document.createElement('small');
      detail.textContent = `${design.stage === 'prestream' ? 'Starting soon' : design.stage === 'brb' ? 'BRB' : 'Ending'} · Editable design`;
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
      const remove = document.createElement('button');
      remove.type = 'button'; remove.className = 'delete-button'; remove.textContent = 'Delete';
      remove.setAttribute('aria-label', `Delete design ${design.name || 'Untitled design'}`);
      remove.addEventListener('click', () => deleteDesign(design));
      const row = document.createElement('div'); row.className = 'design-row'; const more=document.createElement('details'),summary=document.createElement('summary');more.className='row-maintenance studio-authoring';summary.textContent='More';summary.setAttribute('aria-label',`More actions for ${design.name}`);more.append(summary,remove);row.append(cleanupCheckbox(design,selectedDesigns,'designs'),button,more);
      $('design-list').append(row);
    }
    cleanupControl('design-cleanup',selectedDesigns,'designs',listDesigns);
  }
  async function deleteDesign(design) {
    if (deleting || saving || jobRequest) { notify('Wait for the current save or request before deleting.'); return; }
    if (!window.confirm(`Permanently delete “${design.name || 'Untitled design'}”, its generation history and generated files? Uploaded images, videos and music stay in Reusable media. Output shared with another saved result is kept. Selected or playing output blocks deletion.${draft?.id === design.id && dirty ? ' Unsaved edits to this design will also be discarded.' : ''}`)) return;
    deleting = true; editSequence++;
    const current = draft?.id === design.id;
    if (current) clearTimeout(saveTimer);
    $('scene-form').inert = true;
    try {
      const result = await (await request(`/api/generator/designs/${design.id}`, 'DELETE', {version: current ? draft.version : design.version})).json();
      if (current) {
        clearTimeout(previewTimer); previewSequence++; previewAbort?.abort();
        draft = null; dirty = false; conflict = false; editSequence++;
        showCollection('designs');
        history.replaceState(null, '', '/studio');
      }
      stopGeneratedPreview();
      await listDesigns(); await loadJobs(); await loadAssets();
      notify(`Design deleted with ${result.deleted_media} generated file(s). Uploaded assets were kept.${result.retained_shared_media.length ? ' Shared generated output was retained for other results.' : ''}`);
    } catch (error) { notify(error.message); }
    finally { deleting = false; $('scene-form').inert = false; if (draft && dirty && !conflict) saveTimer = setTimeout(() => saveDraft(), 700); }
  }
  function stopGeneratedPreview() {
    const video = $('generated-video'); video.pause(); video.removeAttribute('src'); video.load();
    reviewedRevision = ''; $('generated-preview').hidden = true;
  }
  function openDraft(value) {
    if(document.body.dataset.workspace==='library')returnPath=location.pathname+location.search;
    const back=$('shared-editor-nav').querySelector('a');back.href=returnPath;back.textContent=returnPath.startsWith('/library')?'Library':'Show setup';
    if(value.stage==='brb'){location.href=`/editor?editor=brb&id=${encodeURIComponent(value.id)}&return=${encodeURIComponent(returnPath)}`;return;}
    clearTimeout(saveTimer);
    draft = value; dirty = false; conflict = false; editSequence++;
    loadPinnedTheme(value.theme);
    selectedScene = 0; validationIssues = [];
    stopGeneratedPreview();
    $('generation-history').open = false;
    $('design-name').value = draft.name;
    $('editor-heading').textContent = draft.name;
    $('loop-controls').hidden = draft.stage !== 'prestream';
    $('loop-transition').value = draft.loop_transition?.kind || 'cut';
    $('loop-duration').value = draft.loop_transition?.duration_seconds || 0.5;
    $('loop-duration-controls').hidden = $('loop-transition').value !== 'crossfade';
    renderScenes(); renderSelectedScene(); renderMusicPicker(); renderDesignTheme();
    $('ending-finish').hidden = draft.stage !== 'ending';
    $('ending-fade').value = draft.ending_fade_seconds ?? 1;
    $('design-stage').textContent = `${draft.stage.toUpperCase()} · EDITABLE DRAFT`;
    showEditor(); showInspector('scene'); showEditorPanel('preview');
    $('editor-heading').focus();
    notify(''); saveState(`Saved · version ${draft.version}`);
    history.replaceState(null, '', `/editor?id=${encodeURIComponent(draft.id)}&return=${encodeURIComponent(returnPath)}`);
    window.dispatchEvent(new Event('studio-route-changed'));
    listDesigns().catch(error => notify(error.message));
    schedulePreview();
    loadJobs();
  }
  let thumbnailSignature = '', thumbnails = new Map();
  const scenePreviewSignature = value => JSON.stringify([value.id,value.stage,value.theme,value.scenes,value.loop_transition,value.ending_fade_seconds]);
  function storeSceneThumbnail(index, bitmap) {
    const thumbnail=document.createElement('canvas');thumbnail.width=160;thumbnail.height=90;
    thumbnail.getContext('2d').drawImage(bitmap,0,0,160,90);thumbnails.set(index,thumbnail);
    const visible=$('scene-list').children[index]?.querySelector('canvas');
    if(visible)visible.getContext('2d').drawImage(thumbnail,0,0);
  }
  function renderScenes() {
    $('scene-list').replaceChildren();
    for (const [index, scene] of draft.scenes.entries()) {
      const button = document.createElement('button'); button.type = 'button';
      const invalid = validationIssues.some(issue => issue.field.startsWith(`scenes.${index}.`));
      const caption=document.createElement('span');
      caption.textContent = `${index + 1}. ${scene.text.split('\n')[0] || (scene.layout === 'list' ? 'List' : 'Untitled')} · ${scene.duration_seconds}s${invalid ? ' · needs attention' : ''}`;
      const thumbnail=document.createElement('canvas');thumbnail.width=160;thumbnail.height=90;thumbnail.setAttribute('aria-hidden','true');
      if(thumbnailSignature===scenePreviewSignature(draft)&&thumbnails.has(index))thumbnail.getContext('2d').drawImage(thumbnails.get(index),0,0);
      button.append(thumbnail,caption);
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
    $('editor-heading').textContent = draft.name || 'Untitled design';
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
    renderSceneLength();
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
    renderDesignTheme();
  }
  function renderSceneLength(pending = false) {
    const scene = draft?.scenes[selectedScene];
    if (!scene) return;
    const timing = animationTiming?.key === `${draft.stage}:${themeKey(draft.theme)}` ? animationTiming : null;
    const loops = scene.animation_loops !== undefined;
    $('scene-length-mode').value = loops ? 'loops' : 'seconds';
    $('scene-length-mode').querySelector('[value="loops"]').disabled = !timing?.cycle;
    $('scene-loops-control').hidden = !loops;
    $('scene-seconds-control').hidden = loops;
    $('scene-loops').value = scene.animation_loops ?? 1;
    $('scene-loops').max = timing?.cycle ? Math.floor(3600 / timing.cycle) : 300;
    $('scene-duration').value = scene.duration_seconds;
    $('scene-length-help').textContent = !timing ? 'Checking the theme’s animation length…'
      : !timing.cycle ? 'This theme has no repeating animation for this stage. Use seconds.'
      : loops ? pending ? `${scene.animation_loops} × ${timing.cycle}s · calculating…` : `${scene.animation_loops} × ${timing.cycle}s = ${scene.animation_loops * timing.cycle}s. Render length: ${scene.duration_seconds}s, including any crossfade overlap.`
      : `One animation loop is ${timing.cycle} seconds. Choose Animation loops to finish complete cycles. Background motion continues between scenes.`;
  }
  function renderTransitionControls() {
    const last = selectedScene === draft.scenes.length - 1;
    $('transition-duration-controls').hidden = $('scene-transition').value !== 'crossfade';
    $('transition-help').textContent = last ? 'This is the last scene. Its outgoing transition is saved for reordering but is not applied here.' : 'Crossfades overlap the end of this scene with the beginning of the next, including enabled video audio. Seconds-based scenes lose the overlap from their duration; Animation loops adds it to preserve complete cycles. Generate to inspect the exact transition; the quick preview shows one scene.';
  }
  function selectScene(index) { selectedScene = index; renderScenes(); renderSelectedScene(); showInspector('scene'); schedulePreview(); }
  function changed() {
    dirty = true; editSequence++;
    if (!conflict) { saveState('Unsaved local changes'); clearTimeout(saveTimer); saveTimer = setTimeout(() => saveDraft(), 700); }
    renderSceneLength(true); renderScenes(); schedulePreview();
  }
  function captureFields() {
    draft.name = $('design-name').value;
    $('editor-heading').textContent = draft.name || 'Untitled design';
    if (draft.stage === 'prestream') {
      draft.loop_transition = $('loop-transition').value === 'crossfade' ? {kind: 'crossfade', duration_seconds: Number($('loop-duration').value)} : {kind: 'cut'};
      $('loop-duration-controls').hidden = $('loop-transition').value !== 'crossfade';
    }
    if (draft.stage === 'ending') draft.ending_fade_seconds = Number($('ending-fade').value);
    const musicValue = $('soundtrack-asset').value;
    if (musicValue) {
      draft.soundtrack = {...window.musicSelection.decode(musicValue), mode: $('soundtrack-mode').value, volume_percent: Number($('soundtrack-volume').value), fade_in_seconds: Number($('soundtrack-fade-in').value), fade_out_seconds: Number($('soundtrack-fade-out').value)};
    } else delete draft.soundtrack;
    if (draft.duration_mode || $('duration-mode').value === 'music') draft.duration_mode = $('duration-mode').value;
    const scene = draft.scenes[selectedScene]; if (!scene) return;
    scene.layout = $('scene-layout').value;
    scene.media_kind = $('scene-media-kind').value;
    if (scene.video) { scene.video.trim_start_seconds = Number($('video-trim-start').value); scene.video.trim_end_seconds = Number($('video-trim-end').value); scene.video.repeat = $('video-repeat').checked; scene.video.audio_enabled = $('video-audio').checked; scene.video.audio_volume_percent = Number($('video-volume').value); }
    const imageValue = $('scene-image').value;
    if (imageValue) { const [id, revision] = imageValue.split(':'); scene.image = {id, revision: Number(revision)}; } else delete scene.image;
    scene.text = $('scene-text').value; scene.font = $('scene-font').value;
    scene.font_size = Number($('scene-size').value); scene.duration_seconds = Number($('scene-duration').value);
    if ($('scene-length-mode').value === 'loops') scene.animation_loops = Number($('scene-loops').value);
    else delete scene.animation_loops;
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
  function clearValidation() {
    for (const [input, output] of Object.values(fields)) { $(input).removeAttribute('aria-invalid'); $(output).textContent = ''; }
    for (const editor of itemEditors) { editor.input.removeAttribute('aria-invalid'); editor.error.textContent = ''; }
    $('scene-issues').replaceChildren();
  }
  function schedulePreview() {
    clearTimeout(previewTimer);
    previewSequence++;
    previewAbort?.abort();
    validationIssues = [];
    clearValidation();
    if (draft) renderScenes();
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
      animationTiming = {key: `${draft.stage}:${themeKey(draft.theme)}`, cycle: result.animation_loop_seconds};
      for (let i = 0; i < draft.scenes.length; i++) {
        if (draft.scenes[i].animation_loops !== undefined && result.scenes?.[i]) {
          draft.scenes[i].duration_seconds = result.scenes[i].duration_seconds;
          snapshot.scenes[i].duration_seconds = result.scenes[i].duration_seconds;
        }
      }
      const nextThumbnailSignature=scenePreviewSignature(snapshot);
      if(thumbnailSignature!==nextThumbnailSignature){thumbnails.clear();thumbnailSignature=nextThumbnailSignature;}
      renderSceneLength();
      validationIssues = result.issues;
      renderScenes();
      $('sequence-duration').textContent = `${draft.scenes.length} scene${draft.scenes.length === 1 ? '' : 's'} · ${result.duration_seconds.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')} seconds ${draft.duration_mode === 'music' ? 'per music pass (live repeats until stopped)' : draft.stage === 'prestream' ? 'per complete cycle after transition overlaps' : 'total after transition overlaps'} at ${result.profile.fps} fps`;
      if (draft.stage === 'ending') {
        const fade = Math.round((draft.ending_fade_seconds ?? 1) * result.profile.fps) / result.profile.fps;
        $('ending-fade-effective').textContent = fade === 0 ? 'Final fade disabled: the final image and audio are preserved.' : `Final ${fade.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')} seconds fade to black and silence after rounding to video frames, within the total above.`;
      }
      clearValidation();
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
            if (field) { $(field[0]).setAttribute('aria-invalid', 'true'); $(field[1]).textContent = issue.message; const details=$(field[0]).closest('details');if(details)details.open=true; }
          }
        }
        const entry = document.createElement(sceneIndex === null ? 'p' : 'button');
        const sceneName = sceneIndex === null ? '' : snapshot.scenes[sceneIndex]?.text.trim().split('\n')[0];
        const fieldLabel = fieldName.startsWith('items.') ? `List item ${Number(fieldName.split('.')[1]) + 1}` : fieldName === 'text' ? 'Title text' : fieldName === 'items' ? 'List items' : fieldName.replaceAll('_', ' ');
        entry.textContent = `${sceneIndex === null ? 'Composition' : `Scene ${sceneIndex + 1}${sceneName ? ` · ${sceneName}` : ''}`} · ${fieldLabel}: ${issue.message}`;
        if (sceneIndex !== null) { entry.type = 'button'; entry.addEventListener('click', () => { selectScene(sceneIndex); showEditorPanel('properties'); $('selected-scene-heading').scrollIntoView({block: 'nearest'}); }); }
        $('scene-issues').append(entry);
      }
      $('preview-profile').textContent = `${result.profile.width} × ${result.profile.height} · ${result.profile.fps} fps`;
      const selectedInvalid = result.preview_issues.some(issue => !issue.field.startsWith('scenes.') || issue.field.startsWith(`scenes.${selectedScene}.`));
      if (!snapshot.scenes[selectedScene] || selectedInvalid) {
        $('preview-state').textContent = 'Resolve this scene’s validation issues to preview. Draft edits are still saved.';
        return;
      }
      const response = await request(`/api/generator/preview?scene=${selectedScene}&frame=${Math.round(Math.min(3600, Math.max(0, Number($('preview-time').value) || 0)) * result.profile.fps)}`, 'POST', snapshot, previewAbort.signal);
      const bitmap = await createImageBitmap(await response.blob());
      if (sequence !== previewSequence) { bitmap.close(); return; }
      const canvas = $('scene-preview');
      canvas.width = bitmap.width; canvas.height = bitmap.height;
      canvas.getContext('2d').drawImage(bitmap, 0, 0); storeSceneThumbnail(selectedScene,bitmap); bitmap.close();
      canvas.hidden = false;
      canvas.setAttribute('aria-label', `Quick preview: ${snapshot.scenes[selectedScene].text || 'Blank title scene'}`);
      $('preview-state').textContent = `Current draft preview · ${snapshot.scenes[selectedScene].duration_seconds} seconds · ${snapshot.scenes[selectedScene].transition?.kind === 'crossfade' && selectedScene + 1 < snapshot.scenes.length ? 'crossfade shown in generated preview' : 'individual scene'} · not on air`;
      // Keep requests sequential: the server admits one draft preview at a time.
      // A new edit cancels this pass; an old response must never label a new scene.
      for (let index=0;index<snapshot.scenes.length;index++) {
        if(sequence!==previewSequence)return;
        if(thumbnails.has(index)||result.preview_issues.some(issue=>!issue.field.startsWith('scenes.')||issue.field.startsWith(`scenes.${index}.`)))continue;
        try {
          const response=await request(`/api/generator/preview?scene=${index}`, 'POST', snapshot, previewAbort.signal);
          const bitmap=await createImageBitmap(await response.blob());
          if(sequence===previewSequence)storeSceneThumbnail(index,bitmap);
          bitmap.close();
        } catch(error) {
          if(error.name==='AbortError'||sequence!==previewSequence)return;
          const caption=$('scene-list').children[index]?.querySelector('span');
          if(caption)caption.textContent+=' · preview unavailable';
        }
      }
    } catch (error) {
      if (error.name !== 'AbortError' && sequence === previewSequence) $('preview-state').textContent = `Preview unavailable: ${error.message}`;
    }
  }
  async function saveDraft(asCopy = false) {
    clearTimeout(saveTimer);
    if (!draft || saving || deleting || (conflict && !asCopy) || (!dirty && !asCopy)) return;
    saving = true;
    const sequence = editSequence;
    const snapshot = structuredClone(draft);
    saveState(asCopy ? 'Saving independent copy…' : 'Saving…');
    try {
      const path = asCopy ? '/api/generator/designs' : `/api/generator/designs/${snapshot.id}`;
      const saved = await (await request(path, asCopy ? 'POST' : 'PUT', snapshot)).json();
      if (saved.id !== draft.id) { stopGeneratedPreview(); $('generation-history').open = false; }
      // Keep edits made while this request was in flight, but advance the CAS version.
      draft.id = saved.id; draft.version = saved.version; draft.updated_at = saved.updated_at;
      conflict = false; dirty = editSequence !== sequence;
      saveState(dirty ? 'Unsaved local changes' : `Saved · version ${draft.version}`);
      history.replaceState(null, '', `/editor?id=${encodeURIComponent(draft.id)}&return=${encodeURIComponent(returnPath)}`);
    window.dispatchEvent(new Event('studio-route-changed'));
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
    if (event.target.id.startsWith('soundtrack-') || event.target.id === 'duration-mode') draft.duration_mode = $('duration-mode').value;
    captureFields();
    if (event.target.id === 'scene-layout' || event.target.id === 'scene-media-kind') renderSelectedScene();
    if (event.target.id === 'scene-image') renderImagePicker();
    if (['soundtrack-asset','soundtrack-mode','duration-mode'].includes(event.target.id)) renderMusicPicker();
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
    $('template-list').replaceChildren();
    for (const template of templates) {
      const row = document.createElement('article'); row.className = 'template-card';
      const title = document.createElement('h3'); title.textContent = template.name;
      const detail = document.createElement('p'); detail.className = 'stage-help';
      detail.textContent = `${template.builtin ? 'Built-in starter' : `Custom · v${template.version}`} · ${template.content.stage} · ${template.content.scenes.length} scenes`;
      const use = document.createElement('button'); use.type = 'button'; use.className = 'preview-button studio-authoring'; use.textContent = 'Use template';
      use.setAttribute('aria-label', `Use template ${template.name}`);
      use.addEventListener('click', () => {
        $('new-stage').value = template.content.stage; renderTemplateChoices();
        $('new-template').value = template.id; $('new-name').value = template.name;
        showCollection('designs'); $('new-name').focus();
        notify('Choose a name and exact style theme, then create an independent design. Your open draft is retained.');
      });
      row.append(title, detail, use);
      if (!template.builtin) {
        const remove = document.createElement('button'); remove.type = 'button'; remove.className = 'preview-button studio-authoring'; remove.textContent = 'Delete template';
        remove.setAttribute('aria-label', `Delete template ${template.name}`);
        remove.addEventListener('click', async () => {
          if (templateSaving || !window.confirm(`Permanently delete template “${template.name}”? Independent designs, generated files and uploaded assets stay.`)) return;
          remove.disabled = true;
          try { await request(`/api/generator/templates/${template.id}`, 'DELETE', {version: template.version}); await loadTemplates(); await loadAssets(); notify('Template deleted. Designs and uploads kept.'); $('templates-refresh').focus(); }
          catch (error) { notify(error.message); remove.disabled = false; }
        });
        row.append(remove);
      }
      $('template-list').append(row);
    }
    if (!templates.length) $('template-list').textContent = 'No templates available. Save content from an open design to start.';
    renderCollectionContext();
  }
  function renderCollectionContext() {
    $('template-source').textContent = draft ? `${draft.name} · draft v${draft.version}${dirty ? ' · local edits included' : ''}. Saving creates an explicit content snapshot.` : 'Open a design first to save or update reusable content.';
    $('save-template').disabled = !draft || templateSaving;
    $('update-template-button').disabled = !draft || !$('update-template').value || templateSaving;
    $('theme-adopt').disabled = !draft || !editingTheme || themeSaving;
    const pinnedTheme = themes.find(theme => theme.id === draft?.theme.id && theme.revision === draft?.theme.revision);
    $('theme-adoption-state').textContent = draft ? `Open design: ${draft.name} · pinned ${pinnedTheme?.name || 'unavailable theme'} revision ${draft.theme.revision}. Adoption changes this draft only; scene overrides stay.` : 'Open a design to adopt a saved theme revision. Unpublished settings are never adopted.';
  }
  async function loadTemplates() {
    $('templates-state').textContent = 'Loading templates…';
    try {
      templates = await (await request('/api/generator/templates')).json();
      renderTemplateChoices();
      $('templates-state').textContent = `${templates.filter(item => item.builtin).length} built-in starters · ${templates.filter(item => !item.builtin).length} custom templates`;
    } catch (error) { $('templates-state').textContent = `Templates could not refresh: ${error.message}. Existing choices and local edits are retained.`; throw error; }
  }
  $('templates-refresh').addEventListener('click', () => loadTemplates().catch(error => notify(error.message)));
  $('open-template-collection').addEventListener('click', () => { showCollection('templates'); $('template-name').focus(); });
  $('new-stage').addEventListener('change', renderTemplateChoices);
  $('update-template').addEventListener('change', () => {
    const template = templates.find(item => item.id === $('update-template').value);
    if (template) $('template-name').value = template.name;
    $('update-template-button').disabled = !draft || !template;
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

  let historySignature = '';
  function renderJobs() {
    const focused = document.activeElement;
    const focusJob = focused?.closest('[data-job-id]')?.dataset.jobId;
    const focusAction = focused?.textContent;
    const focusContainer = focused?.closest('#generation-queue,#generation-jobs')?.id;
    const outstanding = jobs.filter(job => ['queued', 'running', 'cancelling'].includes(job.state));
    const busy = outstanding.length >= 8;
    const active = jobs.find(job => ['running', 'cancelling'].includes(job.state));
    $('generation-status').textContent = !jobsLoaded ? 'Shared queue · loading…' : `${outstanding.length ? `Generating ${outstanding.length} video${outstanding.length === 1 ? '' : 's'}` : 'Generation activity'}${active ? ` · ${active.progress}%` : ''}${jobsCurrent ? '' : ' · unavailable'}`;
    $('generation-queue').replaceChildren();
    if (!outstanding.length) {
      const empty = document.createElement('p'); empty.textContent = jobsLoaded ? 'No outstanding work. Completed and failed attempts are in each design’s history.' : 'Loading generation status…'; $('generation-queue').append(empty);
    }
    for (const job of outstanding) {
      const row = document.createElement('article'); row.className = 'queue-job'; row.dataset.jobId = job.id;
      const label = document.createElement('p'); label.textContent = `${job.design_snapshot.name} · captured draft v${job.design_snapshot.version} · ${job.state}${job.queue_position ? ` · queue #${job.queue_position}` : ` · ${job.progress}%`}`;
      const cancel = document.createElement('button'); cancel.type = 'button'; cancel.className = 'preview-button studio-authoring'; cancel.textContent = job.state === 'cancelling' ? 'Cancelling…' : 'Cancel generation'; cancel.disabled = !jobsCurrent || job.state === 'cancelling';
      cancel.addEventListener('click', async () => { cancel.disabled = true; try { await request(`/api/generator/jobs/${job.id}/cancel`, 'POST', {}); await loadJobs(); } catch (error) { queueError(error.message); cancel.disabled = false; } });
      row.append(label, cancel); $('generation-queue').append(row);
    }
    if (!draft) {
      historySignature = '';
      if (focusContainer === 'generation-queue') $('generation-status').focus({preventScroll: true});
      return;
    }
    const videoSummary = window.mediaPresentation.latestDesignVideo(draft, jobs, dirty);
    $('latest-video-name').textContent = videoSummary ? window.mediaPresentation.renderName(videoSummary.job) : 'No video generated yet';
    $('latest-video-detail').textContent = videoSummary ? `${window.mediaPresentation.capturedMusic(videoSummary.job,assets,window.musicPlaylists?.catalog()||[])} · ${window.mediaPresentation.date(videoSummary.job.created_at)} · ${videoSummary.match}` : 'Generate to review this design with animation and music.';
    $('review-latest-video').hidden = !videoSummary;
    $('review-latest-video').disabled = !jobsCurrent || !mediaRevisions.some(media => media.id === videoSummary?.job.media_revision && media.state === 'ready');
    $('generate-design').disabled = !jobsCurrent || deleting || dirty || saving || conflict || jobRequest || busy;
    $('generation-save-hint').textContent = dirty || saving || conflict
      ? 'Save and resolve conflicts before generating. Your local edits are retained.'
      : `Generate saved draft v${draft.version}. Later edits leave the captured revision unchanged.`;
    const stagePhase = stageStatus.stage === 'ENDING' && stageStatus.ending?.draining ? 'ENDING · finishing destination writes' : stageStatus.stage === 'OFF' && stageStatus.ending?.completed ? 'Ending completed · destination results are in live control' : stageStatus.stage || 'Status unavailable';
    $('generation-stage-status').textContent = `${stagePhase}${stageStatus.mode === 'preview_only' ? ' · PREVIEW ONLY — NOT BROADCASTING' : stageStatus.mode === 'real' ? ' · real session' : ''}. Generation does not change stage, delivery, or completion.`;
    const nextHistory = JSON.stringify([jobs, draft, dirty, saving, conflict, deleting, jobRequest, jobsCurrent, stageSelections, stageStatus.stage, stageStatus.source, stageStatus.mode, stageStatus.media, stageStatus.return_stage, stageStatus.return_media, stageStatus.ending, mediaRevisions]);
    if(namingJob)return;
    if (nextHistory === historySignature) return;
    historySignature = nextHistory;
    const relevant = jobs.filter(job => job.design_snapshot.id === draft.id).sort((a, b) => b.sequence - a.sequence);
    const latest = relevant[0];
    lastReadyJob = relevant.find(job => job.state === 'ready') || null;
    const readyMedia = mediaRevisions.find(media => media.id === lastReadyJob?.media_revision);
    const shortRevision = revision => window.mediaPresentation.label(revision, jobs, mediaRevisions);
    const selectedRevision = stageSelections[draft.stage];
    const activeRevision = stageStatus.source === 'file' ? stageStatus.media?.revision : null;
    const summary = [
      ['Draft', `v${draft.version}${dirty ? ' · unsaved local edits' : saving ? ' · saving' : ' · saved'}${conflict ? ' · conflict' : ''}`],
      ['Latest attempt', latest ? `${latest.state} · captured draft v${latest.design_snapshot.version}${latest.error ? ` · ${latest.error}` : ''}` : jobsLoaded ? 'No attempts yet' : 'Loading…'],
      ['Last ready output', lastReadyJob ? `Captured draft v${lastReadyJob.design_snapshot.version} · ${shortRevision(lastReadyJob.media_revision)} · ${readyMedia?.state || 'unavailable'}` : jobsLoaded ? 'No ready output yet' : 'Loading…'],
      [`Selected for ${draft.stage.toUpperCase()}`, `${shortRevision(selectedRevision)}${selectedRevision && !relevant.some(job => job.media_revision === selectedRevision) ? ' · from other media' : ''}`],
      ['Active revision', activeRevision ? `${shortRevision(activeRevision)} · ${stageStatus.mode === 'preview_only' ? 'rehearsal' : 'on-air source'}${!relevant.some(job => job.media_revision === activeRevision) ? ' · from other media' : ''}` : `None · ${stageStatus.source || 'status unavailable'}`]
    ];
    $('generation-summary').replaceChildren();
    for (const [label, value] of summary) {
      const term = document.createElement('dt'); term.textContent = label;
      const description = document.createElement('dd'); description.textContent = value;
      $('generation-summary').append(term, description);
    }
    $('generation-summary').classList.toggle('stale', !jobsCurrent);
    $('preview-last-ready').hidden = !lastReadyJob;
    $('preview-last-ready').disabled = !jobsCurrent || readyMedia?.state !== 'ready';
    $('generation-history-heading').textContent = `Older videos · ${relevant.filter(job=>job!==lastReadyJob&&job!==latest).length}`;
    $('generation-history').hidden=!relevant.some(job=>job!==lastReadyJob&&job!==latest);
    $('generation-jobs').replaceChildren(); $('latest-generation').replaceChildren();
    if (!relevant.length) {
      const empty = document.createElement('p'); empty.className = 'stage-help'; empty.textContent = jobsLoaded ? 'No generated results for this design yet. Generate its saved draft on desktop, then review the exact prepared output here.' : 'Loading design generation history…'; $('generation-jobs').append(empty);
    }
    for (const job of relevant) {
      const row = document.createElement('article'); row.className = 'generator-job'; row.dataset.jobId = job.id; row.dataset.state = job.state;
      const title = document.createElement('strong'); title.textContent = `${window.mediaPresentation.renderName(job)} · ${window.mediaPresentation.date(job.created_at)} · ${job.state}${job.queue_position ? ` #${job.queue_position} in queue` : ''} · captured draft v${job.design_snapshot.version} · ${job.duration.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')} seconds`;
      const identity = document.createElement('p'); identity.textContent = `Design revision ${job.design_revision.slice(0, 12)} · theme ${job.design_snapshot.theme.id} revision ${job.design_snapshot.theme.revision} · ${job.profile.width} × ${job.profile.height} · ${job.profile.fps} fps${job.retry_of ? ' · retry of captured inputs' : ''}`;
      const technical = document.createElement('details'); technical.className = 'render-technical';
      const technicalHeading = document.createElement('summary'); technicalHeading.textContent = 'Technical details';
      technical.append(technicalHeading, identity);
      const changes = document.createElement('p'); changes.textContent = window.mediaPresentation.changes(job, relevant[relevant.indexOf(job) + 1]);
      row.append(title, changes, technical);
      if (job.visual_revision) { const reuse = document.createElement('p'); reuse.textContent = job.reused_visual ? 'Music prepared · encoded visual reused' : 'Visual and music prepared'; row.append(reuse); }
      if (job.mix_gain != null) {
        const gain = document.createElement('p');
        gain.textContent = `Whole-mix gain: ${(job.mix_gain * 100).toFixed(2)}%${job.mix_gain < 1 ? ' · fixed peak protection; relative levels preserved' : ' · requested levels retained'}`;
        technical.append(gain);
      }
      if (job.message) { const message = document.createElement('p'); message.textContent = job.message; row.append(message); }
      if (['failed', 'interrupted', 'cancelled'].includes(job.state)) {
        const retry = document.createElement('button'); retry.type = 'button'; retry.className = 'preview-button studio-authoring'; retry.textContent = 'Retry captured revision'; retry.disabled = busy;
        retry.addEventListener('click', async () => { retry.disabled = true; try { await request(`/api/generator/jobs/${job.id}/retry`, 'POST', {}); await loadJobs(); } catch (error) { generationError(error.message); retry.disabled = false; } }); row.append(retry);
      }
      if (job.error) { const error = document.createElement('p'); error.textContent = job.error; row.append(error); }
      if (['queued', 'running', 'cancelling'].includes(job.state)) {
        const progress = document.createElement('progress'); progress.max = 100; progress.value = job.progress; progress.setAttribute('aria-label', 'Generation progress'); row.append(progress);
        const cancel = document.createElement('button'); cancel.type = 'button'; cancel.className = 'preview-button studio-authoring'; cancel.textContent = job.state === 'cancelling' ? 'Cancelling…' : 'Cancel generation'; cancel.disabled = job.state === 'cancelling';
        cancel.addEventListener('click', async () => { cancel.disabled = true; try { await request(`/api/generator/jobs/${job.id}/cancel`, 'POST', {}); await loadJobs(); } catch (error) { generationError(error.message); cancel.disabled = false; } }); row.append(cancel);
      }
      if (job.state === 'ready') {
        const detail = document.createElement('p');
        const media = mediaRevisions.find(item => item.id === job.media_revision);
        const selected = ['prestream', 'ending', 'brb'].filter(kind => stageSelections[kind] === job.media_revision).map(kind => kind.toUpperCase());
        const onAir = stageStatus.source === 'file' && stageStatus.media?.revision === job.media_revision;
        const returning = stageStatus.return_media?.revision === job.media_revision;
        const newer = job.design_snapshot.id === draft.id && job.design_snapshot.version < draft.version;
        detail.textContent = ` ${media?.state || 'unavailable'} · ${selected.length ? `selected for next ${selected.join(' / ')}` : 'not selected for a stage'} · ${onAir ? stageStatus.mode === 'preview_only' ? 'playing in rehearsal' : 'on-air source' : 'not the on-air source'}${returning ? ` · retained for return to ${stageStatus.return_stage}` : ''}${newer ? ' · newer saved draft exists' : ''}${job.design_snapshot.id === draft.id && dirty ? ' · unsaved local edits' : ''}${media?.error ? ` · ${media.error}` : ''}`;
        const preview = document.createElement('button'); preview.type = 'button'; preview.className = 'preview-button'; preview.textContent = 'Preview video'; preview.disabled = media?.state !== 'ready'; preview.addEventListener('click', () => previewGenerated(job)); row.append(detail, preview);
        const download = document.createElement('a'); download.href = `/api/library/revisions/${encodeURIComponent(job.media_revision)}/preview`; download.download = 'presentation.mp4'; download.textContent = 'Download one pass'; download.className = 'preview-button'; row.append(download);
        if (media?.state === 'ready') { const thumbnail = document.createElement('video'); thumbnail.className = 'render-thumbnail'; thumbnail.muted = true; thumbnail.playsInline = true; thumbnail.preload = 'metadata'; thumbnail.src = `/api/library/revisions/${encodeURIComponent(job.media_revision)}/preview#t=0.1`; thumbnail.setAttribute('aria-label', window.mediaPresentation.renderName(job)); row.prepend(thumbnail); }
        const mediaID = document.createElement('code'); mediaID.textContent = job.media_revision; technical.append(mediaID);
        const kind = job.design_snapshot.stage, stageName = kind.toUpperCase();
        if (media?.state === 'ready' && ['prestream', 'ending'].includes(kind)) {
          const actions = document.createElement('div'); actions.className = 'generator-actions';
          const addLink = (action, text) => {
            const link = document.createElement('a'); link.className = 'preview-button';  link.textContent = text;
            const params = {media_revision: job.media_revision, media_stage: kind, media_action: action};
            if (action !== 'select_next') Object.assign(params, {server_id: stageStatus.server_id, context: stageStatus.context});
            link.href = action === 'select_next' ? `/setup?stage=${kind}&render=${encodeURIComponent(job.media_revision)}` : `/?${new URLSearchParams(params)}#media-intent`; actions.append(link);
          };
          addLink('select_next', `Use for ${kind === 'prestream' ? 'Starting soon' : 'Ending'}`);
          if (stageStatus.stage === stageName && stageStatus.media?.revision && stageStatus.media.revision !== job.media_revision && (kind !== 'ending' || (!stageStatus.ending?.draining && !stageStatus.ending?.completed))) addLink('replace_now', `Review Replace ${stageName} now`);
          if (kind === 'prestream' && stageStatus.return_stage === 'PRESTREAM' && stageStatus.return_media?.revision && stageStatus.return_media.revision !== job.media_revision) addLink('replace_on_return', 'Review Replace PRESTREAM on return');
          row.append(actions);
        }
      }
      const rename = document.createElement('button'); rename.type='button';rename.className='preview-button studio-authoring';rename.textContent='Name this video';
      rename.addEventListener('click',()=>{
        namingJob=job.id;
        const form=document.createElement('form'),input=document.createElement('input'),label=document.createElement('label'),save=document.createElement('button');
        label.textContent='Video name';input.value=job.name||'';input.required=true;input.maxLength=180;label.append(input);save.textContent='Save name';save.type='submit';save.className='preview-button';const cancel=document.createElement('button');cancel.type='button';cancel.className='preview-button';cancel.textContent='Cancel';cancel.onclick=()=>{namingJob='';historySignature='';renderJobs();};form.append(label,save,cancel);rename.replaceWith(form);input.focus();
        form.addEventListener('submit',async event=>{event.preventDefault();try{await request(`/api/generator/jobs/${job.id}/name`,'PUT',{name:input.value});namingJob='';historySignature='';await loadJobs();}catch(error){generationError(error.message);}});
      });
      const remove = document.createElement('button'); remove.type = 'button'; remove.className = 'preview-button studio-authoring'; remove.textContent = 'Delete result';
      remove.disabled = ['running', 'cancelling'].includes(job.state);
      remove.addEventListener('click', async () => {
        if (!window.confirm(`Permanently delete this result for “${job.design_snapshot.name}” and its generated file? The editable design and uploaded assets stay. Output shared with another result is kept. Selected or playing output blocks deletion.`)) return;
        remove.disabled = true;
        try {
          const result = await (await request(`/api/generator/jobs/${job.id}`, 'DELETE', {})).json();
          if (reviewedRevision === job.media_revision) stopGeneratedPreview();
          await loadJobs(); await loadAssets();
          generationError(''); notify(result.retained_shared_media.length ? 'Result deleted. Shared output remains available to other results.' : 'Result and generated file deleted. Design and uploads kept.');
        } catch (error) { generationError(error.message); remove.disabled = false; }
      });
      const more=document.createElement('details'),summary=document.createElement('summary');more.className='row-maintenance studio-authoring';summary.textContent='More';more.append(summary,rename,remove);row.append(more);row.prepend(cleanupCheckbox(job,selectedVideos,'jobs'));
      (job === lastReadyJob || job === latest ? $('latest-generation') : $('generation-jobs')).append(row);
    }
    cleanupControl('video-cleanup',selectedVideos,'jobs',loadJobs);
    if (focusJob && focusContainer) {
      const row = [...$(focusContainer).querySelectorAll('[data-job-id]')].find(item => item.dataset.jobId === focusJob);
      const nextFocus = [...(row?.querySelectorAll('button,a') || [])].find(button => button.textContent === focusAction);
      (nextFocus || $(focusContainer === 'generation-queue' ? 'generation-status' : 'generation-history-heading')).focus({preventScroll: true});
    }
  }
  $('review-latest-video').addEventListener('click', () => { showPreviewPanel('results'); $('preview-last-ready').click(); });
  $('preview-last-ready').addEventListener('click', () => { if (lastReadyJob && jobsCurrent && mediaRevisions.some(media => media.id === lastReadyJob.media_revision && media.state === 'ready')) previewGenerated(lastReadyJob); });
  function queueError(message) { $('generation-queue-error').textContent = message; $('generation-queue-error').hidden = !message; }
  function generationError(message) { $('generation-error').textContent = message; $('generation-error').hidden = !message; }
  async function loadJobs() {
    if (jobsFetching) return;
    jobsFetching = true;
    try {
      const [nextJobs, selections, stage, catalog] = await Promise.all(['/api/generator/jobs', '/api/stage-media', '/api/stage', '/api/library'].map(async path => (await request(path)).json()));
      jobs = nextJobs; jobsLoaded = true; jobsCurrent = true; stageSelections = selections; stageStatus = stage; mediaRevisions = catalog.revisions || [];
      document.querySelectorAll('[data-editor-stage]').forEach(link => { const job = window.mediaPresentation.jobFor(stageSelections[link.dataset.editorStage], jobs); link.href = job ? `/studio?id=${encodeURIComponent(job.design_snapshot.id)}` : `/studio?collection=designs&stage=${link.dataset.editorStage}`; });
      queueError('');
      if ($('generation-error').textContent.startsWith('Generation status unavailable:')) generationError('');
      renderJobs();
    } catch (error) { jobsCurrent = false; queueError(`Generation status unavailable: ${error.message}. Last received data is retained.`); generationError(`Generation status unavailable: ${error.message}`); renderJobs(); if (!jobsLoaded) $('generation-status').textContent = 'Shared queue · status unavailable'; }
    finally { jobsFetching = false; }
  }
  function previewGenerated(job) {
    reviewedRevision = job.media_revision;
    $('generated-preview').hidden = false;
    const passes = job.design_snapshot.duration_mode !== 'music' && job.design_snapshot.stage === 'prestream' && job.exact_timing ? 2 : 1;
    $('generated-identity').textContent = `${passes === 2 ? `Two passes · ${job.duration}s per complete cycle. ` : job.design_snapshot.duration_mode === 'music' ? 'One music pass. On air, music and video keep looping independently until you stop or change stage. ' : 'One pass. '}${window.mediaPresentation.renderName(job)} · ${window.mediaPresentation.date(job.created_at)}. Preview does not change the broadcast.`;
    const video = $('generated-video');
    video.src = `/api/library/revisions/${encodeURIComponent(reviewedRevision)}/preview${passes === 2 ? "?passes=2" : ""}`;
    $('generated-playback-status').textContent = 'Loading exact prepared output…';
    video.play().catch(() => { $('generated-playback-status').textContent = 'Press Play to review the exact prepared output.'; });
    $('generated-preview').scrollIntoView({block: 'nearest'});
    video.focus({preventScroll: true});
  }
  $('generated-video').addEventListener('playing', () => { $('generated-playback-status').textContent = `Playing exact revision ${window.mediaPresentation.label(reviewedRevision, jobs, mediaRevisions)}.`; });
  $('generated-video').addEventListener('ended', () => { $('generated-playback-status').textContent = `Finished exact revision ${window.mediaPresentation.label(reviewedRevision, jobs, mediaRevisions)}. Playback and selected broadcast media are unchanged.`; });
  $('generated-video').addEventListener('pause', () => { if (reviewedRevision && !$('generated-video').ended) $('generated-playback-status').textContent = `Preview paused · exact revision ${window.mediaPresentation.label(reviewedRevision, jobs, mediaRevisions)}. Broadcast playback is unchanged.`; });
  $('generated-video').addEventListener('error', () => { $('generated-playback-status').textContent = 'Exact output preview unavailable. Check the active profile and try Preview exact revision again.'; });
  $('generate-design').addEventListener('click', async () => {
    if (!draft || deleting || dirty || saving || conflict || jobRequest) return;
    showPreviewPanel('results'); jobRequest = true; generationError(''); renderJobs();
    try { await request('/api/generator/jobs', 'POST', {design_id: draft.id, version: draft.version}); await loadJobs(); }
    catch (error) { generationError(error.message); }
    finally { jobRequest = false; renderJobs(); }
  });
  setInterval(loadJobs, 2000);


  function assetError(message) { $('asset-error').textContent = message; $('asset-error').hidden = !message; $('music-error').textContent = message; $('music-error').hidden = !message; }
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
    const opened = new Set([...$('asset-list').querySelectorAll('details[open]')].map(card => card.dataset.assetId));
    $('asset-list').replaceChildren();
    const musicOpened = new Set([...$('music-list').querySelectorAll('details[open]')].map(card => card.dataset.assetId));
    $('music-list').replaceChildren();
    $('music-list-state').textContent = `${assets.filter(asset => asset.kind === 'audio').length} uploaded tracks`;
    $('asset-list-state').textContent = assets.length ? `${assets.filter(asset => asset.kind !== 'audio').length} images / videos · music is in the Music collection` : 'No scene assets uploaded. Add an image, video or music file on desktop to use it in your designs.';
    for (const asset of assets) {
      const card = document.createElement('details'); card.className = 'generator-asset'; card.dataset.assetId = asset.id; card.open = (asset.kind === 'audio' ? musicOpened : opened).has(asset.id);
      const summary = document.createElement('summary'); summary.textContent = `${asset.name} · ${asset.kind === 'audio' ? `${Math.round(asset.revisions.find(item => item.revision === asset.revision)?.duration_seconds || 0)} seconds` : asset.kind} · ${asset.uses.length ? `Used by ${[...new Set(asset.uses.map(use => use.name))].join(', ')}` : 'Not used'}`;
      if (asset.kind === 'audio') {
        summary.replaceChildren(); summary.className = 'music-row-summary';
        for (const text of [asset.name, `${Math.round(asset.revisions.find(item => item.revision === asset.revision)?.duration_seconds || 0)} seconds`, asset.uses.length ? `Used in ${asset.uses.length} saved ${asset.uses.length === 1 ? 'item' : 'items'}` : 'Not used']) { const cell = document.createElement('span'); cell.textContent = text; summary.append(cell); }
      }
      const img = document.createElement(asset.kind === 'video' ? 'video' : asset.kind === 'audio' ? 'audio' : 'img'); if (asset.kind !== 'image') { img.controls = true; img.muted = asset.kind === 'video'; img.playsInline = true; img.preload = 'none'; } else { img.alt = asset.name; img.loading = 'lazy'; } img.src = `/api/generator/assets/${asset.id}/revisions/${asset.revision}`;
      img.addEventListener('error', () => assetError(`Preview unavailable for ${asset.name}. Refresh assets to check its exact revision.`));
      const revisionLabel = document.createElement('label'); revisionLabel.textContent = 'Preview exact revision';
      const revisionPicker = document.createElement('select');
      revisionPicker.replaceChildren(...asset.revisions.map(revision => new Option(`Revision ${revision.revision}${revision.revision === asset.revision ? ' · latest' : ''}`, revision.revision)));
      revisionPicker.value = asset.revision;
      revisionPicker.addEventListener('change', () => { img.src = `/api/generator/assets/${asset.id}/revisions/${revisionPicker.value}`; });
      revisionLabel.append(revisionPicker);
      const referenceStatus = document.createElement('p'); referenceStatus.className = 'stage-help'; referenceStatus.textContent = asset.uses.length ? 'Protected while referenced by saved content or generation history.' : 'No saved references. Eligible for separate deletion.';
      const list = document.createElement('ul');
      for (const use of asset.uses) { const item = document.createElement('li'); item.textContent = `${use.kind}: ${use.name} · revision ${use.revision}`; list.append(item); }
      const remove = document.createElement('button'); remove.type = 'button'; remove.className = 'preview-button studio-authoring'; remove.textContent = `Delete unused ${asset.kind}`; remove.disabled = asset.uses.length > 0;
      remove.addEventListener('click', async () => {
        if (!window.confirm(`Delete unused asset “${asset.name}” and all its revisions?`)) return;
        remove.disabled = true;
        try { await request(`/api/generator/assets/${asset.id}`, 'DELETE', {}); assetError(''); await loadAssets(); }
        catch (error) { assetError(error.message); await loadAssets(); }
      });
      card.append(summary, revisionLabel, img, referenceStatus, list, remove); $(asset.kind === 'audio' ? 'music-list' : 'asset-list').append(card);
    }
    renderImagePicker(); renderVideoPicker(); renderMusicPicker(); renderThemeAssets();
  }
  $('music-upload-open').addEventListener('click', () => window.openAssetUpload('audio',$('music-upload-open')));
  window.assetUploads={busy:()=>assetUploading};
  async function loadAssets() {
    if (assetsFetching) return;
    assetsFetching = true;
    try { assets = await (await request('/api/generator/assets')).json(); renderAssets(); await window.musicPlaylists?.refresh(); }
    catch (error) { assetError(`Asset library unavailable: ${error.message}`); $('asset-list-state').textContent = 'Asset status unavailable. Previously loaded references may be stale. Refresh to retry.'; $('asset-list').querySelectorAll('button').forEach(button => { button.disabled = true; }); }
    finally { assetsFetching = false; }
  }
  function renderMusicPicker() {
    if (!draft) return;
    const track = draft.soundtrack;
    const playlists = window.musicPlaylists?.catalog() || [];
    const value = window.musicSelection.value(track);
    const options = window.musicSelection.choices(assets, playlists, track).map(choice => new Option(choice.label,choice.value));
    $('duration-mode').value = draft.duration_mode || 'fixed';
    $('duration-mode-help').textContent = draft.duration_mode === 'music' ? 'Download: one music pass, rounded up to a video frame (maximum one hour). On air: video and music repeat independently until Stop or a stage change. Music-only preparations reuse the encoded visual.' : 'Download length follows the scene sequence. Existing fixed ending behavior is preserved.';
    $('soundtrack-mode').disabled = draft.duration_mode === 'music';
    $('soundtrack-asset').replaceChildren(...options); $('soundtrack-asset').value = value;
    $('soundtrack-settings').hidden = !track;
    $('soundtrack-mode').value = track?.mode || 'end'; $('soundtrack-volume').value = track?.volume_percent ?? 100;
    $('soundtrack-fade-in').value = track?.fade_in_seconds || 0; $('soundtrack-fade-out').value = track?.fade_out_seconds || 0;
    const asset = assets.find(asset => asset.id === track?.asset?.id);
    const meta = asset?.revisions.find(revision => revision.revision === track.asset?.revision);
    $('soundtrack-info').textContent = meta ? `Track: ${meta.duration_seconds.toFixed(3)} seconds · pinned revision ${track.asset?.revision}` : 'Select an available music revision.';
    const playlist = playlists.find(p => p.id === track?.playlist?.id);
    $('adopt-music-revision').hidden = playlist ? playlist.revision === track.playlist.revision : !asset || asset.revision === track?.asset?.revision;
    if (track?.playlist) {
      const ref = {...track.playlist};
      $('soundtrack-info').textContent = `${playlist?.name || 'Saved playlist'} · pinned version ${ref.revision}. Loading duration…`;
      request(`/api/music/playlists/${ref.id}/revisions/${ref.revision}`).then(response => response.json()).then(saved => {
        if (draft?.soundtrack?.playlist?.id === ref.id && draft.soundtrack.playlist.revision === ref.revision) $('soundtrack-info').textContent = `${saved.name} · ${saved.duration.toFixed(3)} seconds · pinned version ${ref.revision}. Playlist edits do not change this selection.`;
      }).catch(error => { if (draft?.soundtrack?.playlist?.id === ref.id && draft.soundtrack.playlist.revision === ref.revision) $('soundtrack-info').textContent = `Saved playlist unavailable: ${error.message}`; });
    }
  }
  $('adopt-music-revision').addEventListener('click', () => {
    const track = draft?.soundtrack, asset = assets.find(asset => asset.id === track?.asset?.id);
    const playlist = window.musicPlaylists?.catalog().find(p => p.id === track?.playlist?.id);
    if (playlist) track.playlist.revision = playlist.revision; else if (asset) track.asset.revision = asset.revision; else return; draft.duration_mode = $('duration-mode').value; renderMusicPicker(); changed();
  });
  window.addEventListener('music-playlists-changed', renderMusicPicker);
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
  $('asset-upload').addEventListener('submit', event => {
    event.preventDefault(); if (assetUploading || window.matchMedia('(max-width: 600px)').matches) return;
    const file = $('asset-file').files[0]; if (!file) return;
    const kind = $('asset-kind').value;
    if (file.size > (kind === 'video' ? 512 : kind === 'audio' ? 32 : 10) * 1048576) { assetError(`Choose a ${kind} up to ${kind === 'video' ? 512 : kind === 'audio' ? 32 : 10} MiB.`); return; }
    const target = assets.find(asset => asset.id === $('asset-upload-target').value);
    const data = new FormData(); data.append('file', file);
    assetUploading = true;
    $('asset-upload').querySelectorAll('input, select, button').forEach(control => { control.disabled = control.id !== 'asset-upload-cancel'; });
    $('asset-upload-cancel').hidden = false; assetError(''); $('asset-status').textContent = 'Uploading asset…';
    const path = target ? `/api/generator/assets/${target.id}/revisions?version=${target.revision}&kind=${kind}` : `/api/generator/assets?kind=${kind}`;
    const xhr = new XMLHttpRequest(); assetUploadRequest = xhr;
    xhr.open('POST', new URL(path, location.origin)); xhr.setRequestHeader('X-Restreamer-Control', '1'); xhr.timeout = kind === 'video' ? 960000 : kind === 'audio' ? 360000 : 120000;
    xhr.upload.onprogress = event => { $('asset-status').textContent = event.lengthComputable ? `Uploading asset · ${Math.floor(event.loaded / event.total * 100)}%` : 'Uploading asset…'; };
    xhr.upload.onload = () => { $('asset-status').textContent = 'Upload transferred. Validating and preparing the exact revision; waiting for current media work if necessary…'; };
    xhr.onload = () => {
      if (xhr.status === 201) {
        try {
          const asset = JSON.parse(xhr.responseText); $('asset-file').value = '';
          $('asset-status').textContent = `${asset.name} ready as revision ${asset.revision}. Choose it in a scene or soundtrack to use it.`;
          if (asset.kind === 'audio') showCollection('music');
        } catch (_) { assetError('Upload response could not be read. Refresh assets to check whether it was saved.'); }
      } else { assetError(xhr.responseText.trim() || 'Upload failed.'); $('asset-status').textContent = 'Asset was not acknowledged as saved. Previous revisions remain unchanged.'; }
    };
    xhr.onerror = () => assetError('Upload connection failed. Refresh assets before retrying; the server may have saved the revision.');
    xhr.ontimeout = () => assetError('Preparation timed out. Refresh assets before retrying.');
    xhr.onabort = () => { $('asset-status').textContent = 'Upload cancelled. Refresh assets to check whether the server had already saved it.'; };
    xhr.onloadend = () => {
      assetUploading = false; assetUploadRequest = undefined; $('asset-upload-cancel').hidden = true;
      $('asset-upload').querySelectorAll('input, select, button').forEach(control => { control.disabled = false; }); loadAssets();
    };
    xhr.send(data);
  });
  $('asset-upload-cancel').addEventListener('click', () => assetUploadRequest?.abort());
  $('adopt-image-revision').addEventListener('click', () => {
    const scene = draft?.scenes[selectedScene]; const asset = assets.find(asset => asset.id === scene?.image?.id);
    if (!asset) return; scene.image = {id: asset.id, revision: asset.revision}; renderImagePicker(); changed();
  });
  $('asset-refresh').addEventListener('click', loadAssets);
  loadAssets();


  const themeFields = [
    ['font', 'Default font', ['go-sans', 'go-mono', 'arcade-pixel']], ['font_size', 'Title/default font size at 1080p', 24, 120], ['list_font_size', 'List font size at 1080p (blank inherits default)', 'optional-size'],
    ['background_color', 'Background color', 'color'], ['text_color', 'Text color', 'color'], ['text_edge_color', 'Pixel text edge color (blank for warm default)', 'text'], ['accent_color', 'Accent color', 'color'],
    ['artwork', 'Built-in background artwork', ['', 'arcade-after-hours', 'neon-night', 'maker-workshop', 'maker-halloween', 'maker-christmas', 'pacman-maze']], ['background', 'Background image revision (clear built-in artwork first)', 'asset'], ['logo', 'Logo image revision', 'asset'],
    ['logo_position', 'Logo corner', ['top-left', 'top-right', 'bottom-left', 'bottom-right']], ['logo_height_percent', 'Logo height (%)', 2, 8],
    ['content_offset_y_percent', 'Content vertical offset (%)', -40, 40], ['width_percent', 'Default content width (%)', 30, 90], ['height_percent', 'Default content height (%)', 12, 90],
    ['line_spacing_percent', 'Line spacing (%)', 100, 180], ['list_spacing_percent', 'List item spacing (%)', 0, 100],
    ['border_style', 'Border', ['none', 'line', 'pixel']], ['border_width', 'Border width at 1080p', 1, 12],
    ['effect', 'Decorative effect', ['none', 'pixel-trail', 'arcade-palette', 'neon-palette', 'workshop-motion', 'pacman-motion']], ['effect_speed', 'Pixel trail steps per second', 1, 12]
  ];
  for (const [key, title, kind, maximum] of themeFields) {
    const label = document.createElement('label'); label.textContent = title;
    const input = document.createElement(Array.isArray(kind) || kind === 'asset' ? 'select' : 'input'); input.id = `theme-${key}`;
    if (Array.isArray(kind)) input.replaceChildren(...kind.map(value => new Option(value ? value.replaceAll('-', ' ') : 'None', value)));
    else if (typeof kind === 'number') { input.type = 'number'; input.min = kind; input.max = maximum; input.step = '1'; input.required = true; }
    else if (kind === 'optional-size') { input.type = 'number'; input.min = 24; input.max = 120; input.step = '1'; }
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
    if (value && !themes.some(theme => themeKey(theme) === value)) $('design-theme').add(new Option(pinnedThemes.has(value) ? `${pinnedThemes.get(value).name} · revision ${draft.theme.revision} (retired)` : 'Loading pinned theme…', value));
    $('design-theme').value = value;
    const selected = themes.find(theme => themeKey(theme) === value) || pinnedThemes.get(value);
    const latest = themes.filter(theme => theme.id === draft?.theme.id).sort((a,b) => b.revision-a.revision)[0];
    $('apply-theme-update').hidden = !latest || latest.revision <= draft.theme.revision;
    $('design-theme-status').textContent = selected ? `${selected.name} · revision ${selected.revision}. Theme edits leave this draft pinned until you apply an update.` : 'Choose an available exact theme revision.';
    $('preview-time-control').hidden = !['pixel-trail', 'arcade-palette', 'neon-palette', 'workshop-motion', 'pacman-motion'].includes(selected?.style.effect);
    if (selected) {
      $('scene-font').options[0].textContent = `Theme default · ${selected.style.font === 'arcade-pixel' ? 'Arcade Pixel · uppercase' : selected.style.font === 'go-mono' ? 'Go Mono' : 'Go Sans'}`;
      const defaultSize = draft.scenes[selectedScene]?.layout === 'list' && selected.style.list_font_size ? selected.style.list_font_size : selected.style.font_size;
      $('scene-size').placeholder = `Theme default · ${defaultSize}`;
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
      $(`theme-${key}`).value = value && typeof value === 'object' ? themeKey(value) : value ?? (key === 'content_offset_y_percent' ? 0 : '');
    }
    scheduleThemePreview();
    $('theme-publish').disabled = theme.builtin;
    $('theme-delete').disabled = theme.builtin;
    renderCollectionContext();
    $('theme-status').textContent = `${theme.name} · revision ${theme.revision}${theme.builtin ? '. Duplicate to create an editable variant.' : '. Publish changes explicitly; existing designs keep their pinned revision.'}`;
  }
  function localTheme() {
    const theme = structuredClone(editingTheme); theme.name = $('theme-name').value;
    for (const [key, , kind] of themeFields) {
      const value = $(`theme-${key}`).value;
      if (['width_percent','height_percent'].includes(key)) theme.style.content_region[key] = Number(value);
      else if (kind === 'asset') { if (value) theme.style[key] = themeRef(value); else delete theme.style[key]; }
      else if (kind === 'optional-size') { if (value) theme.style[key] = Number(value); else delete theme.style[key]; }
      else theme.style[key] = typeof kind === 'number' ? Number(value) : value;
    }
    return theme;
  }
  async function loadPinnedTheme(ref) {
    const key = themeKey(ref);
    if (themes.some(theme => themeKey(theme) === key) || pinnedThemes.has(key)) return;
    try {
      const theme = await (await request(`/api/generator/themes/${encodeURIComponent(ref.id)}/revisions/${ref.revision}`)).json();
      pinnedThemes.set(key, theme);
      if (draft && themeKey(draft.theme) === key) renderDesignTheme();
    } catch (error) { if (draft && themeKey(draft.theme) === key) notify(`Pinned theme unavailable: ${error.message}`); }
  }
  function scheduleThemePreview() {
    const sequence = ++themePreviewSequence;
    clearTimeout(themePreviewTimer); themePreviewAbort?.abort();
    if (themePreviewURL) URL.revokeObjectURL(themePreviewURL);
    themePreviewURL = ''; $('theme-download').removeAttribute('href'); $('theme-download').setAttribute('aria-disabled', 'true'); $('theme-download').tabIndex = -1;
    const canvas = $('theme-preview-canvas'); canvas.getContext('2d').clearRect(0, 0, canvas.width, canvas.height);
    if (!editingTheme || $('studio-themes').hidden) return;
    $('theme-preview-status').textContent = 'Rendering preview…';
    $('theme-preview-help').textContent = themeView === 'border'
      ? '1920 × 1080 PNG with a transparent center. Add as an Image source in OBS. Each artwork has one fixed frame; colors and logos apply to scene samples only. Checkerboard is preview-only.'
      : '1920 × 1080 still sample with example text and your current settings. Animated backgrounds remain animated in generated shows and BRB media.';
    themePreviewTimer = setTimeout(() => renderThemePreview(sequence), 300);
  }
  async function renderThemePreview(sequence) {
    const controller = new AbortController(); themePreviewAbort = controller;
    const timer = setTimeout(() => controller.abort(), 15000);
    try {
      const theme = localTheme();
      const response = await request('/api/generator/themes/preview', 'POST', {style: theme.style, view: themeView}, controller.signal);
      const blob = await response.blob();
      const bitmap = await createImageBitmap(blob);
      if (sequence !== themePreviewSequence) { bitmap.close(); return; }
      const canvas = $('theme-preview-canvas');
      canvas.getContext('2d').drawImage(bitmap, 0, 0); bitmap.close();
      themePreviewURL = URL.createObjectURL(blob);
      themeDownloadName = `${theme.id}-r${theme.revision}-${themeView}.png`;
      $('theme-download').href = themePreviewURL; $('theme-download').download = themeDownloadName;
      $('theme-download').setAttribute('aria-disabled', 'false'); $('theme-download').tabIndex = 0;
      $('theme-preview-status').textContent = `${theme.name} · ${themeView === 'border' ? 'OBS frame · alpha transparency' : 'scene sample'}${themeDirty ? ' · unpublished settings' : ''}`;
    } catch (error) {
      if (sequence === themePreviewSequence) $('theme-preview-status').textContent = `Preview unavailable: ${error.name === 'AbortError' ? 'render timed out; refresh to retry.' : error.message}`;
    } finally { clearTimeout(timer); }
  }
  $('theme-views').addEventListener('click', event => {
    const button = event.target.closest('[data-theme-view]'); if (!button) return;
    themeView = button.dataset.themeView;
    document.querySelectorAll('[data-theme-view]').forEach(item => item.setAttribute('aria-pressed', String(item === button)));
    scheduleThemePreview();
  });
  $('theme-preview-retry').addEventListener('click', scheduleThemePreview);
  async function loadThemes() {
    $('themes-state').textContent = 'Loading theme revisions…';
    try { themes = await (await request('/api/generator/themes')).json(); }
    catch (error) { $('themes-state').textContent = `Themes could not refresh: ${error.message}. Local settings are retained.`; throw error; }
    $('themes-state').textContent = `${themes.length} immutable revisions · ${new Set(themes.map(theme => theme.id)).size} themes`;
    const selection = $('new-theme').value;
    $('new-theme').replaceChildren(...themes.map(theme => new Option(`${theme.name} · revision ${theme.revision}`, themeKey(theme))));
    $('new-theme').value = themes.some(theme => themeKey(theme) === selection) ? selection : 'arcade-after-hours:2';
    $('theme-library').replaceChildren(...themes.map(theme => new Option(`${theme.name} · revision ${theme.revision}`, themeKey(theme))));
    if (!editingTheme) editTheme(themes.find(theme => theme.id === 'arcade-after-hours' && theme.revision === 2));
    else $('theme-library').value = themeKey(editingTheme);
    renderDesignTheme();
  }
  $('theme-library').addEventListener('change', () => {
    if (themeSaving || (themeDirty && !window.confirm('Discard unpublished theme settings?'))) { $('theme-library').value = themeKey(editingTheme); return; }
    editTheme(themes.find(theme => themeKey(theme) === $('theme-library').value));
  });
  $('theme-form').addEventListener('input', () => { themeDirty = true; scheduleThemePreview(); $('theme-status').textContent = 'Unpublished theme settings. Save as a new theme or publish a revision.'; });
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
    finally { themeSaving = false; $('theme-form').querySelectorAll('input, select, button').forEach(control => { control.disabled = false; }); $('theme-library').disabled = false; $('theme-publish').disabled = editingTheme.builtin; renderCollectionContext(); }
  }
  $('theme-form').addEventListener('submit', event => { event.preventDefault(); publishTheme(false); });
  $('theme-duplicate').addEventListener('click', () => publishTheme(true));
  $('theme-delete').addEventListener('click', async () => {
    if (!editingTheme || editingTheme.builtin || themeSaving) return;
    const theme = editingTheme;
    const revision = Math.max(...themes.filter(item => item.id === theme.id).map(item => item.revision));
    if (!window.confirm(`Permanently delete custom theme “${theme.name}” and all its revisions? Uploaded backgrounds and logos stay. Designs, generation history or BRB media still using this theme block deletion.${themeDirty ? ' Unpublished settings will also be discarded.' : ''}`)) return;
    themeSaving = true; $('theme-delete').disabled = true;
    $('theme-form').inert = true;
    $('theme-library').disabled = true;
    try {
      await request(`/api/generator/themes/${theme.id}`, 'DELETE', {revision});
      editingTheme = null; themeDirty = false;
      await loadThemes(); await loadAssets(); notify('Custom theme and its revisions deleted. Uploaded assets kept.');
    } catch (error) { themeError(error.message); }
    finally { themeSaving = false; $('theme-form').inert = false; $('theme-library').disabled = false; $('theme-delete').disabled = editingTheme?.builtin ?? true; renderCollectionContext(); }
  });
  $('theme-refresh').addEventListener('click', async () => {
    if (themeSaving || (themeDirty && !window.confirm('Discard unpublished settings and reload the latest theme revision?'))) return;
    try { await loadThemes(); editTheme(themes.filter(theme => theme.id === editingTheme.id).sort((a,b) => b.revision-a.revision)[0]); } catch (error) { themeError(error.message); }
  });
  $('theme-adopt').addEventListener('click', () => {
    if (!draft || !editingTheme || themeSaving) return;
    draft.theme = themeRef(themeKey(editingTheme)); renderDesignTheme(); changed(); renderCollectionContext();
    $('theme-adoption-state').textContent = `Adopted ${editingTheme.name} revision ${editingTheme.revision} in ${draft.name}. Revalidation and autosave pending; prepared and on-air revisions are unchanged.`;
  });
  $('design-theme').addEventListener('change', event => { event.stopPropagation(); if (!draft) return; draft.theme = themeRef($('design-theme').value); renderDesignTheme(); changed(); });
  $('apply-theme-update').addEventListener('click', () => {
    const latest = themes.filter(theme => theme.id === draft?.theme.id).sort((a,b) => b.revision-a.revision)[0];
    if (!latest) return; draft.theme = themeRef(themeKey(latest)); renderDesignTheme(); changed();
  });
  $('preview-time').addEventListener('input', schedulePreview);

  window.addEventListener('beforeunload', event => { if (!window.workspace?.departureApproved && (dirty || saving || namingJob || themeDirty || themeSaving)) { event.preventDefault(); event.returnValue = ''; } });
  (async () => {
    try {
      await listDesigns();
      await loadTemplates();
      await loadThemes();
      if (requestedDesign && requestedCollection !== 'brb') openDraft(await (await request(`/api/generator/designs/${encodeURIComponent(requestedDesign)}`)).json());
      else showCollection(['setup','designs','media','music','themes','templates','brb'].includes(requestedCollection) ? requestedCollection : 'setup');
    } catch (error) { $('design-list-state').textContent = 'Studio could not load. Reload this page to retry; saved designs are kept.'; notify(error.message); }
  })();
})();
