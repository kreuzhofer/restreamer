(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  let current, connected = false, otherPending = false, dirty = false;
  let themes = [], candidate = null, reviewed = '', preparing = false, operation = false, fetching = false, controller;
  async function request(path, method = 'GET', body, signal) {
    const response = await fetch(new URL(path, location.origin), {method, credentials: 'same-origin', signal, headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: body === undefined ? undefined : JSON.stringify(body)});
    if (!response.ok) throw new Error((await response.text()).trim() || `Request failed (${response.status})`);
    return response;
  }
  function status(message) { $('brb-theme-status').textContent = message; }
  function render() {
    $('brb-theme-settings').hidden = !current?.generation;
    if (!current) return;
    $('brb-theme-current').textContent = current.theme ? `Current BRB: ${current.theme.name} · revision ${current.theme.revision}. Shared edits never update it automatically.` : 'Current BRB: legacy arcade/custom-image styling. Shared themes are opt-in.';
    $('brb-theme-prepare').disabled = !connected || otherPending || dirty || preparing || operation || !themes.length;
    $('brb-theme-select').disabled = preparing || operation;
    $('brb-theme-cancel').hidden = !preparing;
    $('brb-theme-candidate').hidden = !candidate;
    if (!candidate) return;
    const stale = candidate.base_generation !== current.generation;
    $('brb-theme-candidate-detail').textContent = `${candidate.settings.theme.name} · revision ${candidate.settings.theme.revision} · ${candidate.settings.custom_image ? 'saved custom image' : candidate.settings.text} · ${candidate.settings.music ? `music at ${candidate.settings.volume}%` : 'silent audio'}${stale ? ' · BRB settings changed: prepare again before activation.' : ''}`;
    $('brb-theme-activate').disabled = !connected || stale || preparing || operation || otherPending || reviewed !== candidate.id;
    $('brb-theme-discard').disabled = !connected || preparing || operation;
    $('brb-theme-preview').disabled = preparing || operation;
  }
  async function loadThemes() {
    themes = await (await request('/api/generator/themes')).json();
    const previous = $('brb-theme-select').value || (current?.theme ? `${current.theme.id}:${current.theme.revision}` : 'retro:2');
    $('brb-theme-select').replaceChildren();
    for (const theme of themes) $('brb-theme-select').add(new Option(`${theme.name} · revision ${theme.revision}`, `${theme.id}:${theme.revision}`));
    if ([...$('brb-theme-select').options].some(option => option.value === previous)) $('brb-theme-select').value = previous;
    render();
  }
  function adoptCandidate(next) {
    if (candidate?.id !== next?.id) {
      reviewed = ''; $('brb-theme-video').pause(); $('brb-theme-video').removeAttribute('src'); $('brb-theme-video').load(); $('brb-theme-video').hidden = true;
    }
    candidate = next; render();
  }
  async function loadCandidate() {
    if (!connected || fetching || preparing || operation || !current?.generation) return;
    fetching = true;
    try { adoptCandidate(await (await request('/api/brb/theme/candidate')).json()); }
    catch (error) { status(error.message); }
    finally { fetching = false; }
  }
  $('brb-theme-refresh').addEventListener('click', () => loadThemes().catch(error => status(error.message)));
  $('brb-theme-select').addEventListener('change', () => status('Theme choice is not active. Prepare and preview it before activation.'));
  $('brb-theme-prepare').addEventListener('click', async () => {
    if (!current || preparing || operation || otherPending || dirty) return;
    const [id, revision] = $('brb-theme-select').value.split(':');
    preparing = true; controller = new AbortController();
    status('Preparing captured BRB settings. Current media stays active.'); render();
    try { adoptCandidate(await (await request('/api/brb/theme/prepare', 'POST', {theme: {id, revision: Number(revision)}, base_generation: current.generation}, controller.signal)).json()); status('Prepared, not active. Preview the exact result, then activate explicitly.'); }
    catch (error) { status(error.name === 'AbortError' ? 'Preparation cancelled. Current BRB and any earlier candidate are unchanged.' : error.message); }
    finally { preparing = false; controller = null; render(); loadCandidate(); }
  });
  $('brb-theme-cancel').addEventListener('click', () => controller?.abort());
  $('brb-theme-preview').addEventListener('click', () => {
    if (!candidate) return;
    reviewed = '';
    const video = $('brb-theme-video'); video.dataset.candidate = candidate.id; video.src = `/api/brb/theme/candidate/preview?id=${encodeURIComponent(candidate.id)}`; video.hidden = false;
    video.play().catch(() => status('Use the video controls to review the exact prepared BRB.')); render();
  });
  $('brb-theme-video').addEventListener('loadeddata', () => {
    if (candidate?.id === $('brb-theme-video').dataset.candidate) { reviewed = candidate.id; status('Exact prepared BRB loaded. Activation changes the current fallback only after your confirmation.'); render(); }
  });
  $('brb-theme-video').addEventListener('error', () => { if (!candidate) return; reviewed = ''; status('Exact preview unavailable or replaced. Reload the candidate and preview again.'); render(); });
  $('brb-theme-activate').addEventListener('click', async () => {
    if (!candidate || reviewed !== candidate.id || candidate.base_generation !== current?.generation || operation) return;
    if (!window.confirm('Activate this exact prepared BRB? If BRB is on air, its video and music restart from the beginning.')) return;
    operation = true; render();
    try { await request('/api/brb/theme/activate', 'POST', {id: candidate.id, base_generation: candidate.base_generation}); adoptCandidate(null); status('Activated the reviewed BRB.'); }
    catch (error) { status(error.message); }
    finally { operation = false; render(); loadCandidate(); }
  });
  $('brb-theme-discard').addEventListener('click', async () => {
    if (!candidate || operation || preparing) return;
    operation = true; render();
    try { await request('/api/brb/theme/candidate', 'DELETE', {id: candidate.id}); adoptCandidate(null); status('Candidate discarded. Current BRB is unchanged.'); }
    catch (error) { status(error.message); }
    finally { operation = false; render(); }
  });
  window.brbThemes = {update(snapshot, online, pending, unsaved) {
    const first = !current?.generation;
    current = snapshot?.brb_assets; connected = online; otherPending = pending; dirty = unsaved;
    render();
    if (first && current?.generation) { loadThemes().catch(error => status(error.message)); loadCandidate(); }
  }};
  setInterval(loadCandidate, 3000);
})();
