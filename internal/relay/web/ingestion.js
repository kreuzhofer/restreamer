/* Managed ingestion settings. Mount only in the desktop Settings workspace. */
(() => {
  'use strict';
  window.mountIngestionSettings = async (container) => {
    if (!container || container.dataset.ingestionMounted) return;
    container.dataset.ingestionMounted = 'true';
    container.innerHTML = `<h2>OBS ingestion</h2>
      <p>In OBS, select Settings → Stream → Custom. Enter the server address and stream key separately.</p>
      <label>OBS server address <input class="ingestion-address" type="text" autocomplete="off" spellcheck="false" aria-describedby="ingestion-address-help" placeholder="Loading application path…"></label>
      <p id="ingestion-address-help" class="ingestion-address-help">Use your RTMP server’s public hostname and published RTMP port. These can differ from this dashboard’s address.</p>
      <button class="preview-button" type="button" data-ingestion="address">Copy server address</button>
      <label>Stream key <input class="ingestion-key" type="password" value="••••••••••••••••" readonly autocomplete="off" aria-label="Ingestion stream key"></label>
      <div class="controls"><button class="preview-button" type="button" data-ingestion="reveal">Reveal key</button><button class="preview-button" type="button" data-ingestion="copy">Copy key</button><button class="preview-button" type="button" data-ingestion="regenerate">Regenerate key…</button></div>
      <p class="ingestion-feedback" role="status" aria-live="polite"></p>`;
    const field = container.querySelector('.ingestion-key');
    const feedback = container.querySelector('.ingestion-feedback');
    const buttons = [...container.querySelectorAll('button')];
    buttons.forEach(button => { button.disabled = true; });
    feedback.textContent = 'Loading ingestion settings…';
    let revision = '', application = '', visibilityEpoch = 0;
    const address = container.querySelector('.ingestion-address');
    const mask = () => { if (field.type === 'text') feedback.textContent = 'Key hidden.'; visibilityEpoch++; field.type = 'password'; field.value = '••••••••••••••••'; container.querySelector('[data-ingestion="reveal"]').textContent = 'Reveal key'; };
    const request = async (path, body) => {
      const options = {cache: 'no-store', signal: AbortSignal.timeout(10000)};
      if (body) Object.assign(options, {method: 'POST', headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: JSON.stringify(body)});
      const response = await fetch(location.origin + path, options);
      if (!response.ok) throw new Error(await response.text() || 'Ingestion request failed.');
      return response.json();
    };
    const refresh = async () => {
      const status = await request('/api/ingestion');
      revision = status.revision;
      application = status.application;
      address.placeholder = `rtmp://YOUR_SERVER:YOUR_PUBLISHED_PORT/${application}`;
      buttons.forEach(button => { button.disabled = !status.managed; });
      feedback.textContent = status.managed ? 'Key hidden. Keep the server address separate from your stream key.' : 'Managed ingestion is unavailable. Configure ingestion_key_file on the server to migrate.';
    };
    container.addEventListener('click', async (event) => {
      const button = event.target.closest('[data-ingestion]');
      if (!button || button.disabled) return;
      const action = button.dataset.ingestion;
      if (action === 'reveal' && field.type === 'text') { mask(); return; }
      if (action === 'regenerate' && !confirm('Regenerate the ingestion key? Update OBS with the new key before its next connection. The currently connected publisher stays connected until it disconnects. The old key will no longer authenticate new connections.')) return;
      const requestEpoch = visibilityEpoch;
      buttons.forEach(item => { item.disabled = true; });
      try {
        if (action === 'address') {
          const url = new URL(address.value.trim());
          if (url.protocol !== 'rtmp:' || !url.hostname || !url.port || Number(url.port) > 65535 || Number(url.port) < 1 || url.username || url.password || url.search || url.hash || url.pathname !== '/' + application) throw new Error('Enter rtmp://, your RTMP hostname, the published port, and /' + application + '. Keep the key separate.');
          if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable. Select the server address and copy it manually.');
          await navigator.clipboard.writeText(address.value.trim());
          feedback.textContent = 'OBS server address copied.';
        } else if (action === 'regenerate') {
          const result = await request('/api/ingestion/regenerate', {confirm: true, revision});
          revision = result.revision;
          mask();
          feedback.textContent = 'Key regenerated. Copy the new key into OBS before reconnecting.';
        } else {
          const result = await request('/api/ingestion/key', {});
          revision = result.revision;
          if (requestEpoch !== visibilityEpoch) return;
          if (action === 'reveal') {
            field.value = result.key; field.type = 'text'; button.textContent = 'Hide key';
            feedback.textContent = 'Key revealed. Hide it when finished.';
          } else {
            if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable. Use Reveal key and copy it manually.');
            await navigator.clipboard.writeText(result.key);
            feedback.textContent = 'Ingestion key copied.';
          }
        }
      } catch (error) {
        mask();
        feedback.textContent = action === 'copy' ? 'Could not copy the key. Use Reveal key and copy it manually.' : error.message;
        if (action === 'regenerate') {
          try { const status = await request('/api/ingestion'); revision = status.revision; } catch (_) { /* Keep the explicit error above. */ }
        }
      } finally { buttons.forEach(item => { item.disabled = false; }); }
    });
    document.addEventListener('visibilitychange', () => { if (document.hidden) mask(); });
    window.addEventListener('blur', mask);
    const workspaceObserver = new MutationObserver(() => {
      if (document.body.dataset.workspace !== 'settings') mask();
    });
    workspaceObserver.observe(document.body, {attributes: true, attributeFilter: ['data-workspace']});
    try { await refresh(); } catch (error) { buttons.forEach(button => { button.disabled = true; }); feedback.textContent = error.message + " Reload this page to retry."; }
  };
  document.querySelectorAll("[data-ingestion-settings]").forEach(window.mountIngestionSettings);
})();
