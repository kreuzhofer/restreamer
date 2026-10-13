'use strict';
(() => {
  const el = id => document.getElementById(id);
  const form = el('brb-form');
  const desktop = matchMedia('(min-width: 601px)');
  let snapshot, connected = false, pending = false, dirty = false, generation;
  function render() {
    if (!snapshot) return;
    const brb = snapshot.brb || {}, active = connected && snapshot.stage?.source === 'brb', deliberate = snapshot.stage?.stage === 'BRB';
    window.brbThemes?.update(snapshot, connected, pending, dirty);
    el('brb-badge').textContent = !connected ? 'Unavailable' : active ? deliberate ? 'Deliberate BRB' : 'Fallback BRB' : brb.ready ? 'Protection ready' : 'Not configured';
    el('brb-badge').className = `badge ${active ? 'warn' : brb.ready ? 'live' : ''}`;
    el('brb-status').textContent = !connected ? 'Status disconnected · last known state' : !brb.enabled ? 'BRB is disabled in server configuration.' : active ? deliberate ? 'Deliberate break · OBS will not replace it' : 'Fallback media is on air' : brb.ready ? 'Fallback BRB is prepared' : 'Fallback BRB is not ready';
    el('brb-help').textContent = deliberate ? 'Use Return in Live to resume the recorded stage, or choose a different stage.' : 'Prepared fallback is required for Prestream, clips, deliberate BRB and Ending. Ordinary LIVE can run without BRB.';
    const profile = snapshot.brb_profile;
    el('brb-active-profile').hidden = !brb.ready || !profile;
    if (profile) el('brb-active-profile').textContent = `Shared profile · managed in Settings: ${profile.width} × ${profile.height} · ${profile.fps} fps · ${profile.sample_rate / 1000} kHz stereo`;
    el('brb-error').hidden = !brb.error; el('brb-error').textContent = brb.error || '';
    if (!form) return;
    el('brb-settings').hidden = !brb.ready;
    el('brb-unsaved').hidden = !brb.ready || !dirty || pending;
    el('brb-save').classList.toggle('needs-save', dirty && !pending);
    el('brb-save').disabled = !desktop.matches || !connected || !brb.ready || pending;
    el('brb-save').textContent = pending ? 'Preparing for activation…' : 'Prepare & activate BRB';
    form.querySelectorAll('input').forEach(input => { input.disabled = pending || !desktop.matches; });
    const assets = snapshot.brb_assets;
    if (brb.ready && assets) {
      if (generation !== assets.generation) {
        generation = assets.generation; el('brb-image').src = `/api/brb/image?v=${encodeURIComponent(generation)}`; el('brb-image').hidden = false;
      }
      el('brb-asset-status').textContent = `${assets.theme ? `${assets.theme.name} · revision ${assets.theme.revision} · visual loop · still preview` : assets.custom_image ? 'Custom image · static screen' : 'Default arcade animation · 32-second loop · still preview'} · ${assets.music ? `Looping music at ${assets.volume}%` : 'Silent audio'}`;
      if (!dirty && !pending) { el('brb-text').value = assets.text ?? 'BE RIGHT BACK'; el('brb-volume').value = assets.volume; }
    }
  }
  form?.addEventListener('submit', async event => {
    event.preventDefault();
    if (!desktop.matches || !connected || pending) return;
    if (snapshot.stage?.stage !== 'OFF' && !window.confirm('Prepare and activate these BRB settings? Current media stays active during preparation. Success replaces BRB immediately and restarts its video and music if BRB is on air.')) return;
    const data = new FormData(form);
    for (const name of ['image', 'music']) if (!data.get(name)?.size) data.delete(name);
    pending = true; el('brb-save-status').textContent = 'Preparing video and audio… Current BRB stays active until the replacement is ready.'; render();
    try {
      const response = await fetch(`${location.origin}/api/brb/assets`, {method: 'POST', headers: {'X-Restreamer-Control': '1'}, body: data, signal: AbortSignal.timeout(180000)});
      if (!response.ok) throw new Error((await response.text()).trim() || 'Could not prepare BRB.');
      form.reset(); dirty = false; el('brb-save-status').textContent = 'Saved and activated · BRB is ready.';
    } catch (error) { el('brb-save-status').textContent = error.message; }
    finally {
      try {
        const response = await fetch(`${location.origin}/api/dashboard`, {cache: 'no-store', signal: AbortSignal.timeout(8000)});
        if (!response.ok) throw new Error('Status unavailable');
        snapshot = await response.json(); window.workspace?.update(snapshot, true);
      } catch { connected = false; window.workspace?.update(null, false); }
      pending = false; render();
    }
  });
  function changed() { dirty = true; el('brb-save-status').textContent = 'Not active yet. Prepare & activate BRB applies these changes after preparation.'; render(); }
  form?.addEventListener('input', changed); form?.addEventListener('change', changed);
  window.addEventListener('beforeunload', event => { if (!window.workspace?.departureApproved && (dirty || pending)) { event.preventDefault(); event.returnValue = ''; } });
  desktop.addEventListener('change', render);
  window.brbMedia = {update(data, online) { snapshot = data; connected = online; render(); }};
})();
