'use strict';
(() => {
  const $ = id => document.getElementById(id);
  document.querySelectorAll('[data-disclose]').forEach(button => button.addEventListener('click', () => {
    const panel = $(button.dataset.disclose); panel.hidden = !panel.hidden;
    button.setAttribute('aria-expanded', String(!panel.hidden));
    if (!panel.hidden) panel.querySelector('input,select,button')?.focus();
  }));
  document.querySelectorAll('[data-filter-list]').forEach(input => {
    const list=$(input.dataset.filterList),collection=input.closest('[data-collection-panel]')?.dataset.collectionPanel;
    const params=new URLSearchParams(location.search);if(params.get('collection')===collection||(collection==='designs'&&!params.has('collection')))input.value=params.get('q')||'';
    const count=document.createElement('p');count.className='stage-help';count.setAttribute('role','status');input.parentElement.after(count);
    const apply=()=>{const query=input.value.trim().toLocaleLowerCase();let matches=0;for(const row of list.children){row.hidden=!row.textContent.toLocaleLowerCase().includes(query);if(!row.hidden)matches++;}count.textContent=query?`${matches} matching ${collection==='music'?'tracks':collection==='media'?'uploads':'designs'}`:'';};
    input.addEventListener('input',()=>{apply();if(document.body.dataset.workspace==='library'){const url=new URL(location.href);input.value?url.searchParams.set('q',input.value):url.searchParams.delete('q');history.replaceState(null,'',url.pathname+url.search);}});
    new MutationObserver(apply).observe(list,{childList:true});apply();
  });
  function disclosure(element, label) {
    if (!element) return;
    const details = document.createElement('details'), summary = document.createElement('summary');
    summary.textContent = label; details.className = 'task-disclosure';
    element.before(details); details.append(summary, element);
    return details;
  }
  disclosure($('asset-upload'), 'Upload or replace an asset');
  disclosure($('media-library-upload'), 'Upload a video for playback');
  // Technical explanations stay available close to the controls they describe.
  for (const element of document.querySelectorAll('.studio-scenes > section > p.stage-help:not(#sequence-duration)')) disclosure(element, 'Scene limits');
  disclosure($('text-help'), 'Text and font limits');
  disclosure($('ending-fade-help'), 'Ending fade behavior');
  disclosure(document.querySelector('.generator-soundtrack > p:last-child'), 'Music playback details');
  disclosure($('soundtrack-settings'), 'Volume, fades and looping');
  disclosure($('generation-summary'), 'Selection and generation details');
  const liveHelp = $('stage-help'); if (liveHelp) disclosure(liveHelp, 'About stage controls');
  const tools = $('live-brb-tools'); if (tools) {tools.querySelector('summary').textContent = 'Fallback readiness'; $('brb-theme-settings').hidden = true;}
  const collectionNav = document.querySelector('.studio-collections');
  if (collectionNav) {
    const heading = document.createElement('h1'); heading.textContent = 'Library'; heading.id = 'library-heading'; collectionNav.before(heading);
    const sync = () => {heading.hidden = document.body.dataset.workspace !== 'library';}; sync(); window.addEventListener('studio-route-changed',sync);
  }
  const breadcrumb = $('shared-editor-nav');
  if (breadcrumb) {
    const route = window.studioRouting.parse(new URL(location.href));
    const link = breadcrumb.querySelector('a'); link.href = route.back; link.textContent = route.back.startsWith('/library') ? 'Library' : 'Show setup';
  }
  const uploadForm=$('asset-upload');
  if(uploadForm){
    const dialog=document.createElement('dialog'),title=document.createElement('h2'),close=document.createElement('button');
    dialog.className='context-upload';dialog.setAttribute('aria-label','Upload an asset');close.type='button';close.className='preview-button';close.textContent='Back';dialog.append(title,close);document.body.append(dialog);
    let placements=[],origin;
    const canClose=()=>{if(window.assetUploads.busy()){$('asset-status').textContent='Upload is still running. Wait for it or use Cancel upload before returning.';return false;}return true;};
    const restore=()=>{for(const {placeholder,element} of placements)placeholder.replaceWith(element);placements=[];origin?.focus();window.dispatchEvent(new Event('music-playlists-changed'));};
    close.addEventListener('click',()=>{if(canClose())dialog.close();});dialog.addEventListener('cancel',event=>{if(!canClose())event.preventDefault();});dialog.addEventListener('close',restore);
    window.openAssetUpload=(kind,trigger)=>{
      if(window.assetUploads.busy())return;
      if($('asset-file').files.length&&$('asset-kind').value!==kind&&!window.confirm('Discard the unsubmitted file selection and choose another asset?'))return;
      origin=trigger;title.textContent=kind==='audio'?'Upload music':kind==='video'?'Upload video':'Upload image';
      placements=[uploadForm,$('asset-status'),$('asset-error')].map(element=>{const placeholder=document.createElement('span');placeholder.hidden=true;element.before(placeholder);dialog.append(element);return {placeholder,element};});
      $('asset-kind').value=kind;$('asset-kind').dispatchEvent(new Event('change'));dialog.showModal();$('asset-file').focus();
    };
    for(const [selector,kind,label] of [['[data-inspector-panel="music"],[data-context-music]','audio','Upload music'],['#scene-image-controls','image','Upload image'],['#scene-video-controls','video','Upload video']]){
      for(const panel of document.querySelectorAll(selector)){const button=document.createElement('button');button.type='button';button.className='preview-button';button.textContent=label;button.addEventListener('click',()=>window.openAssetUpload(kind,button));panel.append(button);}
    }
  }
  for(const musicPanel of document.querySelectorAll('[data-inspector-panel="music"],[data-context-music]')) {
    const create=document.createElement('button');create.type='button';create.textContent='New playlist';create.className='preview-button';musicPanel.append(create);
    const dialog=document.createElement('dialog');dialog.setAttribute('aria-label','Create a playlist');const content=document.createElement('section'),close=document.createElement('button');close.type='button';close.className='preview-button';close.textContent='Back to design';dialog.append(close,content);document.body.append(dialog);
    let manager;
    create.addEventListener('click',()=>{dialog.showModal();if(!manager)manager=window.mountMusicPlaylists(content);manager.create();});
    close.addEventListener('click',()=>{if(manager.canClose()){dialog.close();window.musicPlaylists?.refresh();create.focus();}});
    dialog.addEventListener('cancel',event=>{if(!manager.canClose())event.preventDefault();});
  }
  const settings = document.querySelector('.settings-desktop');
  if (settings) {
    const tabs = document.createElement('nav'); tabs.className = 'settings-tabs'; tabs.setAttribute('aria-label','Settings sections');
    const sections = [['ingestion','Ingestion','.settings-ingestion'],['destinations','Destinations','.settings-destinations'],['profile','Streaming profile','.settings-profile']];
    function select(name) {
      for (const [key,,selector] of sections) settings.querySelector(selector).hidden = key !== name;
      for (const link of tabs.children) {if (link.dataset.section === name) link.setAttribute('aria-current','page'); else link.removeAttribute('aria-current');}
    }
    for (const [key,label] of sections) {
      const link = document.createElement('a'); link.href = `/settings#${key}`; link.textContent = label; link.dataset.section = key;
      link.addEventListener('click', event => {event.preventDefault(); history.pushState(null,'',link.href); select(key);}); tabs.append(link);
    }
    settings.before(tabs);
    const restore = () => select(sections.some(([key]) => location.hash === '#'+key) ? location.hash.slice(1) : location.hash === '#ingestion-settings' ? 'ingestion' : 'destinations');
    restore(); window.addEventListener('popstate', restore);
  }
  // Arrow navigation is shared by local tab strips, never by command groups.
  document.querySelectorAll('.studio-tabs,.studio-collections,.settings-tabs').forEach(tabs => tabs.addEventListener('keydown',event => {
    if (!['ArrowLeft','ArrowRight','Home','End'].includes(event.key)) return;
    const items = [...tabs.querySelectorAll('button,a')].filter(item => !item.hidden && !item.disabled);
    const index = items.indexOf(document.activeElement); if(index < 0) return;
    event.preventDefault(); const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length-1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + items.length) % items.length;
    items[next]?.focus(); items[next]?.click();
  }));
})();
