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
  const input = (label, type, name) => {
    const wrapper = node('label', label);
    const field = node('input'); field.type = type; field.name = name; field.autocomplete = 'off'; field.spellcheck = false;
    wrapper.append(field); fields.append(wrapper); return field;
  };
  const name = input('Unique name', 'text', 'destination_name'); name.required = true; name.pattern = '[a-zA-Z0-9_-]{1,64}'; name.maxLength = 64;
  const url = input('RTMP or RTMPS server URL', 'url', 'destination_url'); url.required = true; url.maxLength = 2048;
  const key = input('Stream key', 'password', 'destination_key'); key.autocomplete = 'new-password'; key.maxLength = 2048;
  const hint = node('p', 'New destinations start disabled. Enable delivery in Live after checking readiness.');
  const save = node('button', 'Add destination'); save.type = 'submit';
  const cancel = node('button', 'Cancel edit'); cancel.type = 'button'; cancel.hidden = true;
  fields.append(hint, save, cancel); form.append(fields);
  const retry = node('button', 'Refresh destinations'); retry.type = 'button'; retry.className = 'preview-button';
  container.replaceChildren(heading, message, retry, list, form);
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
    editing = ''; form.reset(); cancel.hidden = true; save.textContent = 'Add destination'; key.placeholder = '';
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
      row.append(node('h3', destination.name), node('p', destination.url), node('p', `${destination.has_key ? 'Key stored ••••••••' : 'No key'} · ${destination.ready ? 'Ready' : destination.reason}`));
      const edit = node('button', 'Edit'); edit.type = 'button'; edit.disabled = busy || !snapshot.managed;
      edit.onclick = () => {
        if (dirty() && !window.confirm('Discard the unsaved destination draft and edit this destination?')) return;
        editing = destination.name; name.value = destination.name; url.value = destination.url; key.value = '';
        key.placeholder = destination.has_key ? 'Leave blank to keep stored key' : 'Enter stream key';
        hint.textContent = 'Leaving the key blank preserves the stored key. Saving an active destination reconnects only that destination.';
        save.textContent = 'Save destination'; cancel.hidden = false; baseline = JSON.stringify([name.value, url.value, key.value]); name.focus();
      };
      const remove = node('button', 'Remove'); remove.type = 'button'; remove.disabled = busy || !snapshot.managed;
      remove.onclick = () => mutate(`/api/destinations/${encodeURIComponent(destination.name)}`, 'DELETE', {}, `Remove ${destination.name} and its stored key? Its delivery will stop. Removing the last destination leaves the show and preview running without delivery.`);
      row.append(edit, remove); list.append(row);
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
