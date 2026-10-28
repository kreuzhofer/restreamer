'use strict';
(() => {
  const root = document.getElementById('brb-presentations');
  if (!root) return;
  const node = (tag,text='') => {const n=document.createElement(tag); n.textContent=text; return n;};
  const button = (text,action) => {const b=node('button',text); b.type='button'; b.className='preview-button'; b.onclick=action; return b;};
  const field = (text,input) => {const label=node('label',text);label.append(input);return label;};
  const form=node('form'); form.className='studio-authoring generator-soundtrack';form.inert=true;
  const name=node('input');name.value='BRB design';name.required=true;name.maxLength=180;
  const music=node('select'), duration=node('select'), behavior=node('select');
  const theme=node('select'), message=node('input'); message.value='BE RIGHT BACK';message.maxLength=40;message.required=true;
  duration.append(new Option('Follow music','music'),new Option('Fixed · one visual cycle','fixed'));
  behavior.append(new Option('Repeat to presentation end','repeat'),new Option('End at track end','end'));
  const number=(value,max) => {const input=node('input');input.type='number';input.min='0';input.max=String(max);input.step='any';input.value=String(value);return input;};
  const volume=number(100,100), fadeIn=number(0,600), fadeOut=number(0,600);
  const submit=button('Generate video');submit.className='primary-button studio-authoring';submit.type='submit';submit.disabled=true;
  const durationInfo=node('p');durationInfo.className='stage-help';durationInfo.setAttribute('role','status');
  const status=node('p');status.className='stage-help';status.setAttribute('role','status');
  const rows=new Map();
  let namingJob='', submittedJob='', savedDraft=null, savedContent='', draftLoading=true, savingDraft=false;
  const parameters=new URLSearchParams(location.search), designID=parameters.get('id');
  const results=node('div'), cleanup=node('div'), selectedResults=new Map();let cleanupMode=false;let busy=false, loading=false, dirty=false, assets=[], playlists=[];
  form.id='brb-design-form';submit.setAttribute('form',form.id);
  const header=node('header'), heading=node('h2','BRB design');header.className='brb-design-heading';header.append(heading,status,submit);
  const contentPanel=node('section'), appearancePanel=node('section'), musicPanel=node('section'), tabs=node('nav');tabs.className='studio-tabs';tabs.setAttribute('aria-label','BRB properties');
  appearancePanel.hidden=true;musicPanel.hidden=true;musicPanel.setAttribute('data-context-music','');
  for(const [label,panel] of [['Content',contentPanel],['Appearance',appearancePanel],['Music',musicPanel]]) {
    const tab=button(label,()=>{contentPanel.hidden=panel!==contentPanel;appearancePanel.hidden=panel!==appearancePanel;musicPanel.hidden=panel!==musicPanel;for(const item of tabs.children)item.setAttribute('aria-pressed',String(item===tab));});tab.setAttribute('aria-pressed',String(panel===contentPanel));tabs.append(tab);
  }
  contentPanel.append(field('Design name',name),field('Message',message));
  appearancePanel.append(field('Theme',theme));
  const advanced=node('details');advanced.append(node('summary','Volume, fades and looping'),field('Fixed-mode music behavior',behavior),field('Music volume (%)',volume),field('Fade in (seconds)',fadeIn),field('Fade out (seconds)',fadeOut));
  musicPanel.append(field('Music',music),field('Duration',duration),durationInfo,advanced);
  const monitor=node('div'), previewPane=node('section'), previewTabs=node('nav'), draftImage=node('canvas'), previewState=node('p','Draft preview loads after choosing an appearance.');
  previewTabs.className='studio-tabs';previewTabs.setAttribute('aria-label','BRB preview view');draftImage.hidden=true;draftImage.setAttribute('role','img');draftImage.setAttribute('aria-label','Current BRB draft preview');draftImage.className='brb-draft-preview';previewState.setAttribute('role','status');
  const selectPreview=showVideos=>{previewPane.hidden=showVideos;results.hidden=!showVideos;for(const tab of previewTabs.children)tab.setAttribute('aria-pressed',String((tab.textContent==='Videos')===showVideos));};
  previewTabs.append(button('Draft preview',()=>selectPreview(false)),button('Videos',()=>selectPreview(true)));previewPane.append(draftImage,previewState);monitor.append(previewTabs,previewPane,results);selectPreview(false);
  const latestSummary=node('section'), latestInfo=node('div'), latestName=node('strong','No video generated yet'), latestDetail=node('p','Generate to review this design with animation and music.'), reviewLatest=button('Review video',()=>{
    selectPreview(true);
    const latest=window.mediaPresentation.latestDesignVideo(savedDraft,[...rows.values()].map(entry=>entry.job),dirty);
    const video=latest&&rows.get(latest.job.id)?.node.querySelector('video');
    if(video){video.scrollIntoView({block:'nearest'});video.focus({preventScroll:true});video.play().catch(()=>{status.textContent='Press Play to review the exact prepared output.';});}
  });
  latestSummary.className='latest-video-summary';latestSummary.setAttribute('aria-label','Latest video');latestDetail.className='stage-help';reviewLatest.hidden=true;latestInfo.append(latestName,latestDetail);latestSummary.append(latestInfo,reviewLatest);previewPane.append(latestSummary);
  form.append(tabs,contentPanel,appearancePanel,musicPanel);root.append(header,monitor,form);
  root.setAttribute('data-panel','preview');
  const panels=node('nav');panels.className='brb-panel-switch studio-tabs';panels.setAttribute('aria-label','Editor panels');
  for(const [label,value] of [['Preview','preview'],['Properties','properties']]){const tab=button(label,()=>{root.setAttribute('data-panel',value);for(const item of panels.children)item.setAttribute('aria-pressed',String(item===tab));});tab.setAttribute('aria-pressed',String(value==='preview'));panels.append(tab);}root.insertBefore(panels,monitor);
  let previewTimer, previewSequence=0;
  function schedulePreview(){clearTimeout(previewTimer);const sequence=++previewSequence;previewTimer=setTimeout(async()=>{
    try{
      const response=await fetch(new URL('/api/generator/preview',location.origin),{method:'POST',headers:{'Content-Type':'application/json','X-Restreamer-Control':'1'},body:JSON.stringify(capture()),signal:AbortSignal.timeout(35000)});
      if(!response.ok)throw new Error(await responseError(response));
      const bitmap=await createImageBitmap(await response.blob());if(sequence!==previewSequence){bitmap.close();return;}
      draftImage.width=bitmap.width;draftImage.height=bitmap.height;draftImage.getContext('2d').drawImage(bitmap,0,0);bitmap.close();draftImage.hidden=false;previewState.textContent='Draft appearance · generate to review the full animation and music.';
    }catch(error){if(sequence===previewSequence)previewState.textContent=`Draft preview unavailable: ${error.message}`;}
  },350);}

  const selectResults=button('Select videos',()=>{cleanupMode=!cleanupMode;selectedResults.clear();for(const entry of rows.values()){entry.checkbox.hidden=!cleanupMode;entry.checkbox.checked=false;}renderCleanup();});
  const deleteResults=button('Delete selected',async()=>{
    if(busy||!selectedResults.size)return;
    const items=[...selectedResults.values()];
    if(!window.confirm(`Delete ${items.length} videos and their generated files? Uploaded music stays. Selected and playing videos are protected.\n${items.map(job=>job.name||`Render ${job.sequence}`).join('\n')}`))return;
    busy=true;deleteResults.disabled=true;refreshDraftLabels();
    try{const outcomes=await window.cleanupItems(items,job=>request(`/api/generator/jobs/${job.id}`,'DELETE',{}));for(const outcome of outcomes)if(outcome.deleted)selectedResults.delete(outcome.item.id);
      await refresh();status.textContent=`${outcomes.filter(outcome=>outcome.deleted).length} videos deleted. Uploaded files kept.`+(outcomes.some(outcome=>!outcome.deleted)?' Kept: '+outcomes.filter(outcome=>!outcome.deleted).map(outcome=>`${outcome.item.name||`Render ${outcome.item.sequence}`}: ${outcome.error}`).join('; '):'');
    }finally{busy=false;renderCleanup();refreshDraftLabels();}
  });cleanup.className='studio-authoring';cleanup.append(selectResults,deleteResults);results.append(cleanup);
  function renderCleanup(){selectResults.textContent=cleanupMode?'Cancel selection':'Select videos';deleteResults.hidden=!cleanupMode;deleteResults.disabled=busy||!selectedResults.size;deleteResults.textContent=`Delete selected (${selectedResults.size})`;}
  renderCleanup();
  const older=node('details'),olderHeading=node('summary','Older videos');older.append(olderHeading);results.append(older);

  async function loadDraft() {
    try {
      const themes=await request('/api/generator/themes');theme.replaceChildren(...themes.map(t=>new Option(t.name,`${t.id}:${t.revision}`)));
      theme.value='arcade-after-hours:2';
      if(designID) {
        savedDraft=await request(`/api/generator/designs/${encodeURIComponent(designID)}`);
        if(savedDraft.stage!=='brb')throw new Error('This is not a BRB design. Open it from Library.');
        name.value=savedDraft.name;heading.textContent=savedDraft.name;
        const pinned=`${savedDraft.theme.id}:${savedDraft.theme.revision}`;
        if(!themes.some(t=>`${t.id}:${t.revision}`===pinned)){const exact=await request(`/api/generator/themes/${encodeURIComponent(savedDraft.theme.id)}/revisions/${savedDraft.theme.revision}`);theme.append(new Option(`${exact.name} · saved version ${exact.revision}`,pinned));}
        message.value=savedDraft.scenes[0]?.text||'BE RIGHT BACK';theme.value=`${savedDraft.theme.id}:${savedDraft.theme.revision}`;
        duration.value=savedDraft.duration_mode||'fixed';volume.value=savedDraft.soundtrack?.volume_percent??100;fadeIn.value=savedDraft.soundtrack?.fade_in_seconds||0;fadeOut.value=savedDraft.soundtrack?.fade_out_seconds||0;behavior.value=savedDraft.soundtrack?.mode||'end';
      } else {duration.value='fixed';}
      status.textContent=savedDraft?'Saved':'New design';behavior.disabled=duration.value==='music';await refresh();savedContent=window.designEditState.contentKey(capture());draftLoading=false;form.inert=false;refreshDraftLabels();schedulePreview();
    }catch(error){status.textContent=error.message;submit.disabled=true;}
  }
  function capture() {
    const selected=window.musicSelection.decode(music.value),ref=theme.value.split(':');
    return {...(savedDraft||{}),name:name.value.trim(),stage:'brb',theme:{id:ref[0],revision:Number(ref[1])},duration_mode:duration.value,
      scenes:[{id:'brb',layout:'title',text:message.value,duration_seconds:4}],
      soundtrack:selected?{...selected,mode:behavior.value,volume_percent:Number(volume.value),fade_in_seconds:Number(fadeIn.value),fade_out_seconds:Number(fadeOut.value)}:null};
  }
  async function saveDraft() {
    if(draftLoading||savingDraft||busy)return false;
    if(savedDraft&&!dirty)return true;
    if(!form.reportValidity())return false;
    const data=capture();
    savingDraft=true;status.textContent='Saving…';refreshDraftLabels();
    try {
      savedDraft=await request('/api/generator/designs'+(savedDraft?`/${savedDraft.id}`:''),savedDraft?'PUT':'POST',data);
      savedContent=window.designEditState.contentKey(data);
      dirty=window.designEditState.contentKey(capture())!==savedContent;
      heading.textContent=name.value;status.textContent=dirty?'Unsaved changes':'Saved · changes not generated';
      history.replaceState(null,'',`/editor?editor=brb&id=${savedDraft.id}&return=${encodeURIComponent(parameters.get('return')||'/setup')}`);
      return true;
    }catch(error){status.textContent=`Could not save. Your edits are retained. ${error.message}`;return false;}
    finally{savingDraft=false;refreshDraftLabels();}
  }
  const saveButton=button('Save changes',saveDraft);saveButton.className='primary-button studio-authoring';saveButton.disabled=true;header.insertBefore(saveButton,submit);
  loadDraft();
  form.addEventListener('input',()=>{dirty=window.designEditState.contentKey(capture())!==savedContent;status.textContent=dirty?'Unsaved changes':savedDraft?'Saved':'New design';refreshDraftLabels();schedulePreview();behavior.disabled=duration.value==='music';updateDuration();});
  behavior.disabled=true;
  async function responseError(response){const text=(await response.text()).trim();try{const parsed=JSON.parse(text);return parsed.issues?.map(issue=>issue.message).join(' ')||parsed.error||text;}catch(_){return text;}}
  async function request(path,method='GET',body) {
    const response=await fetch(new URL(path,location.origin),{method,signal:AbortSignal.timeout(15000),cache:'no-store',headers:{'Content-Type':'application/json','X-Restreamer-Control':'1'},...(body?{body:JSON.stringify(body)}:{})});
    if (!response.ok) throw new Error(await responseError(response));
    return response.json();
  }

  let durationRequest = 0;
  async function updateDuration() {
    const requestID = ++durationRequest, selected = window.musicSelection.decode(music.value);
    if (!selected) { durationInfo.textContent = duration.value === 'music' ? 'Choose a track or playlist for Follow music.' : 'No music · download one visual cycle.'; return; }
    try {
      const exact = selected.playlist ? await request(`/api/music/playlists/${selected.playlist.id}/revisions/${selected.playlist.revision}`) : assets.find(a=>a.id===selected.asset.id)?.revisions.find(r=>r.revision===selected.asset.revision);
      if (requestID !== durationRequest) return;
      const seconds = selected.playlist ? exact.duration : exact?.duration_seconds;
      durationInfo.textContent = Number.isFinite(seconds) ? `Selected music: ${seconds.toFixed(3)} seconds${selected.playlist ? ` · saved version ${selected.playlist.revision}` : ''}. ${duration.value === 'music' ? 'Download covers this pass, rounded up to the next video frame. Live repeats until you leave BRB.' : 'Download uses one visual cycle.'}` : 'Selected music duration is unavailable.';
    } catch(error) { if(requestID===durationRequest) durationInfo.textContent=`Selected music unavailable: ${error.message}`; }
  }

  function refreshDraftLabels(){
    for(const entry of rows.values())entry.draftState.textContent=draftMatch(entry.job);
    const latest=window.mediaPresentation.latestDesignVideo(savedDraft,[...rows.values()].map(entry=>entry.job),dirty);
    latestName.textContent=latest?window.mediaPresentation.renderName(latest.job):'No video generated yet';
    latestDetail.textContent=latest?`${window.mediaPresentation.capturedMusic(latest.job,assets,playlists)} · ${window.mediaPresentation.date(latest.job.created_at)} · ${latest.match}`:'Generate to review this design with animation and music.';
    reviewLatest.hidden=!latest;
    saveButton.disabled=draftLoading||savingDraft||busy||!!savedDraft&&!dirty;
    saveButton.textContent=savingDraft?'Saving…':'Save changes';
    submit.hidden=!dirty&&latest?.match==='Matches current design';
    submit.disabled=draftLoading||savingDraft||busy||dirty||!savedDraft;
  }
  function draftMatch(job){return savedDraft&&job.design_snapshot.id===savedDraft.id&&job.design_snapshot.version===savedDraft.version&&!dirty?'Matches current design':'Based on earlier changes';}
  function capturedMusic(job){const selected=job.design_snapshot.soundtrack;if(!selected)return 'No music';if(selected.playlist)return `${playlists.find(item=>item.id===selected.playlist.id)?.name||'Saved playlist'} · music version ${selected.playlist.revision}`;return `${assets.find(item=>item.id===selected.asset?.id)?.name||'Saved track'} · music version ${selected.asset?.revision}`;}
  async function refresh() {
    if (loading) return;loading=true;
    try {
      const [nextAssets,nextPlaylists,jobs]=await Promise.all(['/api/generator/assets','/api/music/playlists','/api/generator/jobs'].map(path=>request(path)));
      assets=nextAssets;playlists=nextPlaylists;
      const submitted=jobs.find(job=>job.id===submittedJob);if(submitted&&!dirty&&!savingDraft&&['ready','failed','interrupted','cancelled'].includes(submitted.state)){status.textContent=submitted.state==='ready'?`Video ready · ${draftMatch(submitted)}. Preview it, then use it in Show setup.`:`Generation ${submitted.state}. ${submitted.error||'Your previously selected video is unchanged.'}`;}

      const selected=dirty?window.musicSelection.decode(music.value):(savedDraft?.soundtrack||window.musicSelection.decode(music.value));
      music.replaceChildren(...window.musicSelection.choices(assets,playlists,selected).map(c=>new Option(c.label,c.value)));music.value=window.musicSelection.value(selected);
      updateDuration();
      const ordered=jobs.filter(j=>j.kind==='brb'&&(!savedDraft||j.design_snapshot.id===savedDraft.id)).sort((a,b)=>b.sequence-a.sequence);
      const visible = new Set(ordered.map(j=>j.id));
      for (const [id,entry] of rows) if (!visible.has(id)) {entry.node.remove();rows.delete(id);}
      for (const job of ordered) {
        const signature=JSON.stringify(job), previous=rows.get(job.id);
        if(previous)previous.draftState.textContent=draftMatch(job);
        if (previous?.signature===signature || namingJob===job.id) continue;
        const row=node('article');row.className='generator-job';
        const checkbox=node('input');checkbox.type='checkbox';checkbox.className='studio-authoring';checkbox.hidden=!cleanupMode;checkbox.checked=selectedResults.has(job.id);checkbox.disabled=['running','cancelling'].includes(job.state);checkbox.setAttribute('aria-label',`Select ${job.name||`Render ${job.sequence}`} for cleanup`);checkbox.addEventListener('change',()=>{checkbox.checked?selectedResults.set(job.id,job):selectedResults.delete(job.id);renderCleanup();});row.append(checkbox);
        row.append(node('strong',`${job.name||`Render ${job.sequence}`} · ${job.state}`),node('p',`${job.duration.toFixed(2)} seconds per download${job.reused_visual?' · visual reused':''}${job.design_snapshot.duration_mode==='music'?' · loops until you leave BRB':''}`));
        const draftState=node('p',draftMatch(job));draftState.className='assignment-note';
        row.append(node('p',`${window.mediaPresentation.date(job.created_at)} · ${capturedMusic(job)}`),node('p',window.mediaPresentation.changes(job,ordered[ordered.indexOf(job)+1])),draftState);
        if (job.error) row.append(node('p',job.error));
        if (['queued','running','cancelling'].includes(job.state)) {
          const progress=node('progress');progress.max=100;progress.value=job.progress;row.append(progress);
          const cancel=button('Cancel preparation',()=>mutate(`/api/generator/jobs/${job.id}/cancel`));cancel.classList.add('studio-authoring');row.append(cancel);
        }
        if (job.state==='ready') {
          const video=node('video');video.controls=true;video.playsInline=true;video.preload='none';video.className='presentation-preview';video.src=`/api/library/revisions/${job.media_revision}/preview`;row.append(video);
          const download=node('a','Download one pass');download.href=video.src;download.download='brb-presentation.mp4';download.className='preview-button';
          const assign=node('a','Use for BRB');assign.href=`/setup?stage=brb&render=${job.media_revision}`;assign.className='preview-button';row.append(download,assign);
          const rename=button('Name this video',()=>{
            if(namingJob){status.textContent='Save or cancel the current video name first.';return;}
            namingJob=job.id;const editor=node('form'),input=node('input');input.value=job.name||'';input.required=true;input.maxLength=180;
            const save=button('Save name');save.type='submit';
            const cancel=button('Cancel name',()=>{namingJob='';editor.remove();rename.hidden=false;rename.focus();});
            editor.append(field('Video name',input),save,cancel);rename.hidden=true;row.append(editor);
            editor.onsubmit=async event=>{event.preventDefault();save.disabled=true;cancel.disabled=true;try{await request(`/api/generator/jobs/${job.id}/name`,'PUT',{name:input.value});namingJob='';rows.get(job.id).signature='';await refresh();}catch(error){status.textContent=error.message;}finally{save.disabled=false;cancel.disabled=false;}};input.focus();
          });row.append(rename);
        }
        if (['failed','interrupted','cancelled'].includes(job.state)) {const retry=button('Retry captured inputs',()=>mutate(`/api/generator/jobs/${job.id}/retry`));retry.classList.add('studio-authoring');row.append(retry);}
        if (!['running','cancelling'].includes(job.state)) {const remove=button('Delete result',()=>{if(window.confirm('Delete this presentation and its generated files? Uploaded music stays. Selected or playing media is protected.')) mutate(`/api/generator/jobs/${job.id}`,'DELETE');});remove.classList.add('studio-authoring');row.append(remove);}
        if(previous) previous.node.replaceWith(row); else results.prepend(row);
        rows.set(job.id,{node:row,signature,checkbox,draftState,job});
      }
      refreshDraftLabels();
      const latestReady=ordered.find(j=>j.state==='ready');
      for(const [target,group,offset] of [[results,ordered.filter(j=>j===latestReady||j===ordered[0]),1],[older,ordered.filter(j=>j!==latestReady&&j!==ordered[0]),1]])group.forEach((job,index)=>{const row=rows.get(job.id).node,at=target.children[index+offset];if(at!==row)target.insertBefore(row,at||null);});
      olderHeading.textContent=`Older videos (${ordered.filter(j=>j!==latestReady&&j!==ordered[0]).length})`;older.hidden=ordered.length<2;
    } catch(error) {status.textContent=`Music preparation status unavailable: ${error.message}`;}
    finally {loading=false;}
  }
  async function mutate(path,method='POST') {if(busy)return;busy=true;refreshDraftLabels();try{await request(path,method,{});await refresh();}catch(error){status.textContent=error.message;}finally{busy=false;refreshDraftLabels();}}
  form.onsubmit=async event=>{
    event.preventDefault();if(busy||draftLoading||savingDraft||dirty||!savedDraft)return;
    busy=true;refreshDraftLabels();
    try {
      selectPreview(true);
      const job=await request('/api/generator/jobs','POST',{design_id:savedDraft.id,version:savedDraft.version});submittedJob=job.id;
      status.textContent='Preparing on the server. Preview the result, then assign it in Show setup. Current playback and fallback remain unchanged.';await refresh();
    } catch(error) {status.textContent=error.message;}
    finally {busy=false;refreshDraftLabels();}
  };
  window.addEventListener('beforeunload',event=>{if(!window.workspace?.departureApproved&&(dirty||busy||savingDraft||namingJob)){event.preventDefault();event.returnValue='';}});
  window.addEventListener('music-playlists-changed',refresh);
  setInterval(()=>{if(!root.closest('[hidden]'))refresh();},5000);
})();
