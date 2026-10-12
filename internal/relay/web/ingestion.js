/* Managed ingestion settings. Mount only in the desktop Settings workspace. */
(() => {
  'use strict';
  window.mountIngestionSettings = async (container) => {
    if (!container || container.dataset.ingestionMounted) return;
    container.dataset.ingestionMounted = 'true';
    container.innerHTML = `<h2>OBS ingestion</h2>
      <p>Copy the ingestion key into OBS. Keys are stored securely on this server.</p>
      <label>Stream key <input class="ingestion-key" type="password" value="••••••••••••••••" readonly autocomplete="off" aria-label="Ingestion stream key"></label>
      <div class="controls"><button type="button" data-ingestion="reveal">Reveal key</button><button type="button" data-ingestion="copy">Copy key</button><button type="button" data-ingestion="regenerate">Regenerate key…</button></div>
      <p class="ingestion-feedback" role="status" aria-live="polite"></p>`;
    const field = container.querySelector('.ingestion-key');
    const feedback = container.querySelector('.ingestion-feedback');
    const buttons = [...container.querySelectorAll('button')];
    let revision = '';
    const mask = () => { field.type = 'password'; field.value = '••••••••••••••••'; container.querySelector('[data-ingestion="reveal"]').textContent = 'Reveal key'; };
    const request = async (path, body) => {
      const options = {cache: 'no-store'};
      if (body) Object.assign(options, {method: 'POST', headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: JSON.stringify(body)});
      const response = await fetch(path, options);
      if (!response.ok) throw new Error(await response.text() || 'Ingestion request failed.');
      return response.json();
    };
    const refresh = async () => {
      const status = await request('/api/ingestion');
      revision = status.revision;
      buttons.forEach(button => { button.disabled = !status.managed; });
      feedback.textContent = status.managed ? 'Key hidden. OBS server path: /' + status.application : 'Managed ingestion is unavailable. Configure ingestion_key_file on the server to migrate.';
    };
    container.addEventListener('click', async (event) => {
      const button = event.target.closest('[data-ingestion]');
      if (!button || button.disabled) return;
      const action = button.dataset.ingestion;
      if (action === 'reveal' && field.type === 'text') { mask(); return; }
      if (action === 'regenerate' && !confirm('Regenerate the ingestion key? Update OBS with the new key before its next connection. The currently connected publisher stays connected until it disconnects. The old key will no longer authenticate new connections.')) return;
      buttons.forEach(item => { item.disabled = true; });
      try {
        if (action === 'regenerate') {
          const result = await request('/api/ingestion/regenerate', {confirm: true, revision});
          revision = result.revision;
          mask();
          feedback.textContent = 'Key regenerated. Copy the new key into OBS before reconnecting.';
        } else {
          const result = await request('/api/ingestion/key', {});
          revision = result.revision;
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
    try { await refresh(); } catch (error) { buttons.forEach(button => { button.disabled = true; }); feedback.textContent = error.message; }
  };
  document.querySelectorAll("[data-ingestion-settings]").forEach(window.mountIngestionSettings);
})();
