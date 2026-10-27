(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  const authoring = !!$('brb-theme-select');
  const desktop = matchMedia('(min-width: 601px)');
  let candidateCurrent = false, loadFailed = false;
  const fail = message => { $('brb-theme-error').textContent = message; $('brb-theme-error').hidden = !message; };
  let current, connected = false, otherPending = false, dirty = false;
  let themes = [], candidate = null, reviewed = '', preparing = false, operation = false, fetching = false, preparation = null;
  async function request(path, method = 'GET', body, signal = AbortSignal.timeout(10000)) {
    const response = await fetch(new URL(path, location.origin), {method, credentials: 'same-origin', signal, headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: body === undefined ? undefined : JSON.stringify(body)});
    if (!response.ok) throw new Error((await response.text()).trim() || `Request failed (${response.status})`);
    return response;
  }
  function status(message) { $('brb-theme-status').textContent = message; }
  function render() {
    $('brb-theme-settings').hidden = !current?.generation;
    if (!current) return;
    $('brb-theme-current').textContent = current.theme ? `Current BRB: ${current.theme.name} · revision ${current.theme.revision}. Shared edits never update it automatically.` : 'Current BRB: legacy arcade/custom-image styling. Shared themes are opt-in.';
    const unsupportedProfile = current.profile.width > 1920 || current.profile.height > 1080 || current.profile.fps > 30;
    if (authoring) {
      $('brb-theme-profile-limit').hidden = !unsupportedProfile;
      $('brb-theme-prepare').disabled = !desktop.matches || unsupportedProfile || !connected || otherPending || preparing || operation || !themes.length;
      $('brb-theme-select').disabled = preparing || operation;
      $('brb-theme-cancel').hidden = !preparing;
      $('brb-theme-cancel').disabled = !connected || operation || preparation?.state === 'cancelling';
    }
    $('brb-theme-candidate').hidden = !candidate;
    $('brb-theme-empty').hidden = !!candidate;
    $('brb-theme-empty').textContent = candidateCurrent ? 'No prepared candidate. Prepare an exact theme revision in Studio; current BRB remains ready.' : 'Prepared BRB status unavailable or loading.';
    if (!candidate) return;
    const stale = candidate.base_generation !== current.generation;
    $('brb-theme-candidate-detail').textContent = `${candidate.settings.theme.name} · revision ${candidate.settings.theme.revision} · ${candidate.video_duration}-second visual loop · ${candidate.settings.custom_image ? 'saved custom image' : candidate.settings.text} · ${candidate.settings.music ? `music at ${candidate.settings.volume}%` : 'silent audio'}${stale ? ' · BRB settings changed: prepare again before activation.' : ''}`;
    $('brb-theme-activate').disabled = !candidateCurrent || !connected || stale || preparing || operation || otherPending || reviewed !== candidate.id;
    if ($('brb-theme-discard')) $('brb-theme-discard').disabled = !desktop.matches || !connected || !candidateCurrent || preparing || operation;
    $('brb-theme-preview').disabled = !connected || !candidateCurrent || preparing || operation;
  }
  async function loadThemes() {
    if (!authoring) return;
    themes = await (await request('/api/generator/themes')).json();
    const previous = $('brb-theme-select').value || (current?.theme ? `${current.theme.id}:${current.theme.revision}` : 'arcade-after-hours:2');
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
    if (!connected || fetching || operation || !current?.generation) return;
    fetching = true;
    try {
      const previous = preparation;
      preparation = await (await request('/api/brb/theme/preparation')).json();
      preparing = preparation?.state === 'running' || preparation?.state === 'cancelling';
      if (preparation && (previous?.id !== preparation.id || previous?.state !== preparation.state)) {
        status(preparing ? (preparation.state === 'cancelling' ? 'Cancelling this preparation…' : 'Preparing captured BRB settings. You can close this page; work continues on the server.') : preparation.state === 'ready' ? 'Prepared, not active. Preview the exact result, then activate explicitly.' : preparation.error || `Preparation ${preparation.state}. Current BRB is unchanged.`);
      }
      const next = await (await request('/api/brb/theme/candidate')).json();
      if (loadFailed) { fail(''); loadFailed = false; }
      candidateCurrent = true; adoptCandidate(next);
    }
    catch (error) { loadFailed = true; candidateCurrent = false; fail(`Prepared BRB status unavailable. ${error.message}`); render(); }
    finally { fetching = false; }
  }
  $('brb-theme-refresh')?.addEventListener('click', () => loadThemes().catch(error => status(error.message)));
  $('brb-theme-select')?.addEventListener('change', () => status('Theme choice is not active. Prepare and preview it before activation.'));
  $('brb-theme-prepare')?.addEventListener('click', async () => {
    if (!desktop.matches || !current || preparing || operation || otherPending) return;
    fail('');
    const [id, revision] = $('brb-theme-select').value.split(':');
    if (!$('brb-text').reportValidity() || !$('brb-volume').reportValidity()) return;
    if ($('brb-image-file').files.length || $('brb-music-file').files.length || [...$('brb-form').querySelectorAll('input[type=checkbox]')].some(input => input.checked)) { fail('Upload/reset changes use the separate Apply uploads control. Prepare preview captures message, theme and volume only.'); return; }
    operation = true;
    status('Accepting captured BRB settings. Current media stays active.'); render();
    try {
      preparation = await (await request('/api/brb/theme/prepare', 'POST', {theme: {id, revision: Number(revision)}, base_generation: current.generation, async: true, text: $('brb-text').value, volume: Number($('brb-volume').value)})).json();
      preparing = true;
      status('Preparing on the server. You can close this page and return to the result.');
    }
    catch (error) { fail(error.message); }
    finally { operation = false; render(); loadCandidate(); }
  });
  $('brb-theme-cancel')?.addEventListener('click', async () => {
    if (!desktop.matches || !preparation || !preparing || operation) return;
    fail('');
    const id = preparation.id;
    operation = true; render();
    try { await request('/api/brb/theme/preparation/cancel', 'POST', {id}); status('Cancelling this preparation. Current BRB and any earlier candidate stay unchanged.'); }
    catch (error) { fail(error.message); }
    finally { operation = false; render(); loadCandidate(); }
  });
  $('brb-theme-preview').addEventListener('click', () => {
    if (!candidate || !candidateCurrent || !connected) return;
    fail(''); reviewed = '';
    const video = $('brb-theme-video'); video.dataset.candidate = candidate.id; video.src = `/api/brb/theme/candidate/preview?id=${encodeURIComponent(candidate.id)}`; video.hidden = false;
    video.play().catch(() => status('Use the video controls to review the exact prepared BRB.')); render();
  });
  $('brb-theme-video').addEventListener('loadeddata', () => {
    if (candidate?.id === $('brb-theme-video').dataset.candidate) { reviewed = candidate.id; status('Exact prepared BRB loaded. Activation changes the current fallback only after your confirmation.'); render(); }
  });
  $('brb-theme-video').addEventListener('error', () => { if (!candidate) return; reviewed = ''; status('Exact preview unavailable or replaced. Reload the candidate and preview again.'); render(); });
  $('brb-theme-activate').addEventListener('click', async () => {
    if (!candidateCurrent || !connected || !candidate || reviewed !== candidate.id || candidate.base_generation !== current?.generation || operation) return;
    if (!window.confirm('Activate this exact prepared BRB? If BRB is on air, its video and music restart from the beginning.')) return;
    fail(''); operation = true; render();
    try { await request('/api/brb/theme/activate', 'POST', {id: candidate.id, base_generation: candidate.base_generation}); window.brbMedia?.accepted(candidate.settings); adoptCandidate(null); status('Activated the reviewed BRB.'); }
    catch (error) { fail(error.message); }
    finally { operation = false; render(); loadCandidate(); }
  });
  $('brb-theme-discard')?.addEventListener('click', async () => {
    if (!desktop.matches || !candidate || !candidateCurrent || operation || preparing) return;
    fail('');
    operation = true; render();
    try { await request('/api/brb/theme/candidate', 'DELETE', {id: candidate.id}); adoptCandidate(null); status('Candidate discarded. Current BRB is unchanged.'); }
    catch (error) { fail(error.message); }
    finally { operation = false; render(); }
  });
  window.brbThemes = {update(snapshot, online, pending, unsaved) {
    const first = !current?.generation;
    current = snapshot?.brb_assets; connected = online; otherPending = pending; dirty = unsaved;
    render();
    if (first && current?.generation) { loadThemes().catch(error => status(error.message)); loadCandidate(); }
  }};
  desktop.addEventListener('change', render);
  setInterval(loadCandidate, 3000);
})();
