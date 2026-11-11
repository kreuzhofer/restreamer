'use strict';
window.mountMusicPlaylists = function mountMusicPlaylists(root) {
  if (!root) return;
  const node = (tag, text) => { const n = document.createElement(tag); n.textContent = text || ''; return n; };
  let playlists = [], assets = [], draft = null, busy = false, savedTracks = '[]';
  const dirty = () => name.value !== draft.name || JSON.stringify(draft.tracks) !== savedTracks;
  const heading = node('h3', 'Playlists'), help = node('p', 'Arrange uploaded tracks in playback order. Saved presentations keep the exact playlist version you chose.');
  help.className = 'stage-help';
  const status = node('p'); status.setAttribute('role', 'status'); status.className = 'stage-help';
  const list = node('div'); if (root.id === 'music-playlists') list.id = 'music-playlist-list';
  const form = node('form'); form.className = 'studio-authoring';
  const nameLabel = node('label', 'Playlist name'), name = node('input'); name.required = true; name.maxLength = 180; nameLabel.append(name);
  const pickerLabel = node('label', 'Uploaded music'), picker = node('select'); pickerLabel.append(picker);
  const tracks = node('ol'); tracks.className = 'playlist-tracks';
  const button = (text, action) => { const b = node('button', text); b.type = 'button'; b.className = 'preview-button'; b.onclick = action; return b; };
  const add = button('Add track', () => { if (!picker.value || busy) return; const [id, revision] = picker.value.split(':'); draft.tracks.push({id, revision: Number(revision)}); renderTracks(); });
  const save = button('Save playlist'); save.type = 'submit';
  const cancel = button('Cancel edit', () => { if (!busy && (!dirty() || window.confirm('Discard unsaved playlist edits?'))) reset(); });
  const refreshButton = button('Refresh music and playlists', () => refresh().catch(showError));
  const create = button('New playlist', () => { if (busy || (dirty() && !window.confirm('Discard unsaved playlist edits?'))) return; reset(); form.hidden = false; name.focus(); }); create.classList.add('studio-authoring');
  form.append(nameLabel, pickerLabel, add, tracks, save, cancel); if (root.id === 'music-playlists') {
    const maintenance = node('details'), summary = node('summary','Playlist details');
    maintenance.className = 'task-disclosure'; maintenance.append(summary,help,refreshButton);
    root.replaceChildren(form, status, list, maintenance);
  } else root.replaceChildren(heading, help, create, refreshButton, list, form, status);
  function showError(error) { status.textContent = error.message; }
  async function request(path, method = 'GET', body) {
    const response = await fetch(location.origin + path, {method, cache:'no-store', signal:AbortSignal.timeout(10000), headers:{'Content-Type':'application/json','X-Restreamer-Control':'1'}, ...(body ? {body:JSON.stringify(body)} : {})});
    if (!response.ok) throw new Error((await response.text()).trim());
    return response.json();
  }
  function reset() { form.hidden = true; draft = {name:'', tracks:[]}; name.value = ''; savedTracks = '[]'; renderTracks(); }
  function describe(ref) { const asset = assets.find(a => a.id === ref.id); return `${asset?.name || 'Missing track'} · version ${ref.revision}`; }
  function renderTracks() {
    tracks.replaceChildren();
    draft.tracks.forEach((ref, i) => {
      const row = node('li'), title = node('span', describe(ref));
      const move = direction => { const j = i + direction; if (busy || j < 0 || j >= draft.tracks.length) return; [draft.tracks[i], draft.tracks[j]] = [draft.tracks[j], draft.tracks[i]]; renderTracks(); };
      const up = button('Move up', () => move(-1)), down = button('Move down', () => move(1)); up.disabled = i === 0; down.disabled = i === draft.tracks.length - 1;
      row.append(title, up, down, button('Remove track', () => { if (!busy) { draft.tracks.splice(i, 1); renderTracks(); } })); tracks.append(row);
    });
    const seconds = draft.tracks.reduce((total, ref) => total + (assets.find(a => a.id === ref.id)?.revisions.find(r => r.revision === ref.revision)?.duration_seconds || 0), 0);
    status.textContent = form.hidden ? '' : `${draft.tracks.length} tracks · ${(seconds / 60).toFixed(2)} minutes · limit 50 tracks / 60 minutes`;
    save.textContent = draft.id ? 'Save new playlist version' : 'Create playlist'; cancel.hidden = false;
  }
  function renderList() {
    list.replaceChildren();
    for (const playlist of playlists) {
      const row = node('article'); row.className = 'playlist-row';
      const title = node('h4', playlist.name), info = node('p', `${playlist.tracks.length} ${playlist.tracks.length === 1 ? 'track' : 'tracks'} · ${Math.round(playlist.duration)} seconds`);
      const usage = node('p', playlist.uses.length ? `Used by ${playlist.uses.length} saved ${playlist.uses.length === 1 ? 'item' : 'items'}` : 'Not used by a saved presentation.'); usage.className = 'stage-help';
      const edit = button('Edit playlist', () => { if (busy) return; if (dirty() && !window.confirm('Discard your unsaved playlist draft?')) return; draft = {id:playlist.id, version:playlist.revision, name:playlist.name, tracks:playlist.tracks.map(ref => ({...ref}))}; name.value = draft.name; savedTracks = JSON.stringify(draft.tracks); form.hidden = false; renderTracks(); name.focus(); });
      const remove = button('Delete playlist', async () => {
        if (busy || !window.confirm(`Delete playlist “${playlist.name}” and its saved versions? Uploaded tracks stay. Referenced playlists cannot be deleted.`)) return;
        busy = true;
        try { await request(`/api/music/playlists/${playlist.id}`, 'DELETE', {version:playlist.revision}); await refresh(); status.textContent = 'Playlist deleted. Uploaded tracks retained.'; } catch (error) { showError(error); } finally { busy = false; }
      });
      edit.classList.add('studio-authoring'); remove.classList.add('studio-authoring'); remove.disabled = playlist.uses.length > 0;
      const actions=node('details'), summary=node('summary','Actions');actions.className='playlist-actions studio-authoring';actions.append(summary,edit,remove);
      row.append(title, info, usage, actions); list.append(row);
    }
  }
  async function refresh() {
    [playlists, assets] = await Promise.all([request('/api/music/playlists'), request('/api/generator/assets')]);
    const options = [new Option('Choose an uploaded track', '')];
    for (const asset of assets.filter(a => a.kind === 'audio')) for (const revision of asset.revisions) options.push(new Option(`${asset.name} · version ${revision.revision}`, `${asset.id}:${revision.revision}`));
    picker.replaceChildren(...options); renderList();
    window.dispatchEvent(new Event('music-playlists-changed'));
  }
  form.onsubmit = async event => {
    event.preventDefault(); if (busy) return;
    busy = true; save.disabled = true;
    try { await request('/api/music/playlists' + (draft.id ? `/${draft.id}` : ''), draft.id ? 'PUT' : 'POST', {name:name.value.trim(), version:draft.version || 0, tracks:draft.tracks}); reset(); await refresh(); status.textContent = 'Playlist saved. Existing prepared presentations are unchanged.'; }
    catch (error) { showError(error); }
    finally { busy = false; save.disabled = false; }
  };
  window.addEventListener('beforeunload', event => { if (!window.workspace?.departureApproved && (busy || dirty())) { event.preventDefault(); event.returnValue = ''; } });
  reset(); refresh().catch(showError);
  return {refresh, catalog:() => playlists, create:()=>create.onclick(), canClose:()=>!busy&&(!dirty()||window.confirm("Discard unsaved playlist edits?"))};
};
window.musicPlaylists = window.mountMusicPlaylists(document.getElementById('music-playlists'));
