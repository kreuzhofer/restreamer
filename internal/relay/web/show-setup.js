'use strict';
(() => {
  const $ = id => document.getElementById(id), ui = window.mediaPresentation;
  if (!$('show-setup')) return;
  let catalog={revisions:[]}, jobs=[], selections={}, dashboard, assets=[], playlists=[], designs=[], current=false, fetching=false, saving=false, signature='', chooserStage='';
  let requested=new URLSearchParams(location.search), previewMedia=null, previewStage='', reviewed=false;
  const dialog=$('show-chooser'), previewDialog=$('show-preview');
  const node=(tag,text='',className='')=>{const el=document.createElement(tag);el.textContent=text;el.className=className;return el;};
  const button=(text,action,className='preview-button')=>{const b=node('button',text,className);b.type='button';b.onclick=action;return b;};
  const link=(text,href,className='')=>{const a=node('a',text,className);a.href=href;return a;};
  const title=stage=>({prestream:'Starting soon',brb:'BRB',ending:'Ending',fallback:'Fallback'})[stage];
  const error=message=>{$('show-setup-error').textContent=message;$('show-setup-error').hidden=!message;};
  async function request(path,method='GET',body) {
    const r=await fetch(new URL(path,location.origin),{method,cache:'no-store',headers:{'Content-Type':'application/json','X-Restreamer-Control':'1'},signal:AbortSignal.timeout(90000),...(body?{body:JSON.stringify(body)}:{})});
    if(!r.ok) throw new Error((await r.text()).trim()||'Request failed');return r.status===204?null:r.json();
  }
  function soundtrack(job) {
    const track=job?.design_snapshot.soundtrack;
    if(!track)return 'No music';
    return track.playlist ? playlists.find(p=>p.id===track.playlist.id)?.name || 'Saved playlist' : assets.find(a=>a.id===track.asset?.id)?.name || 'Saved track';
  }
  const behavior=(stage,job)=>stage==='ending'&&job?.design_snapshot.duration_mode!=='music'?'Stops broadcast when finished':'Loops until stopped';
  function poster(media) {const video=node('video','','show-poster');video.muted=true;video.playsInline=true;video.preload='metadata';if(media.state==='ready')video.src=`/api/library/revisions/${encodeURIComponent(media.id)}/preview#t=0.1`;video.setAttribute('aria-label',media.name||'Video preview');return video;}
  function editURL(stage,job) {
    const saved=job&&designs.some(d=>d.id===job.design_snapshot.id);
    if(stage==='brb')return `/editor?editor=brb${saved?'&id='+encodeURIComponent(job.design_snapshot.id):''}&return=%2Fsetup`;
    return saved?`/editor?id=${encodeURIComponent(job.design_snapshot.id)}&return=%2Fsetup`:`/library?collection=designs&stage=${stage}`;
  }
  function render() {
    const next=JSON.stringify([catalog,jobs,selections,dashboard?.stage,dashboard?.brb_assets,current]);if(next===signature)return;signature=next;
    const focused=document.activeElement?.closest('[data-stage]')?.dataset.stage, action=document.activeElement?.textContent;
    const cards=$('show-setup-cards');cards.replaceChildren();
    for(const stage of ['prestream','brb','ending']) {
      const card=node('article','','show-card');card.dataset.stage=stage;
      const id=selections[stage],media=catalog.revisions.find(m=>m.id===id),job=ui.jobFor(id,jobs),legacy=stage==='brb'&&!id;
      const thumb=button('',()=>media&&preview(media,stage),'show-thumbnail');thumb.setAttribute('aria-label',`Preview ${title(stage)}`);thumb.disabled=!current||(!legacy&&media?.state!=='ready');
      if(media)thumb.append(poster(media));else if(legacy&&dashboard?.brb_assets?.generation){const img=node('img','','show-poster');img.src=`/api/brb/image?v=${encodeURIComponent(dashboard.brb_assets.generation)}`;img.alt='Retained BRB appearance';thumb.append(img);thumb.disabled=true;}else thumb.append(node('span','No video selected','show-poster show-placeholder'));
      const info=node('div','','show-card-info');info.append(node('h2',title(stage)));
      const name=job?.design_snapshot.name||media?.name||(legacy?'Retained BRB screen':'Choose a video');
      const nameLink=job||legacy?link(name,editURL(stage,job),'show-design-name'):node('span',name,'show-design-name');info.append(nameLink);
      info.append(node('p',legacy?'Your existing appearance and music are preserved':`${soundtrack(job)} · ${behavior(stage,job)}`,'stage-help'));
      const active=dashboard?.stage?.stage===stage.toUpperCase()?dashboard.stage.media?.revision:null;
      info.append(node('p',active&&active!==id?`Playing now: ${ui.label(active,jobs,catalog.revisions)} · this selection applies next time`:id?`${media?.state==='ready'?'Ready':'Unavailable'} · Selected for next use${job?' · '+ui.date(job.created_at):''}`:legacy?'Ready · Legacy fallback retained':'Not selected','assignment-note'));
      const change=button('Change',()=>choose(stage));change.disabled=!current||saving;card.append(thumb,info,change);cards.append(card);
    }
    const fallback=node('details','','fallback-options');fallback.append(node('summary','When a source is unavailable'));
    const mode=selections.fallback_mode||'legacy';fallback.append(node('p',mode==='shared'?'Uses the selected BRB video. Changes apply on the next fallback entry.':mode==='override'?`Uses ${ui.label(selections.fallback,jobs,catalog.revisions)}.`:'Your previously configured fallback is preserved. You can switch to the selected BRB video.','stage-help'));
    const shared=button('Use selected BRB',()=>assign('fallback-mode','shared'));shared.disabled=!current||!selections.brb;
    fallback.append(shared,button('Choose a different video',()=>choose('fallback')));cards.append(fallback);
    if(focused){const card=cards.querySelector(`[data-stage="${focused}"]`);[...card.querySelectorAll('button,a')].find(el=>el.textContent===action)?.focus({preventScroll:true});}
  }
  const usePreview=button('Use this video',()=>assign(previewStage,previewMedia.id),'primary-button');usePreview.disabled=true;previewDialog.append(usePreview);
  function preview(media,stage) {
    previewMedia=media;previewStage=stage;reviewed=false;usePreview.disabled=true;usePreview.textContent=`Use for ${title(stage)}`;
    $('show-preview-heading').textContent=ui.label(media.id,jobs,catalog.revisions);
    $('show-preview-video').src=`/api/library/revisions/${encodeURIComponent(media.id)}/preview`;
    $('show-preview-error').hidden=true;previewDialog.showModal();
  }
  $('show-preview-video').addEventListener('loadeddata',()=>{reviewed=true;usePreview.disabled=!current||saving;});
  function choice(media,stage,group,index) {
    const row=node('article','','render-choice'),body=node('div'),job=media.job;
    row.append(poster(media));body.append(node('h3',job?`${ui.renderName(job)}${index===0?' · Latest ready':''}`:media.name));
    body.append(node('p',`${job?ui.date(job.created_at)+' · ':''}${soundtrack(job)} · ${Math.round(media.duration)} seconds`,'stage-help'),node('p',behavior(stage,job),'stage-help'));
    if(job)body.append(node('p',ui.changes(job,group.renders[index+1]?.job),'stage-help'));
    const selected=selections[stage]===media.id;if(selected)body.append(node('p','Selected for next use','assignment-note'));
    const play=button('Preview and select',()=>preview(media,stage));play.disabled=!current||media.state!=='ready';body.append(play);
    if(media.state!=='ready')body.append(node('p',media.error||'Video unavailable. Generate a new video.','notice'));
    const technical=node('details','','render-technical');technical.append(node('summary','Technical details'),node('code',media.id));body.append(technical);row.append(body);return row;
  }
  function choose(stage) {
    chooserStage=stage;$('show-chooser-heading').textContent=`Choose ${title(stage)}`;$('show-chooser-status').textContent='Preview a video, then select it for next use. Current playback stays unchanged.';
    const list=$('show-chooser-list');list.replaceChildren();const groups=ui.groups(stage==='fallback'?'brb':stage,jobs,catalog.revisions);
    if(!groups.length)list.append(node('p','No ready videos yet. Create a design or prepare an upload.'));
    for(const group of groups){const section=node('section','','render-group');section.append(node('h3',group.name));
      const first=group.renders.findIndex(m=>m.state==='ready');const main=first<0?0:first;section.append(choice(group.renders[main],stage,group,main));
      const older=node('details','','render-older');older.append(node('summary',`Older videos (${group.renders.length-1})`));
      group.renders.forEach((m,i)=>{if(i===main)return;if(m.id===selections[stage])section.append(choice(m,stage,group,i));else older.append(choice(m,stage,group,i));});if(older.children.length>1)section.append(older);list.append(section);
    }
    list.append(link('Create or edit a design',editURL(stage==='fallback'?'brb':stage),'preview-button'));
    if(!dialog.open)dialog.showModal();
  }
  async function assign(stage,revision) {
    if(!current||saving)return;if(stage!=='fallback-mode'&&!reviewed)return;
    saving=true;usePreview.disabled=true;usePreview.textContent='Saving selection…';
    try {
      const saved=await request('/api/stage-media'),next={...saved};
      if(stage==='fallback-mode'){next.fallback_mode=revision;next.fallback='';}else if(stage==='fallback'){next.fallback=revision;next.fallback_mode='override';}else{next[stage]=revision;if(stage==='brb'&&!saved.brb&&!saved.fallback_mode)next.fallback_mode='shared';}
      await request('/api/stage-media','PUT',next);selections=next;previewDialog.close();dialog.close();$('show-setup-status').textContent='Selection saved for next use. Current playback stays unchanged.';
    }catch(e){$('show-chooser-status').textContent=`Selection failed: ${e.message}`;error(e.message);}finally{saving=false;usePreview.textContent=`Use for ${title(previewStage)}`;usePreview.disabled=!reviewed;await load();}
  }
  async function load(){if(fetching||saving)return;fetching=true;try{[catalog,jobs,selections,dashboard,assets,playlists,designs]=await Promise.all(['/api/library','/api/generator/jobs','/api/stage-media','/api/dashboard','/api/generator/assets','/api/music/playlists','/api/generator/designs'].map(p=>request(p)));current=true;error('');}catch(e){current=false;error(`Status unavailable; selections may be stale. ${e.message}`);usePreview.disabled=true;}finally{fetching=false;render();}
    if(current&&requested){const stage=requested.get('stage'),id=requested.get('render');requested=null;if(['prestream','brb','ending'].includes(stage)&&id){choose(stage);const media=catalog.revisions.find(m=>m.id===id);if(media?.state==='ready')preview(media,stage);}}
  }
  $('show-chooser-close').onclick=()=>dialog.close();$('show-preview-close').onclick=()=>previewDialog.close();
  previewDialog.addEventListener('close',()=>{const v=$('show-preview-video');v.pause();v.removeAttribute('src');v.load();reviewed=false;});
  $('show-preview-video').addEventListener('error',()=>{$('show-preview-error').hidden=false;reviewed=false;usePreview.disabled=true;});
  $('show-setup-refresh').onclick=load;window.showSetup={refresh:load};load();setInterval(()=>{if(!$('show-setup').hidden&&!dialog.open&&!previewDialog.open)load();},5000);
})();
