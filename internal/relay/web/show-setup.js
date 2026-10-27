'use strict';
(() => {
  const $ = id => document.getElementById(id), ui = window.mediaPresentation;
  if (!$('show-setup')) return;
  let catalog = {revisions:[]}, jobs = [], selections = {}, dashboard, current = false, fetching = false, saving = false, signature = '';
  const dialog = $('show-chooser');
  const requestedRender = new URLSearchParams(location.search).get('render');
  let chooserStage, chooserDesign, requested = new URLSearchParams(location.search);
  const node = (tag, text, className) => { const item = document.createElement(tag); if (text) item.textContent = text; if (className) item.className = className; return item; };
  const button = (text, action) => { const item = node('button',text,'preview-button'); item.type = 'button'; item.addEventListener('click',action); return item; };
  const link = (text, href) => { const item = node('a',text,'preview-button'); item.href = href; return item; };
  const title = stage => ({prestream:'Prestream',brb:'BRB',ending:'Ending'})[stage];
  const fail = message => { $('show-setup-error').textContent = message; $('show-setup-error').hidden = !message; };
  async function request(path, method = 'GET', body) {
    const response = await fetch(new URL(path, location.origin), {method,cache:'no-store',headers:{'Content-Type':'application/json','X-Restreamer-Control':'1'},signal:AbortSignal.timeout(10000),...(body ? {body:JSON.stringify(body)} : {})});
    if (!response.ok) throw new Error((await response.text()).trim() || 'Show setup unavailable');
    return response.status === 204 ? null : response.json();
  }
  function poster(media) {
    const video = node('video', '', 'show-poster'); video.muted = true; video.playsInline = true; video.preload = 'metadata'; video.setAttribute('aria-label',media.name || 'Prepared render');
    // The retained video supplies the thumbnail; no draft reconstruction is shown as a render.
    if (media.state === 'ready') video.src = `/api/library/revisions/${encodeURIComponent(media.id)}/preview#t=0.1`;
    return video;
  }
  function technical(media) {
    const details = node('details', '', 'render-technical'); details.append(node('summary','Technical details'), node('code',media.id)); return details;
  }
  function preview(media, name) {
    $('show-preview-heading').textContent = name;
    $('show-preview-video').src = `/api/library/revisions/${encodeURIComponent(media.id)}/preview`;
    $('show-preview-error').hidden = true;
    $('show-preview').showModal();
  }
  function render() {
    const next = JSON.stringify([catalog.revisions,jobs,selections,dashboard?.brb_assets,dashboard?.brb?.ready,current]);
    if (signature === next) return;
    const focusedStage = document.activeElement?.closest('.show-card')?.dataset.stage, focusedText = document.activeElement?.textContent;
    signature = next;
    $('show-setup-cards').replaceChildren();
    for (const stage of ['prestream','brb','ending']) {
      const card = node('article','','show-card'), heading = node('h2',title(stage)); card.dataset.stage = stage; card.append(heading);
      if (stage === 'brb') {
        const assets = dashboard?.brb_assets;
        if (assets?.generation) { const img = node('img','','show-poster'); img.src = `/api/brb/image?v=${encodeURIComponent(assets.generation)}`; img.alt = 'Assigned BRB screen'; card.append(img); }
        card.append(node('h3',assets?.theme?.name || 'BRB screen'),node('p',assets?.text || 'No BRB prepared','stage-help'),node('p',dashboard?.brb?.ready ? 'Assigned · also used if OBS disconnects' : 'BRB not ready','stage-help'));
        card.append(link('Change / preview','/studio?editor=brb&view=results'),link('Edit design','/studio?editor=brb'));
      } else {
        const id = selections[stage], media = catalog.revisions.find(item => item.id === id), job = ui.jobFor(id,jobs);
        if (media) card.append(poster(media)); else card.append(node('div',id ? 'Assigned render unavailable' : 'Choose what plays here','show-poster show-placeholder'));
        card.append(node('h3',ui.label(id,jobs,catalog.revisions)));
        const latest = job && jobs.filter(item => item.design_snapshot.id === job.design_snapshot.id && item.state === 'ready').sort((a,b) => b.sequence-a.sequence)[0];
        card.append(node('p',media ? `${media.state} · ${Math.round(media.duration)} seconds${job ? ` · ${ui.date(job.created_at)}` : ''}` : 'Assign a prepared design or uploaded video.','stage-help'));
        card.append(node('p',latest && latest.media_revision !== id ? `Newer render available · ${ui.renderName(latest)}` : id ? 'Assigned for next use' : 'Not assigned','assignment-note'));
        const actions = node('div','','generator-actions');
        const change = button('Change',() => choose(stage)); change.disabled = !current || saving; actions.append(change);
        const play = button('Preview',() => preview(media,ui.label(id,jobs,catalog.revisions))); play.disabled = !current || media?.state !== 'ready'; actions.append(play);
        actions.append(link(job ? 'Edit design' : 'Create design',job ? `/studio?id=${encodeURIComponent(job.design_snapshot.id)}` : `/studio?collection=designs&stage=${stage}`));
        if (job) actions.append(button('Render history',() => choose(stage,job.design_snapshot.id)));
        card.append(actions);
      }
      $('show-setup-cards').append(card);
    }
    if (focusedStage) { const card = [...$('show-setup-cards').children].find(item => item.dataset.stage === focusedStage); [...card.querySelectorAll('button,a')].find(item => item.textContent === focusedText)?.focus({preventScroll:true}); }
  }
  function choose(stage, designID) {
    chooserStage = stage; chooserDesign = designID;
    $('show-chooser-heading').textContent = designID ? `${title(stage)} · render history` : `Choose ${title(stage)}`;
    $('show-chooser-status').textContent = 'Assignment changes the next use. Current and suspended playback stay unchanged.';
    const list = $('show-chooser-list'); list.replaceChildren();
    const groups = ui.groups(stage,jobs,catalog.revisions).filter(group => !designID || group.id === designID);
    if (!groups.length) list.append(node('p','No retained renders yet. Create a design or upload a library video in Media.','stage-help'));
    for (const group of groups) {
      const details = node('details','','render-group'); details.open = !!designID;
      details.append(node('summary',`${group.name} · ${group.renders.length} render${group.renders.length === 1 ? '' : 's'}${group.renders.some(media => media.id === selections[stage]) ? ' · Assigned' : ''}`));
      if (group.design) details.append(link('Edit design',`/studio?id=${encodeURIComponent(group.id)}`));
      for (const [index,media] of group.renders.entries()) {
        const row = node('article','','render-choice'); row.dataset.revision = media.id;
        row.append(poster(media));
        const body = node('div'), job = media.job;
        body.append(node('h3',`${job ? ui.renderName(job) : media.name}${index === 0 && job ? ' · Latest retained' : ''}${media.id === selections[stage] ? ' · Assigned' : ''}${media.id === requestedRender ? ' · Requested' : ''}`));
        body.append(node('p',`${job ? ui.date(job.created_at) + ' · ' : ''}${Math.round(media.duration)} seconds · ${media.state}`,'stage-help'));
        if (job) body.append(node('p',ui.changes(job,group.renders[index+1]?.job),'stage-help'));
        const play = button('Preview',() => preview(media,`${group.name} · ${job ? ui.renderName(job) : 'Uploaded video'}`)); play.disabled = !current || media.state !== 'ready';
        const use = button(`Use for ${title(stage)}`,() => assign(stage,media.id)); use.disabled = !current || saving || media.state !== 'ready' || selections[stage] === media.id;
        body.append(play,use,technical(media)); row.append(body); details.append(row);
      }
      list.append(details);
    }
    list.append(link('Create a design',`/studio?collection=designs&stage=${stage}`),link('Upload a video','/studio?collection=media'));
    if (!dialog.open) dialog.showModal();
  }
  async function assign(stage, revision) {
    if (!current || saving) return;
    saving = true; dialog.querySelectorAll('button:not(#show-chooser-close)').forEach(item => {item.disabled = true;});
    try {
      // Read the latest selections so unrelated stages and clip shortcuts are preserved.
      const saved = await request('/api/stage-media');
      const next = ui.selection(saved,stage,revision);
      await request('/api/stage-media','PUT',next); selections = next;
      dialog.close(); $('show-setup-status').textContent = `${title(stage)} assigned. Current playback is unchanged.`;
    } catch (error) { saving = false; choose(chooserStage,chooserDesign); $('show-chooser-status').textContent = `Could not assign: ${error.message}`; }
    finally { saving = false; await load(); }
  }
  async function load() {
    if (fetching || saving) return;
    fetching = true;
    try { [catalog,jobs,selections,dashboard] = await Promise.all(['/api/library','/api/generator/jobs','/api/stage-media','/api/dashboard'].map(path => request(path))); current = true; fail(''); }
    catch (error) { current = false; fail(`Show setup is unavailable. Last loaded assignments may be stale. ${error.message}`); dialog.querySelectorAll('button:not(#show-chooser-close)').forEach(item => {item.disabled = true;}); }
    finally { fetching = false; render(); }
    if (current && requested) { const stage = requested.get('stage'), id = requested.get('render'); requested = null; if (['prestream','ending'].includes(stage) && id) { const job = ui.jobFor(id,jobs); choose(stage,job?.design_snapshot.id); $('show-chooser-status').textContent = `Review ${ui.label(id,jobs,catalog.revisions)} and use its assignment button. Playback stays unchanged.`; } }
  }
  $('show-chooser-close').addEventListener('click',() => dialog.close());
  $('show-preview-close').addEventListener('click',() => $('show-preview').close());
  $('show-preview').addEventListener('close',() => { const video = $('show-preview-video'); video.pause(); video.removeAttribute('src'); video.load(); });
  $('show-preview-video').addEventListener('error',() => {$('show-preview-error').hidden = false;});
  $('show-setup-refresh').addEventListener('click',load);
  window.showSetup = {refresh:load};
  load(); setInterval(() => {if (!$('show-setup').hidden && !dialog.open && !$('show-preview').open) load();},5000);
})();
