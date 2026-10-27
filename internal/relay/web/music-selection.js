'use strict';
(() => {
  const value = track => track?.playlist ? `playlist:${track.playlist.id}:${track.playlist.revision}` : track?.asset ? `${track.asset.id}:${track.asset.revision}` : '';
  function decode(value) {
    if (!value) return null;
    const parts = value.split(':');
    return parts[0] === 'playlist' ? {playlist:{id:parts[1], revision:Number(parts[2])}} : {asset:{id:parts[0], revision:Number(parts[1])}};
  }
  function choices(assets, playlists, selected) {
    const choices = [{label:'No music',value:''}];
    for (const asset of assets.filter(a => a.kind === 'audio')) for (const revision of asset.revisions) choices.push({label:`${asset.name} · version ${revision.revision} · ${revision.duration_seconds.toFixed(1)}s`,value:`${asset.id}:${revision.revision}`});
    for (const p of playlists) choices.push({label:`Playlist: ${p.name} · version ${p.revision} · ${(p.duration / 60).toFixed(1)} min`,value:`playlist:${p.id}:${p.revision}`});
    const pinned = value(selected);
    if (pinned && !choices.some(c => c.value === pinned)) {
      const playlist = playlists.find(p => p.id === selected?.playlist?.id);
      choices.push({label:playlist ? `${playlist.name} · saved version ${selected.playlist.revision}` : 'Unavailable selected music', value:pinned});
    }
    return choices;
  }
  window.musicSelection = {value, decode, choices};
})();
