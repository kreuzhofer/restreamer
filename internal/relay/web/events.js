'use strict';
(() => {
  const $ = id => document.getElementById(id);
  const node = (tag, text) => { const element = document.createElement(tag); element.textContent = text; return element; };
  let busy = false, pending;
  const dialog = $('archive-dialog');
  async function request(path, method = 'GET', body) {
    const response = await fetch(path, {method, cache: 'no-store', signal: AbortSignal.timeout(10000), headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, ...(body ? {body: JSON.stringify(body)} : {})});
    if (!response.ok) throw new Error(await response.text());
    return response.status === 204 ? null : response.json();
  }
  function action(event, kind) {
    pending = {event, kind}; $('archive-error').textContent = '';
    $('archive-password').value = '';
    $('archive-password').hidden = $('archive-password-label').hidden = kind !== 'password';
    const incomplete = event.incomplete && kind === 'publish';
    $('archive-dialog-title').textContent = kind === 'delete' ? 'Delete event permanently?' : kind === 'password' ? 'Change event password' : kind === 'publish' ? (incomplete ? 'Publish this incomplete recording?' : 'Publish replay?') : 'Withdraw replay?';
    $('archive-dialog-description').textContent = kind === 'delete' ? 'Delete ' + event.name + ', its recording, and its public URL. This cannot be undone.' : kind === 'password' ? 'This changes access to both live viewing and replay. Existing viewers must authenticate again.' : kind === 'publish' ? (incomplete ? 'This recording was interrupted and may have gaps. Publishing makes the playable portion available at the original event URL.' : 'Viewers can watch the recording using the original event URL and its current password protection.') : 'New viewer requests will be blocked. Your recording remains in the archive; downloaded media cannot be recalled.';
    $('archive-confirm').textContent = kind === 'delete' ? 'Delete event' : kind === 'password' ? 'Save password' : kind === 'publish' ? 'Publish replay' : 'Withdraw replay';
    dialog.showModal(); (kind === 'password' ? $('archive-password') : $('archive-cancel')).focus();
  }
  async function refresh() {
    if (busy) return;
    busy = true; $('archive-refresh').disabled = true;
    try {
      const result = await request('/api/events'); $('archive-list').replaceChildren();
      if (!result.available) { $('archive-status').textContent = result.error || 'Event storage is not configured. Set up the persistent events directory before creating an event.'; return; }
      const events = result.events.sort((a, b) => a.name.localeCompare(b.name));
      $('archive-status').textContent = events.length ? events.length + ' events · recordings stay until you delete them' : 'No events yet. Create a public streaming page in Settings → Destinations.';
      for (const event of events) {
        const row = node('article', ''); row.className = 'archive-event';
        const details = node('div', ''); details.append(node('h2', event.name));
        const state = event.state === 'deleted' ? 'Deletion needs cleanup' : event.incomplete ? 'Incomplete recording' : event.state === 'ended' ? 'Event ended' : event.state === 'live' ? 'Broadcast in progress' : 'Waiting to start';
        details.append(node('p', state + ' · ' + (event.published ? 'Replay published' : 'Replay private') + ' · ' + (event.protected ? 'Password protected' : 'Anyone with the link')));
        const size = node('p', (event.bytes / (1024 * 1024)).toFixed(1) + ' MiB recorded'); size.className = 'archive-size'; details.append(size);
        if (event.error) { const error = node('p', event.error); error.className = 'archive-error'; details.append(error); }
        const actions = node('div', ''); actions.className = 'archive-actions';
        const button = (text, handler) => { const control = node('button', text); control.type = 'button'; control.onclick = handler; actions.append(control); return control; };
        if (event.state !== 'deleted' && !event.unavailable) {
          button('Copy event link', async () => { try { await navigator.clipboard.writeText(new URL(event.url, location.origin).href); $('archive-status').textContent = 'Link copied for ' + event.name + '.'; } catch (_) { $('archive-status').textContent = 'Copy this link: ' + new URL(event.url, location.origin).href; } });
          if (event.recorded) { const preview = node('a', 'Preview recording'); preview.href = '/events/' + event.id; preview.target = '_blank'; preview.rel = 'noopener'; actions.append(preview); }
          if (event.recorded && event.state !== 'live' && !event.finalizing) button(event.published ? 'Withdraw replay' : 'Publish replay', () => action(event, event.published ? 'withdraw' : 'publish'));
          button('Change password', () => action(event, 'password'));
        }
        const remove = button('Delete event', () => action(event, 'delete')); remove.className = 'danger'; remove.disabled = event.state === 'live';
        row.append(details, actions); $('archive-list').append(row);
      }
    } catch (error) { $('archive-status').textContent = error.message + ' Use Refresh archive to retry.'; }
    finally { busy = false; $('archive-refresh').disabled = false; }
  }
  $('archive-form').onsubmit = async event => {
    event.preventDefault(); if (!pending) return;
    const {event: item, kind} = pending; $('archive-confirm').disabled = $('archive-cancel').disabled = true;
    try {
      const body = {version: item.version};
      if (kind === 'password') body.password = $('archive-password').value;
      else if (kind === 'delete') body.confirmed = true;
      else { body.published = kind === 'publish'; body.acknowledge_incomplete = item.incomplete && kind === 'publish'; }
      await request('/api/events/' + item.id, kind === 'delete' ? 'DELETE' : 'PATCH', body);
      $('archive-password').value = ''; dialog.close(); pending = null; await refresh();
    } catch (error) { $('archive-error').textContent = error.message + ' Close this dialog and refresh before retrying.'; }
    finally { $('archive-confirm').disabled = $('archive-cancel').disabled = false; }
  };
  $('archive-cancel').onclick = () => { $('archive-password').value = ''; dialog.close(); pending = null; };
  $('archive-refresh').onclick = refresh;
  refresh();
})();
