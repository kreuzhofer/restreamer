'use strict';
// Settings owns the mount point. All credentials stay in password inputs and
// are sent only on an explicit save; responses contain presence flags only.
window.mountDestinationSettings = function mountDestinationSettings(container) {
  if (!container || container.dataset.destinationsMounted) return;
  container.dataset.destinationsMounted = 'true';
  let snapshot, editing = '', busy = false, baseline = '';
  const dirty = () => JSON.stringify([name.value, url.value, key.value]) !== baseline;
  const node = (tag, text) => { const element = document.createElement(tag); if (text) element.textContent = text; return element; };
  const heading = node('h2', 'Destinations');
  const message = node('p'); message.setAttribute('role', 'status');
  message.setAttribute('aria-live', 'polite');
  message.textContent = 'Loading destination settings…';
  const list = node('div'); list.className = 'destination-settings-list';
  const form = node('form');
  const fields = node('fieldset'); fields.disabled = true; fields.append(node('legend', 'Add or edit a destination'));
  // Twitch servers come from its public discovery API. YouTube's preset follows
  // OBS's services.json. Kick supplies the URL in its dashboard.
  const presets = {
    twitch: {name: 'Twitch', url: ''},
    youtube: {name: 'YouTube', url: 'rtmps://a.rtmps.youtube.com:443/live2'},
    kick: {name: 'Kick', url: ''},
  };
  const platformLabel = node('label', 'Platform');
  const platform = node('select'); platform.name = 'destination_platform';
  for (const [value, label] of [['custom', 'Custom'], ['twitch', 'Twitch'], ['youtube', 'YouTube'], ['kick', 'Kick']]) {
    const option = node('option', label); option.value = value; platform.append(option);
  }
  platform.value = 'custom'; platformLabel.append(platform); fields.append(platformLabel);
  const input = (label, type, name) => {
    const wrapper = node('label', label);
    const field = node('input'); field.type = type; field.name = name; field.autocomplete = 'off'; field.spellcheck = false;
    wrapper.append(field); fields.append(wrapper); return field;
  };
  const name = input('Unique name', 'text', 'destination_name'); name.required = true; name.pattern = '[a-zA-Z0-9_-]{1,64}'; name.maxLength = 64;
  const url = input('RTMP or RTMPS server URL', 'url', 'destination_url'); url.required = true; url.maxLength = 2048;
  const key = input('Stream key', 'password', 'destination_key'); key.autocomplete = 'new-password'; key.maxLength = 2048;
  const platformHint = node('p'); platformHint.id = 'destination-platform-hint';
  platform.setAttribute('aria-describedby', platformHint.id); fields.append(platformHint);
  const serverLabel = node('label', 'Twitch server');
  const server = node('select'); server.name = 'twitch_ingest';
  serverLabel.append(server); serverLabel.hidden = true;
  const discover = node('button', 'Choose Twitch server'); discover.type = 'button'; discover.hidden = true;
  fields.append(serverLabel, discover);
  let discovery = 0;
  server.onchange = () => { if (server.value) url.value = server.value; };
  async function discoverTwitch({prefill = false} = {}) {
    const attempt = ++discovery;
    const startingURL = url.value;
    serverLabel.hidden = false; server.replaceChildren(); server.disabled = true;
    discover.hidden = false; discover.disabled = true;
    platformHint.textContent = 'Loading Twitch ingest servers…';
    try {
      const ingests = await request('/api/destination-presets/twitch');
      if (attempt !== discovery) return;
      if (!Array.isArray(ingests) || !ingests.length) throw new Error('Twitch returned no servers. Retry or enter a server URL manually.');
      const placeholder = node('option', 'Choose a server'); placeholder.value = ''; server.append(placeholder);
      for (const ingest of ingests) {
        const option = node('option', ingest.name); option.value = ingest.url; server.append(option);
      }
      server.disabled = false;
      if (prefill && url.value === startingURL) url.value = ingests[0].url;
      server.value = ingests.some(ingest => ingest.url === url.value) ? url.value : '';
      platformHint.textContent = 'Servers supplied by Twitch. Choose a region or edit the URL; your stream key stays separate.';
      discover.textContent = 'Refresh Twitch servers';
    } catch (error) {
      if (attempt === discovery) {
        platformHint.textContent = error.message;
        discover.textContent = 'Retry Twitch servers';
      }
    } finally {
      if (attempt === discovery) discover.disabled = false;
    }
  }
  discover.onclick = () => discoverTwitch();
  platform.onchange = async () => {
    if (editing) return;
    discovery++; serverLabel.hidden = true; discover.hidden = true;
    const preset = presets[platform.value];
    if (preset) { name.value = preset.name; url.value = preset.url; key.value = ''; }
    platformHint.textContent = platform.value === 'kick'
      ? 'Copy your server URL and stream key from Kick Creator Dashboard → Channel → Stream URL and Key.'
      : 'You can edit the name and server URL. Paste the stream key from your platform dashboard.';
    if (platform.value === 'twitch') await discoverTwitch({prefill: true});
  };
  const hint = node('p', 'New destinations start disabled. Enable delivery in Live after checking readiness.');
  const save = node('button', 'Add destination'); save.type = 'submit'; save.className = 'primary-button';
  const cancel = node('button', 'Cancel edit'); cancel.type = 'button'; cancel.hidden = true;
  fields.append(hint, save, cancel); form.append(fields);
  const retry = node('button', 'Refresh destinations'); retry.type = 'button'; retry.className = 'text-button';
  const add=node('button','Add destination');add.type='button';add.className='primary-button';add.onclick=()=>{form.hidden=false;name.focus();};
  const headingRow=node('div');headingRow.className='destination-heading';headingRow.append(heading,add);
  form.hidden=true;container.replaceChildren(headingRow,message,list,form,retry);
  baseline = JSON.stringify([name.value, url.value, key.value]);
  window.addEventListener('beforeunload', event => {
    if (!window.workspace?.departureApproved && (dirty() || busy)) { event.preventDefault(); event.returnValue = ''; }
  });
  async function request(path, method = 'GET', body) {
    const response = await fetch(location.origin + path, {method, cache: 'no-store', signal: AbortSignal.timeout(10000), headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, ...(body ? {body: JSON.stringify(body)} : {})});
    if (!response.ok) throw new Error(await response.text());
    return response.json();
  }
  function reset() {
    editing = ''; discovery++; serverLabel.hidden = true; discover.hidden = true; form.hidden = true; form.reset(); cancel.hidden = true; save.textContent = 'Add destination'; key.placeholder = '';
    platform.value = 'custom'; platform.disabled = false; platformLabel.hidden = false; platformHint.textContent = '';
    hint.textContent = 'New destinations start disabled. Enable delivery in Live after checking readiness.';
    baseline = JSON.stringify([name.value, url.value, key.value]);
  }
  function render() {
    list.replaceChildren(); retry.disabled = busy; fields.disabled = busy || !snapshot.available || (!snapshot.managed && snapshot.destinations.length > 0);
    if (!snapshot.available) message.textContent = 'Destination storage is not configured. Configure a persistent destinations_file.';
    if (!snapshot.managed && snapshot.destinations.length) {
      const migrate = node('button', 'Import existing destinations'); migrate.type = 'button'; migrate.disabled = busy;
      migrate.onclick = () => mutate('/api/destinations/import', 'POST', {}, 'Import legacy destinations and their current enabled preferences? Future restarts will use these saved values.');
      list.append(node('p', 'Import existing configuration once before editing. Stop broadcasting or rehearsal first.'), migrate);
    }
    for (const destination of snapshot.destinations) {
      const row = node('article');
      const info=node('div');info.className='destination-info';
      info.append(node('h3', destination.name), node('p', destination.url), node('p', `${destination.has_key ? 'Key stored ••••••••' : 'No key'} · ${destination.ready ? 'Ready' : destination.reason}`));
      const edit = node('button', 'Edit'); edit.type = 'button'; edit.className='preview-button'; edit.disabled = busy || !snapshot.managed;
      edit.onclick = () => {
        if (dirty() && !window.confirm('Discard the unsaved destination draft and edit this destination?')) return;
        editing = destination.name;form.hidden=false;discovery++;serverLabel.hidden=true; name.value = destination.name; url.value = destination.url; key.value = '';
        platform.disabled = true; platformLabel.hidden = true; platformHint.textContent = '';
        discover.hidden = !/\.(?:live-video\.net|twitch\.tv)(?::[0-9]+)?\//.test(destination.url);
        discover.disabled = false; discover.textContent = 'Choose Twitch server';
        key.placeholder = destination.has_key ? 'Leave blank to keep stored key' : 'Enter stream key';
        hint.textContent = 'Leaving the key blank preserves the stored key. Saving an active destination reconnects only that destination.';
        save.textContent = 'Save destination'; cancel.hidden = false; baseline = JSON.stringify([name.value, url.value, key.value]); name.focus();
      };
      const remove = node('button', 'Remove'); remove.type = 'button'; remove.className='text-button danger-text'; remove.disabled = busy || !snapshot.managed;
      remove.onclick = () => mutate(`/api/destinations/${encodeURIComponent(destination.name)}`, 'DELETE', {}, `Remove ${destination.name} and its stored key? Its delivery will stop. Removing the last destination leaves the show and preview running without delivery.`);
      const actions=node('div');actions.className='destination-actions';actions.append(edit,remove);row.append(info,actions); list.append(row);
    }
    if (!snapshot.destinations.length) list.append(node('p', 'No destinations yet. Add one below, or use preview-only rehearsal in Live.'));
  }
  async function refresh() { snapshot = await request('/api/destinations'); render(); if (message.textContent === 'Loading destination settings…') message.textContent = 'Destination credentials stay private. Configure here; control delivery in Live.'; }
  async function mutate(path, method, values, confirmation) {
    if (busy) return;
    busy = true; render();
    let saved = false;
    try {
      const stage = await request('/api/stage');
      if (confirmation && !window.confirm(confirmation + (method === 'DELETE' && editing && path.endsWith('/' + encodeURIComponent(editing)) && dirty() ? ' This also discards your unsaved edits for this destination.' : ''))) return;
      if (method === 'PUT' && stage.mode === 'real' && stage.stage !== 'OFF' && !window.confirm('Saving disconnects this destination and reconnects it with the new settings. Other outputs continue. Apply to the current broadcast?')) return;
      busy = true; render(); message.textContent = 'Saving…';
      snapshot = await request(path, method, {version: snapshot.version, ...values, confirmed: true, server_id: stage.server_id, context: stage.context});
      if (method === 'PUT' || method === 'POST' || (editing && path.endsWith('/' + encodeURIComponent(editing)))) reset();
      message.textContent = 'Destinations saved.';
      saved = true; window.dispatchEvent(new Event('destinations-changed'));
    } catch (error) {
      message.textContent = error.name === 'TimeoutError' || error.name === 'TypeError' ? 'Could not confirm the change. Review the current destination list before retrying; your draft is retained.' : error.message;
      // Preserve the draft for correction, but require a fresh reviewed list.
      try { snapshot = await request('/api/destinations'); } catch (_) { /* Keep the explicit request error visible. */ }
    } finally { busy = false; if (snapshot) render(); if (saved) name.focus(); }
  }
  form.onsubmit = event => {
    event.preventDefault();
    const values = {name: name.value.trim(), url: url.value.trim()};
    if (key.value || !editing) values.stream_key = key.value;
    mutate(editing ? `/api/destinations/${encodeURIComponent(editing)}` : '/api/destinations', editing ? 'PUT' : 'POST', values);
  };
  cancel.onclick = () => { reset(); name.focus(); };
  retry.onclick = () => refresh().then(() => { message.textContent = 'Destinations refreshed. Your draft is retained.'; }).catch(error => { message.textContent = error.message + ' Try Refresh destinations again.'; });
  refresh().catch(error => { fields.disabled = true; message.textContent = error.message + ' Use Refresh destinations to retry.'; });
  return {refresh};
};

document.querySelectorAll("[data-destination-settings]").forEach(window.mountDestinationSettings);
