'use strict';
(() => {
  const renderName = job => job.name || `Render ${job.sequence}`;
  const date = value => value && Number.isFinite(Date.parse(value)) ? new Intl.DateTimeFormat(undefined, {dateStyle:'medium', timeStyle:'short'}).format(new Date(value)) : 'Date unavailable';
  const jobFor = (id, jobs) => [...jobs].sort((a,b) => b.sequence-a.sequence).find(job => job.media_revision === id);
  const label = (id, jobs, revisions) => {
    if (!id) return 'Not assigned';
    const job = jobFor(id, jobs);
    return job ? `${job.design_snapshot.name} · ${renderName(job)}` : revisions.find(item => item.id === id)?.name || 'Unavailable render';
  };
  function changes(job, previous) {
    if (!previous) return 'First retained render';
    const a = previous.design_snapshot, b = job.design_snapshot, changed = [];
    for (const [key, name] of [['name','Name'], ['scenes','Scene content / timing'], ['theme','Appearance'], ['soundtrack','Music'], ['duration_mode','Duration mode'], ['loop_transition','Loop transition'], ['ending_fade_seconds','Ending fade']]) {
      if (JSON.stringify(a[key]) !== JSON.stringify(b[key])) changed.push(name);
    }
    if (JSON.stringify(job.profile) !== JSON.stringify(previous.profile)) changed.push('Streaming profile');
    return changed.length ? `${changed.join(' · ')} changed` : 'Same saved settings · rendered again';
  }
  function groups(stage, jobs, revisions) {
    const groups = new Map(), generated = new Set(jobs.map(job => job.media_revision).filter(Boolean));
    for (const job of [...jobs].sort((a,b) => b.sequence-a.sequence)) {
      if (job.design_snapshot.stage !== stage || !job.media_revision || job.state !== 'ready') continue;
      const media = revisions.find(item => item.id === job.media_revision);
      if (!media) continue;
      const key = job.design_snapshot.id;
      if (!groups.has(key)) groups.set(key, {id:key, name:job.design_snapshot.name, design:true, renders:[]});
      if (!groups.get(key).renders.some(item => item.id === media.id)) groups.get(key).renders.push({...media, job});
    }
    for (const media of revisions) {
      if (generated.has(media.id) || jobs.some(job => job.visual_revision === media.id && job.media_revision !== media.id)) continue;
      const key = media.library_id || media.id;
      if (!groups.has(key)) groups.set(key, {id:key, name:media.name, design:false, renders:[]});
      groups.get(key).renders.push(media);
    }
    return [...groups.values()];
  }
  function capturedMusic(job, assets, playlists) {
    const music=job.design_snapshot.soundtrack;
    if(!music)return 'No music';
    return music.playlist ? playlists.find(item=>item.id===music.playlist.id)?.name || 'Saved playlist' : assets.find(item=>item.id===music.asset?.id)?.name || 'Saved track';
  }
  function latestDesignVideo(draft, jobs, dirty) {
    if (!draft) return null;
    const job = [...jobs].sort((a,b) => b.sequence-a.sequence).find(item => item.design_snapshot.id === draft.id && item.state === 'ready' && item.media_revision);
    return job ? {job, match:!dirty && job.design_snapshot.version === draft.version ? 'Matches current design' : 'Based on earlier changes'} : null;
  }
  function selection(saved, stage, revision) {
    if (!['prestream','ending','brb'].includes(stage)) throw new Error('Unsupported stage selection');
    return {...saved, [stage]:revision};
  }
  window.mediaPresentation = {renderName, date, jobFor, label, changes, groups, selection, latestDesignVideo, capturedMusic};
})();
