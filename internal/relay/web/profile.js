'use strict';
(() => {
  const el = id => document.getElementById(id);
  const equal = (a, b) => a && b && ['width', 'height', 'fps', 'sample_rate'].every(key => Number(a[key]) === Number(b[key]));
  let snapshot, saved, draft, connected = false, assetsBusy = false, pending = false;
  const dirty = () => !!draft && !equal(draft, saved);
  const canSave = () => connected && snapshot?.brb?.ready && !snapshot.forwarding && !assetsBusy && !pending && dirty();

  function renderProfile() {
    el('input-profile').hidden = !snapshot?.brb?.ready || !saved;
    if (!saved) return;
    el('profile-active').textContent = `Active saved profile: ${saved.width} × ${saved.height} · ${saved.fps} fps · ${saved.sample_rate / 1000} kHz stereo`;
    const resolution = `${draft.width}x${draft.height}`;
    const select = el('profile-resolution');
    if (![...select.options].some(option => option.value === resolution)) select.add(new Option(`${draft.width} × ${draft.height}`, resolution));
    select.value = resolution;
    el('profile-fps').value = String(draft.fps);
    el('profile-sample-rate').value = String(draft.sample_rate);
    el('profile-fields').disabled = !connected || pending || assetsBusy || snapshot.forwarding;
    el('profile-locked').hidden = !snapshot.forwarding;
    el('profile-unsaved').hidden = !dirty() || pending;
    el('profile-save').disabled = !canSave();
    el('profile-save').textContent = pending ? 'Rebuilding BRB…' : 'Save & rebuild';
    el('profile-save').classList.toggle('needs-save', dirty() && !pending);
    el('profile-cancel').disabled = !dirty() || pending;
  }
  function changed() {
    const [width, height] = el('profile-resolution').value.split('x').map(Number);
    draft = {width, height, fps: Number(el('profile-fps').value), sample_rate: Number(el('profile-sample-rate').value)};
    el('profile-save-status').textContent = '';
    renderProfile();
  }
  el('profile-form').addEventListener('input', changed);
  el('profile-form').addEventListener('change', changed);
  el('profile-cancel').addEventListener('click', () => {
    if (pending) return;
    draft = {...saved};
    el('profile-save-status').textContent = '';
    renderProfile();
  });
  el('profile-form').addEventListener('submit', async event => {
    event.preventDefault();
    if (!canSave()) return;
    const data = new FormData();
    for (const [key, value] of Object.entries(draft)) data.set(key, String(value));
    pending = true;
    el('profile-save-status').textContent = 'Preparing BRB. The saved profile stays active until preparation succeeds.';
    render();
    try {
      const response = await fetch(`${location.origin}/api/brb/assets`, {method: 'POST', headers: {'X-Restreamer-Control': '1'}, body: data, signal: AbortSignal.timeout(180000)});
      if (!response.ok) throw new Error((await response.text()).trim() || 'Could not rebuild BRB. The previous profile remains active.');
      draft = null;
      el('profile-save-status').textContent = 'Profile saved. Video library preparation is queued; each video becomes playable when ready. Reconnect OBS with matching settings.';
    } catch (error) {
      el('profile-save-status').textContent = error.message;
    } finally {
      await refresh();
      pending = false;
      render();
    }
  });
  window.streamProfile = {
    isPending: () => pending,
    update(data, online, preparingAssets) {
      // Polling adopts externally saved profiles only if this form has no draft.
      const unchanged = !draft || equal(draft, saved);
      snapshot = data; connected = online; assetsBusy = preparingAssets;
      saved = data.brb_profile;
      if (unchanged && saved) draft = {...saved};
      renderProfile();
    }
  };
})();
