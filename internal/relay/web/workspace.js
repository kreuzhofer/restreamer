'use strict';
(() => {
  const el = id => document.getElementById(id);
  const onDashboard = !!el('live-workspace');
  let lastUpdate = 0, online = false, snapshot;
  function navigate(focus = false) {
    const view = location.pathname === '/settings' ? 'settings' : onDashboard ? 'live' : 'studio';
    document.body.dataset.workspace = view;
    document.querySelectorAll('[data-workspace]').forEach(link => {
      if (link.dataset.workspace === view) link.setAttribute('aria-current', 'page');
      else link.removeAttribute('aria-current');
    });
    document.querySelectorAll('[data-workspace-panel]').forEach(panel => { panel.hidden = panel.dataset.workspacePanel !== view; });
    document.title = `Restreamer · ${view[0].toUpperCase() + view.slice(1)}`;
    if (focus) {
      const heading = document.querySelector(`[data-workspace-panel="${view}"] h1`);
      heading?.setAttribute('tabindex', '-1');
      heading?.focus();
    }
  }
  document.querySelectorAll('.workspace-nav a, .workspace-alert-link').forEach(link => link.addEventListener('click', event => {
    if (!onDashboard || link.dataset.workspace === 'studio' || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
    event.preventDefault();
    history.pushState(null, '', link.href);
    navigate(true);
  }));
  window.addEventListener('popstate', () => navigate(true));
  navigate();

  function alerts(stale) {
    const stage = snapshot?.stage;
    const messages = [];
    let severity = 'info';
    if (stale) {
      messages.push(lastUpdate ? 'Status is stale. Last known values do not confirm current delivery. Reconnecting automatically.' : 'Connecting to broadcast status. Live state is not available yet.');
      severity = 'warning';
    } else if (stage) {
      if (stage.error) { messages.push(stage.error); severity = 'error'; }
      if (stage.stage !== 'OFF' && stage.mode === 'real') {
        const eligible = (snapshot.outputs || []).filter(output => output.enabled && output.can_enable);
        const failed = eligible.filter(output => output.state === 'retrying' || (output.last_error && ['connecting', 'waiting_for_keyframe'].includes(output.state)));
        if (!eligible.length) { messages.push('No eligible destination is enabled. The selected feed is not being delivered.'); if (severity !== 'error') severity = 'warning'; }
        else if (failed.length) {
          messages.push(`${failed.length === eligible.length ? 'All enabled destinations are failing' : `${failed.length} of ${eligible.length} destinations failing`}: ${failed.map(output => output.name).join(', ')}. Reconnecting independently.`);
          severity = severity === 'error' || failed.length === eligible.length ? 'error' : 'warning';
        }
      }
      if (stage.stage === 'LIVE' && stage.source !== 'obs') {
        messages.push(stage.source === 'brb' ? 'OBS is unavailable. Fallback BRB is on air; OBS recovery is automatic.' : 'LIVE is selected but OBS is not on air. Check the source in Live.');
        if (severity !== 'error') severity = 'warning';
      }
      if (stage.pending) messages.push(`Transition pending: ${stage.pending.reason || 'waiting for the requested source'}. The current source remains selected.`);
    }
    const banner = el('workspace-alert');
    banner.hidden = !messages.length;
    banner.classList.toggle('error', severity === 'error');
    // Polling should not announce an unchanged alert again to assistive tech.
    const message = messages.join(' ');
    if (el('workspace-alert-message').textContent !== message) el('workspace-alert-message').textContent = message;
  }
  function freshness() {
    const age = lastUpdate ? Math.floor((Date.now() - lastUpdate) / 1000) : null;
    const stale = !online || age === null || age > 10;
    el('workspace-status').classList.toggle('stale', stale);
    alerts(stale);
    el('workspace-freshness').textContent = age === null ? 'Awaiting first update' : stale ? `Status stale · last received ${age}s ago` : 'Status current';
  }
  function update(data, connected) {
    online = connected;
    if (data) snapshot = data;
    if (connected && data) lastUpdate = Date.now();
    if (data?.stage) {
      const stage = data.stage;
      el('workspace-stage').textContent = stage.stage || 'Unknown';
      const sources = {off: 'None', obs: 'OBS', file: stage.media?.name || 'Prepared video', brb: stage.stage === 'BRB' ? 'Deliberate BRB' : 'Fallback BRB'};
      el('workspace-source').textContent = sources[stage.source] || 'Unknown';
      const rehearsal = stage.mode === 'preview_only' || stage.pending?.mode === 'preview_only';
      el('workspace-mode').textContent = rehearsal ? 'REHEARSAL · destination delivery blocked' : stage.stage === 'OFF' ? 'OFF · delivery stopped' : 'Broadcast · relay delivery';
      el('workspace-status').classList.toggle('rehearsal', rehearsal);
    }
    window.brbMedia?.update(snapshot, connected);
    freshness();
  }
  window.workspace = {update};
  setInterval(freshness, 1000);
  // Live and Settings reuse the dashboard's existing status request. Studio has
  // its own read-only subscription; navigation never sends broadcast commands.
  async function poll() {
    try {
      const response = await fetch(`${location.origin}/api/dashboard`, {cache: 'no-store', signal: AbortSignal.timeout(8000)});
      if (!response.ok) throw new Error('Status unavailable');
      update(await response.json(), true);
      el('connection').textContent = 'Connected';
      el('connection-dot').className = 'dot online';
    } catch {
      update(null, false);
      el('connection').textContent = 'Connection lost · retrying';
      el('connection-dot').className = 'dot offline';
    } finally { setTimeout(poll, 2000); }
  }
  if (!onDashboard) poll();
})();
