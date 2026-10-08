'use strict';
(() => {
  const video = document.querySelector('#preview-video');
  const status = document.querySelector('#preview-status');
  const toggle = document.querySelector('#preview-toggle');
  let enabled = true, publishing = false, controller, retryAt = 0;
  const MediaSourceClass = window.MediaSource;

  function stop() {
    controller?.abort();
    video.pause();
    video.removeAttribute('src');
    video.load();
  }
  function event(target, name, signal, action) {
    return new Promise((resolve, reject) => {
      const finish = error => {
        clearTimeout(timer);
        target.removeEventListener(name, success);
        target.removeEventListener('error', failure);
        signal.removeEventListener('abort', aborted);
        error ? reject(error) : resolve();
      };
      const success = () => finish();
      const failure = () => finish(new Error('Browser could not decode this preview. Check the input codec/profile.'));
      const aborted = () => finish(new DOMException('Stopped', 'AbortError'));
      const timer = setTimeout(() => finish(new Error('Preview playback stalled. Retrying…')), 8000);
      target.addEventListener(name, success, {once: true});
      target.addEventListener('error', failure, {once: true});
      signal.addEventListener('abort', aborted, {once: true});
      if (signal.aborted) { aborted(); return; }
      try { action?.(); } catch (error) { finish(error); }
    });
  }
  async function start() {
    const current = new AbortController();
    controller = current;
    const signal = current.signal;
    let objectURL, reader;
    let timeout = setTimeout(() => current.abort(new Error('Preview timed out. Check input and proxy buffering.')), 15000);
    status.textContent = 'Connecting · waiting for a video keyframe…';
    try {
      const response = await fetch(`${location.origin}/api/preview`, {signal, cache: 'no-store'});
      if (!response.ok) throw new Error((await response.text()).trim() || 'Preview unavailable. Retrying…');
      const mime = response.headers.get('X-Preview-Codecs');
      if (!mime || !MediaSourceClass.isTypeSupported(mime)) {
        throw new Error('This browser cannot play the input codec/profile. Try a browser with H.264/AAC support.');
      }
      clearTimeout(timeout);
      const source = new MediaSourceClass();
      objectURL = URL.createObjectURL(source);
      await event(source, 'sourceopen', signal, () => { video.src = objectURL; });
      const buffer = source.addSourceBuffer(mime);
      reader = response.body.getReader();
      let playing = false;
      while (!signal.aborted) {
        timeout = setTimeout(() => current.abort(new Error('Preview stream stalled. Retrying…')), 15000);
        const {done, value} = await reader.read();
        clearTimeout(timeout);
        if (done) throw new Error('Input preview disconnected. Retrying…');
        await event(buffer, 'updateend', signal, () => buffer.appendBuffer(value));
        if (buffer.buffered.length) {
          const end = buffer.buffered.end(buffer.buffered.length - 1);
          if (playing && !video.paused) {
            const lastStart = buffer.buffered.start(buffer.buffered.length - 1);
            if (end - video.currentTime > 4) video.currentTime = Math.max(lastStart, end - 1.5);
            // Join the first playable range and cross timestamp gaps in live input.
            for (let i = 0; i < buffer.buffered.length; i++) {
              if (video.currentTime < buffer.buffered.start(i)) {
                if (end - video.currentTime > 1 && buffer.buffered.start(i) - video.currentTime > 0.05) video.currentTime = buffer.buffered.start(i);
                break;
              }
              if (video.currentTime < buffer.buffered.end(i)) break;
            }
          }

          // Bound browser memory even when the native player's pause button is used.
          if (end > 25 && buffer.buffered.start(0) < end - 25) {
            await event(buffer, 'updateend', signal, () => buffer.remove(0, end - 20));
          }
          if (!playing && end - buffer.buffered.start(0) >= 1) {
            video.currentTime = buffer.buffered.start(0);
            playing = true;
            video.play().catch(() => { if (!signal.aborted) status.textContent = 'Preview ready · press Play in the player.'; });
          }
        }
      }
    } catch (error) {
      if (enabled && publishing) {
        status.textContent = signal.reason instanceof Error && signal.reason.name !== 'AbortError' ? signal.reason.message : error.message;
      }
    } finally {
      clearTimeout(timeout);
      current.abort();
      try { await reader?.cancel(); } catch { /* Connection already closed. */ }
      if (controller === current) {
        controller = undefined;
        video.pause(); video.removeAttribute('src'); video.load();
        retryAt = Date.now() + 3000;
      }
      if (objectURL) URL.revokeObjectURL(objectURL);
    }
  }
  video.addEventListener('playing', () => { status.textContent = 'Live input preview'; });
  video.addEventListener('waiting', () => { if (controller) status.textContent = 'Buffering input preview…'; });
  window.updatePreview = active => {
    publishing = active;
    if (!MediaSourceClass) { status.textContent = 'Preview needs a browser with Media Source Extensions support.'; toggle.disabled = true; return; }
    if (!enabled || !publishing) {
      if (controller) stop();
      status.textContent = enabled ? 'Waiting for OBS input' : 'Preview paused · forwarding is unaffected';
    } else if (!controller && Date.now() >= retryAt) start();
  };
  toggle.addEventListener('click', () => {
    enabled = !enabled;
    toggle.setAttribute('aria-pressed', String(enabled));
    toggle.textContent = enabled ? 'Pause preview' : 'Start preview';
    retryAt = 0;
    window.updatePreview(publishing);
  });
  window.addEventListener('pagehide', stop);
})();
