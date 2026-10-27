'use strict';
(() => {
  const root = document.getElementById('brb-presentations');
  if (!root) return;
  const node = (tag,text='') => {const n=document.createElement(tag); n.textContent=text; return n;};
  const button = (text,action) => {const b=node('button',text); b.type='button'; b.className='preview-button'; b.onclick=action; return b;};
  const field = (text,input) => {const label=node('label',text);label.append(input);return label;};
  const form=node('form'); form.className='studio-authoring generator-soundtrack';
  const name=node('input');name.value='BRB presentation';name.required=true;name.maxLength=180;
  const music=node('select'), duration=node('select'), behavior=node('select');
  duration.append(new Option('Follow music','music'),new Option('Fixed · one visual cycle','fixed'));
  behavior.append(new Option('Repeat to presentation end','repeat'),new Option('End at track end','end'));
  const number=(value,max) => {const input=node('input');input.type='number';input.min='0';input.max=String(max);input.step='any';input.value=String(value);return input;};
  const volume=number(100,100), fadeIn=number(0,600), fadeOut=number(0,600);
  const submit=button('Prepare music presentation');submit.type='submit';
  const durationInfo=node('p');durationInfo.className='stage-help';durationInfo.setAttribute('role','status');
  const status=node('p');status.className='stage-help';status.setAttribute('role','status');
  const rows=new Map();
  const results=node('div');let busy=false, loading=false, dirty=false, assets=[], playlists=[];
  form.append(field('Presentation name',name),field('Music',music),field('Duration',duration),field('Fixed-mode music behavior',behavior),field('Music volume (%)',volume),field('Fade in (seconds)',fadeIn),field('Fade out (seconds)',fadeOut),submit);
  root.append(node('h3','BRB music presentations'),node('p','Use the current BRB visual with an uploaded track, a playlist, or no music. Preparation keeps its encoded animation. Follow music repeats video and music independently until you leave BRB; downloads contain one music pass.'),form,durationInfo,status,results);
  form.addEventListener('input',()=>{dirty=true;behavior.disabled=duration.value==='music';updateDuration();});
  behavior.disabled=true;
  async function request(path,method='GET',body) {
    const response=await fetch(new URL(path,location.origin),{method,signal:AbortSignal.timeout(15000),cache:'no-store',headers:{'Content-Type':'application/json','X-Restreamer-Control':'1'},...(body?{body:JSON.stringify(body)}:{})});
    if (!response.ok) throw new Error((await response.text()).trim());
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

  async function refresh() {
    if (loading) return;loading=true;
    try {
      const [nextAssets,nextPlaylists,jobs]=await Promise.all(['/api/generator/assets','/api/music/playlists','/api/generator/jobs'].map(path=>request(path)));
      assets=nextAssets;playlists=nextPlaylists;
      const selected=window.musicSelection.decode(music.value);
      music.replaceChildren(...window.musicSelection.choices(assets,playlists,selected).map(c=>new Option(c.label,c.value)));music.value=window.musicSelection.value(selected);
      updateDuration();
      const visible = new Set(jobs.filter(j=>j.kind==='brb').map(j=>j.id));
      for (const [id,entry] of rows) if (!visible.has(id)) {entry.node.remove();rows.delete(id);}
      for (const job of jobs.filter(j=>j.kind==='brb').sort((a,b)=>b.sequence-a.sequence)) {
        const signature=JSON.stringify(job), previous=rows.get(job.id);
        if (previous?.signature===signature) continue;
        const row=node('article');row.className='generator-job';
        row.append(node('strong',`${job.design_snapshot.name} · Render ${job.sequence} · ${job.state}`),node('p',`${job.duration.toFixed(2)} seconds per download${job.reused_visual?' · visual reused':''}${job.design_snapshot.duration_mode==='music'?' · loops until you leave BRB':''}`));
        if (job.error) row.append(node('p',job.error));
        if (['queued','running','cancelling'].includes(job.state)) {
          const progress=node('progress');progress.max=100;progress.value=job.progress;row.append(progress);
          const cancel=button('Cancel preparation',()=>mutate(`/api/generator/jobs/${job.id}/cancel`));cancel.classList.add('studio-authoring');row.append(cancel);
        }
        if (job.state==='ready') {
          const video=node('video');video.controls=true;video.playsInline=true;video.preload='none';video.className='presentation-preview';video.src=`/api/library/revisions/${job.media_revision}/preview`;row.append(video);
          const download=node('a','Download one pass');download.href=video.src;download.download='brb-presentation.mp4';download.className='preview-button';
          const assign=node('a','Review / assign BRB');assign.href=`/studio?collection=setup&stage=brb&render=${job.media_revision}`;assign.className='preview-button';row.append(download,assign);
        }
        if (['failed','interrupted','cancelled'].includes(job.state)) {const retry=button('Retry captured inputs',()=>mutate(`/api/generator/jobs/${job.id}/retry`));retry.classList.add('studio-authoring');row.append(retry);}
        if (!['running','cancelling'].includes(job.state)) {const remove=button('Delete result',()=>{if(window.confirm('Delete this presentation and its generated files? Uploaded music stays. Selected or playing media is protected.')) mutate(`/api/generator/jobs/${job.id}`,'DELETE');});remove.classList.add('studio-authoring');row.append(remove);}
        if(previous) previous.node.replaceWith(row); else results.prepend(row);
        rows.set(job.id,{node:row,signature});
      }
    } catch(error) {status.textContent=`Music preparation status unavailable: ${error.message}`;}
    finally {loading=false;}
  }
  async function mutate(path,method='POST') {if(busy)return;busy=true;try{await request(path,method,{});await refresh();}catch(error){status.textContent=error.message;}finally{busy=false;}}
  form.onsubmit=async event=>{
    event.preventDefault();if(busy)return;
    busy=true;submit.disabled=true;
    try {
      const dashboard=await request('/api/dashboard');
      const selected=window.musicSelection.decode(music.value);
      const soundtrack=selected?{...selected,mode:behavior.value,volume_percent:Number(volume.value),fade_in_seconds:Number(fadeIn.value),fade_out_seconds:Number(fadeOut.value)}:null;
      await request('/api/brb/presentations','POST',{base_generation:dashboard.brb_assets.generation,name:name.value.trim(),duration_mode:duration.value,soundtrack});
      dirty=false;status.textContent='Preparing on the server. Preview the result, then assign it in Show setup. Current playback and fallback remain unchanged.';await refresh();
    } catch(error) {status.textContent=error.message;}
    finally {busy=false;submit.disabled=false;}
  };
  window.addEventListener('beforeunload',event=>{if(!window.workspace?.departureApproved&&(dirty||busy)){event.preventDefault();event.returnValue='';}});
  window.addEventListener('music-playlists-changed',refresh);
  setInterval(()=>{if(!root.closest('[hidden]'))refresh();},5000);
  refresh();
})();
