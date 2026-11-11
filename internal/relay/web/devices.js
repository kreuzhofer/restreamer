'use strict';
(() => {
  if (location.pathname !== '/settings') return;
  const $ = id => document.getElementById(id);
  let approved = false, busy = false;
  const code = () => new URLSearchParams(location.hash.split('?')[1] || '').get('code') || '';
  const message = (text, error = false) => { $('devices-message').textContent = text; $('devices-message').classList.toggle('error', error); };
  async function api(path, method = 'GET', body) {
    const response = await fetch(path, {method, credentials:'same-origin', cache:'no-store', headers:{'Content-Type':'application/json','X-Restreamer-Control':'1'}, body:body ? JSON.stringify(body) : undefined});
    if (!response.ok) {
      let detail = 'Could not update devices. Check your connection and try again.';
      try { detail = (await response.json()).error || detail; } catch {}
      throw new Error(detail);
    }
    return response.json();
  }
  async function refresh() {
    try {
      const data = await api('/api/devices?code=' + encodeURIComponent(code()));
      $('devices-list').replaceChildren();
      for (const device of data.devices) {
        const row = document.createElement('div'), details = document.createElement('div'), name = document.createElement('strong'), created = document.createElement('p'), revoke = document.createElement('button');
        row.className = 'device-row'; name.textContent = device.name;
        created.textContent = 'Connected ' + new Date(device.created).toLocaleString() + (device.control_destinations ? ' · Broadcast and destination control' : ' · Broadcast control');
        revoke.className = 'preview-button'; revoke.textContent = 'Revoke'; revoke.setAttribute('aria-label','Revoke access for ' + device.name);
        revoke.addEventListener('click', async () => {
          if (!window.confirm(`Revoke access for ${device.name}? Its buttons will stop controlling Restreamer. Your current broadcast keeps running.`)) return;
          revoke.disabled = true;
          try { await api('/api/devices/' + encodeURIComponent(device.id), 'DELETE', {confirm:true}); await refresh(); message('Device access revoked.'); }
          catch (error) { message(error.message,true); revoke.disabled = false; }
        });
        details.append(name,created); row.append(details,revoke); $('devices-list').append(row);
      }
      const pending = data.pending && !data.pending.approved && !approved;
      $('device-approval').hidden = !pending;
      if (pending) { $('device-pair-name').textContent = data.pending.name; $('device-pair-code').textContent = data.pending.code; }
      message(!data.enabled ? 'Device pairing requires persistent storage. Configure TARGET_STATE_FILE and restart Restreamer.' : approved || data.pending?.approved ? 'Approved. Return to Stream Deck to finish connecting.' : code() && !data.pending ? 'This code has expired or was already used. Start again in Stream Deck if it is not connected.' : data.devices.length ? 'Each device has separate access. Revoking one does not change your password or stop the broadcast.' : 'No connected devices yet. Start with Connect in browser in your Stream Deck plugin.');
    } catch (error) { $('device-approval').hidden = true; message(error.message,true); }
  }
  $('device-code-matches').addEventListener('change', () => { $('device-approve').disabled = busy || !$('device-code-matches').checked; });
  async function decide(allow) {
    if (busy) return;
    busy = true; $('device-approve').disabled = true; $('device-decline').disabled = true;
    try {
      await api('/api/devices/' + (allow ? 'approve' : 'deny'), 'POST', {user_code:code(),confirm:true,...(allow?{control_destinations:$('device-control-destinations').checked}:{})});
      approved = allow;
      $('device-approval').hidden = true;
      await refresh();
      if (!allow) message('Connection declined. No access was granted.');
    } catch (error) { message(error.message,true); }
    finally { busy = false; $('device-approve').disabled = !$('device-code-matches').checked; $('device-decline').disabled = false; }
  }
  $('device-approve').addEventListener('click', () => void decide(true));
  $('device-decline').addEventListener('click', () => void decide(false));
  $('devices-refresh').addEventListener('click', () => void refresh());
  function navigate() { approved = false; $('device-control-destinations').checked = false; $('device-code-matches').checked = false; $('device-approve').disabled = true; document.body.toggleAttribute('data-device-pairing',location.hash.startsWith('#devices')); void refresh(); }
  window.addEventListener('hashchange',navigate); window.addEventListener('popstate',navigate);
  window.addEventListener('settings-section-changed',navigate);
  navigate();
})();
