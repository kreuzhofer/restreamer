'use strict';
(() => {
  const root = document.getElementById('studio-music');
  if (!root) return;
  const get = id => document.getElementById(id);
  const tabs = [...root.querySelectorAll('[data-music-view]')];
  const query = get('music-search'), status = get('music-search-status');
  const params = new URL(location.href).searchParams;
  let view = params.get('music_view') === 'playlists' ? 'playlists' : 'tracks';
  const queries = {tracks:'', playlists:''}; queries[view] = params.get('q') || '';
  function filter() {
    queries[view] = query.value;
    const list = get(view === 'tracks' ? 'music-list' : 'music-playlist-list');
    let count = 0;
    for (const row of list.children) {
      row.hidden = !row.textContent.toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase());
      if (!row.hidden) count++;
    }
    status.textContent = query.value ? `${count} matching ${view}` : '';
  }
  function select(next) {
    view = next;
    get('music-tracks-view').hidden = view !== 'tracks';
    get('music-playlists').hidden = view !== 'playlists';
    get('music-upload-open').hidden = view !== 'tracks';
    get('music-playlist-create').hidden = view !== 'playlists';
    tabs.forEach(tab => tab.setAttribute('aria-pressed', String(tab.dataset.musicView === view)));
    query.value = queries[view]; query.placeholder = `Search ${view}`;
    query.setAttribute('aria-label', `Search ${view}`); filter();
  }
  function saveURL() {
    const url = new URL(location.href);
    url.searchParams.set('music_view', view);
    query.value ? url.searchParams.set('q',query.value) : url.searchParams.delete('q');
    history.replaceState(null,'',url.pathname+url.search);
  }
  tabs.forEach(tab => tab.addEventListener('click', () => {select(tab.dataset.musicView);saveURL();}));
  query.addEventListener('input', () => {filter();saveURL();});
  get('music-playlist-create').addEventListener('click', () => window.musicPlaylists.create());
  for (const id of ['music-list','music-playlist-list']) new MutationObserver(filter).observe(get(id),{childList:true});
  select(view);
})();
