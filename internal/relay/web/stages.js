'use strict';
(() => {
  const el = id => document.getElementById(id);
  const commandID = () => Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, '0')).join('');
  const time = seconds => { const value = Math.max(0, Math.floor(seconds || 0)); return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, '0')}`; };
  let snapshot, online = false, refreshing = false, review, inflight, lastCommand, lastOutcome, resolving = false, message = '', serverID;
  let selections = {prestream: '', ending: '', shortcuts: []}, revisions = [];
  const current = () => snapshot?.stage;
  const name = revision => revisions.find(item => item.id === revision)?.name || revision || 'No media selected';
  const label = media => media?.name ? `${media.name} · revision ${media.revision}` : media?.revision || 'No prepared media';
  const locked = () => current()?.stage === 'ENDING' && !current()?.error;
  const sourceName = stage => ({off: 'Off', obs: 'OBS', brb: stage.stage === 'BRB' ? 'Deliberate BRB' : 'Fallback BRB', file: stage.media?.name || 'Prepared video'})[stage.source] || stage.source;

  function remember(command) {
    try { command ? sessionStorage.setItem('restreamer-stage-command', JSON.stringify({id: command.id, server_id: command.server_id})) : sessionStorage.removeItem('restreamer-stage-command'); } catch { /* Reconciliation still works while this page remains open. */ }
  }
  try { inflight = JSON.parse(sessionStorage.getItem('restreamer-stage-command') || 'null'); } catch { inflight = null; }

  function result(outcome) {
    lastCommand = outcome.id; lastOutcome = JSON.stringify(outcome);
    const titles = {completed: 'Completed', pending: 'Go live pending', cancelled: 'Cancelled', rejected: 'Not applied', timed_out: 'Go live timed out'};
    message = `${titles[outcome.state] || outcome.state}${outcome.reason ? ` · ${outcome.reason}` : ''}`;
    if (inflight?.id === outcome.id) { inflight = null; remember(null); }
    render();
  }
  async function reconcile() {
    if (!inflight || resolving || !online) return;
    resolving = true;
    const command = inflight;
    try {
      const response = await fetch(`${location.origin}/api/stage/commands/${encodeURIComponent(command.id)}`, {cache: 'no-store', signal: AbortSignal.timeout(8000)});
      if (inflight !== command) return;
      if (response.status === 404) {
        message = 'The server has no recorded outcome for this command. It was not replayed. Review the current state before choosing a fresh action.';
        inflight = null; remember(null);
      } else {
        const outcome = await response.json();
        if (!response.ok) throw new Error(outcome.reason || 'Could not read the command outcome.');
        result(outcome);
      }
    } catch {
      message = 'Command response unavailable. Checking its recorded outcome; the action will not be replayed.';
    } finally { resolving = false; render(); }
  }
  async function execute(request, confirmed, context) {
    if (!online || inflight || refreshing || !current()) return;
    const command = {id: commandID(), server_id: current().server_id, context: context ?? current().context, confirmed, ...request};
    refreshing = true; inflight = command; lastCommand = command.id; remember(command); message = 'Sending command…'; render();
    try {
      const response = await fetch(`${location.origin}/api/stage/commands`, {method: 'POST', headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: JSON.stringify(command), signal: AbortSignal.timeout(8000)});
      const outcome = await response.json();
      if (inflight?.id !== command.id) return;
      if (!outcome.id) outcome.id = command.id;
      if (!response.ok && !outcome.state) outcome.state = 'rejected';
      result(outcome);
    } catch {
      message = 'Command response unavailable. Checking its recorded outcome; the action will not be replayed.';
    } finally { await refresh(); refreshing = false; render(); }
  }
  function noChange(request) {
    const stage = current();
    if (stage.error) return false;
    const stages = {go_live: 'LIVE', prestream: 'PRESTREAM', brb: 'BRB', end_stream: 'ENDING'};
    if (stages[request.action] === stage.stage) return true;
    return request.action === 'play_clip' && stage.stage === 'CLIP' && request.revision === stage.media?.revision;
  }
  function describe(request) {
    const stage = current(), destination = stage.return_media?.revision ? `${stage.return_stage} (${label(stage.return_media)})` : stage.return_stage || 'the recorded return stage';
    const mode = request.mode === 'preview_only' || stage.mode === 'preview_only' ? 'Preview only: no destination will receive this feed.' : 'Selected destinations will receive the new source.';
    switch (request.action) {
      case 'prestream': return ['Start Prestream?', `Play ${name(selections.prestream)} on a continuous starting-soon loop. OBS stays off air until you explicitly Go live. ${mode}`];
      case 'go_live': return ['Go live?', `Switch to OBS when a valid keyframe arrives, waiting up to 10 seconds. The current source continues until the switch succeeds. ${mode}`];
      case 'brb': return ['Take a deliberate break?', `Select BRB until you explicitly leave it. Any current clip is suspended; Return can resume it. OBS reconnecting will not end this break. ${mode}`];
      case 'play_clip': {
        const effect = stage.stage === 'CLIP' ? `Replace ${label(stage.media)} and keep its return destination: ${destination}.` : stage.stage === 'BRB' && stage.return_stage === 'CLIP' ? `Permanently discard suspended ${label(stage.return_media)}. After this clip, return to BRB; a later Return restores ${stage.return_after_discard || stage.return_playback?.return_stage || 'the original return stage'}.` : `After playback, return to ${stage.stage}.`;
        return ['Play this clip?', `Play ${name(request.revision)} ${request.loop ? 'on a loop until stopped' : 'once'}. ${effect} ${mode}`];
      }
      case 'end_stream': return ['Play ending and stop?', `Play ${name(selections.ending)} once, finish queued destination writes for up to 10 seconds, then stop. Ordinary stage controls are locked while the ending runs. ${mode}`];
      case 'return': return ['Return to the recorded stage?', `Restore ${destination}, resuming retained media near its saved position. An unavailable return source holds fallback BRB. ${mode}`];
      case 'retry': return ['Retry failed playback?', `Retry the selected media from its beginning. ${mode}`];
      case 'stop_clip': return ['Stop this clip?', `Stop the clip and restore ${destination}. ${mode}`];
      case 'stop_now': return [stage.mode === 'preview_only' ? 'Stop rehearsal now?' : 'Stop broadcast now?', 'Immediately stop all stage playback, cancel pending transitions, and disconnect every destination. OBS input may stay connected.'];
      case 'replace_now': return ['Replace current media?', `Start ${name(request.revision)} from the beginning, preserving the selected stage, return destination and loop setting.${stage.playback?.state === 'paused' ? ' The clip remains paused.' : ''}${stage.stage === 'ENDING' ? ' This restarts the ending video and postpones shutdown.' : ''} ${mode}`];
      case 'replace_on_return': return ['Replace suspended media on return?', `Keep the current source uninterrupted. When returning to ${destination}, start ${name(request.revision)} from its beginning instead of resuming the old revision. ${mode}`];
      case 'set_target': return request.enabled ? [`Enable ${request.target}?`, `This destination immediately joins ${sourceName(stage)}, the current on-air source.`] : [`Disable the last destination?`, `Disable ${request.target}. The stage and preview keep running with NO DESTINATIONS — NOT SENDING. This remains a real session, not a rehearsal.`];
      default: return ['Confirm change?', request.action];
    }
  }
  function request(request, needsConfirmation = true) {
    if (!online || inflight || refreshing || !current()) return;
    if (request.action === 'prestream') request = {...request, revision: selections.prestream};
    if (request.action === 'end_stream') request = {...request, revision: selections.ending};
    if (current().stage === 'OFF' && ['prestream', 'go_live'].includes(request.action)) request = {...request, mode: el('stage-rehearsal').checked ? 'preview_only' : 'real'};
    if (!needsConfirmation || noChange(request)) { execute(request, false); return; }
    const [title, effect] = describe(request);
    review = {request, context: current().context, server_id: current().server_id};
    el('stage-dialog-title').textContent = title;
    el('stage-dialog-effect').textContent = effect;
    el('stage-dialog-context').textContent = `Reviewed: ${current().stage} · ${sourceName(current())}${current().media?.revision ? ` · ${label(current().media)}` : ''}${request.revision ? ` → revision ${request.revision}` : ''}`;
    el('stage-dialog-confirm').textContent = request.action === 'stop_now' ? 'Stop now' : 'Confirm';
    el('stage-dialog-confirm').className = request.action === 'stop_now' ? 'danger-button' : 'preview-button';
    render(); el('stage-dialog').showModal(); el('stage-dialog-cancel').focus();
  }
  function render() {
    const stage = current();
    if (!stage) return;
    const busy = !!inflight || refreshing, usable = online && !busy, ending = locked();
    el('stage-badge').textContent = !online ? 'Disconnected · last known state' : stage.ending?.draining ? 'Finishing destination writes' : stage.stage;
    el('stage-badge').className = `badge ${stage.stage === 'OFF' ? '' : stage.mode === 'preview_only' ? 'warn' : 'live'}`;
    el('stage-selected').textContent = stage.stage;
    el('stage-source').textContent = sourceName(stage);
    const retained = stage.return_playback;
    el('stage-return').textContent = stage.return_stage ? `${stage.return_stage}${retained ? ` · ${retained.state === 'paused' ? 'paused by operator' : 'suspended'} · ${time(retained.position)} / ${time(retained.duration)}${retained.loop ? ' · looping' : ''}` : ''}` : 'None';
    const p = stage.playback || {};
    el('stage-media').textContent = stage.media?.revision ? `${label(stage.media)} · ${p.state || 'selected'} · ${time(p.position)} / ${time(p.duration)}${p.loop ? ' · looping' : ''}` : stage.stage === 'LIVE' && stage.source !== 'obs' ? 'LIVE intent remains selected; OBS recovery is automatic.' : '';
    el('stage-error').hidden = !stage.error;
    el('stage-error').textContent = stage.error || '';
    const rehearsal = stage.mode === 'preview_only' || (stage.stage === 'OFF' && stage.pending?.mode === 'preview_only');
    const noDestinations = stage.mode === 'real' && stage.stage !== 'OFF' && !(snapshot.outputs || []).some(output => output.enabled && output.can_enable);
    el('delivery-banner').hidden = !rehearsal && !noDestinations;
    el('delivery-banner').textContent = rehearsal ? 'PREVIEW ONLY — NOT BROADCASTING' : 'NO DESTINATIONS — NOT SENDING';
    el('stage-pending').hidden = !stage.pending;
    el('stage-pending-reason').textContent = stage.pending ? `${stage.pending.reason} · ${Math.max(0, Math.ceil((stage.pending.deadline - Date.now()) / 1000))} seconds remaining` : '';
    el('stage-cancel-pending').disabled = !usable;
    const results = stage.ending?.results || [];
    el('stage-completion').hidden = !stage.ending?.completed && !results.length;
    el('stage-completion').textContent = results.length ? `Ending completion: ${results.map(item => `${item.name}: ${item.reason === 'disabled' ? item.complete ? 'disabled before drain' : 'disabled before writes finished' : item.complete ? 'relay writes complete' : `incomplete${item.reason ? ` (${item.reason})` : ''}`}`).join('; ')}. This confirms relay writes, not platform-side playback.` : stage.ending?.completed ? 'Ending completed. No destination writes were pending.' : '';
    for (const [id, value] of [['prestream', 'PRESTREAM'], ['live', 'LIVE'], ['brb', 'BRB'], ['ending', 'ENDING']]) {
      const button = el(`stage-${id}`);
      button.setAttribute('aria-pressed', String(stage.stage === value));
      button.disabled = !usable || (ending && stage.stage !== value) || ((id === 'brb' || id === 'ending') && stage.stage === 'OFF');
    }
    el('stage-return-button').disabled = !usable || ending || !stage.return_stage;
    el('stage-return-button').textContent = stage.return_stage ? `Return to ${stage.return_stage}` : 'Return';
    el('stage-retry').hidden = stage.playback?.state !== 'failed';
    el('stage-retry').disabled = !usable || stage.ending?.draining;
    el('stage-stop').disabled = !usable || (stage.stage === 'OFF' && !stage.pending);
    el('stage-stop').textContent = rehearsal ? 'Stop rehearsal' : 'Stop now';
    el('rehearsal-label').hidden = stage.stage !== 'OFF';
    el('stage-rehearsal').disabled = !usable || !!stage.pending;
    el('stage-help').textContent = ending ? 'Ending in progress. Only confirmed Stop now or a ready media replacement can interrupt playback. Destinations can be disabled, but additional destinations cannot join.' : rehearsal ? 'Destination switches only save preferences during rehearsal. Stop rehearsal, then explicitly start real Prestream or Go live to broadcast.' : stage.stage === 'OFF' ? 'Only Prestream and Go live can start. Choose at least one eligible destination, or explicitly select preview-only rehearsal.' : 'Stage describes intent; on-air source shows the selected feed. Platform-side public availability is not confirmed by the relay.';
    el('command-status').textContent = message;
    const buttons = selections.shortcuts.map(shortcut => {
      const button = document.createElement('button');
      button.className = 'preview-button'; button.type = 'button';
      button.textContent = `▶ ${shortcut.name || name(shortcut.revision)}`;
      button.disabled = !usable || ending || stage.stage === 'OFF';
      button.addEventListener('click', () => request({action: 'play_clip', revision: shortcut.revision, loop: false}));
      return button;
    });
    el('clip-shortcuts').replaceChildren(...buttons);
    if (review) {
      const stale = review.server_id !== stage.server_id || review.context !== stage.context;
      el('stage-dialog-stale').hidden = !stale;
      el('stage-dialog-stale').textContent = `The broadcast changed. Current state: ${stage.stage} · ${sourceName(stage)}${stage.media?.revision ? ` · ${label(stage.media)}` : ''}. Cancel and review this state before trying again.`;
      el('stage-dialog-confirm').disabled = stale || !usable;
    }
    window.broadcastPreview?.update(online && stage.stage !== 'OFF', p.epoch);
    document.dispatchEvent(new Event('stage-command-state'));
  }
  for (const [id, action] of [['prestream', 'prestream'], ['live', 'go_live'], ['brb', 'brb'], ['ending', 'end_stream'], ['return-button', 'return'], ['retry', 'retry'], ['stop', 'stop_now']]) el(`stage-${id}`).addEventListener('click', () => request({action}));
  el('stage-cancel-pending').addEventListener('click', () => request({action: 'cancel_pending'}, false));
  el('stage-dialog-cancel').addEventListener('click', () => el('stage-dialog').close());
  el('stage-dialog').addEventListener('close', () => { review = null; });
  el('stage-dialog-confirm').addEventListener('click', () => {
    if (!review || review.context !== current().context || review.server_id !== current().server_id) return;
    const accepted = review; el('stage-dialog').close(); execute(accepted.request, true, accepted.context);
  });
  window.broadcastStages = {
    request,
    busy: () => !!inflight || refreshing,
    locked,
    setSelections(value, available) { selections = {...value, shortcuts: value.shortcuts || []}; revisions = available || revisions; render(); },
    target(output) {
      const stage = current();
      if (!stage) return;
      const activeReal = stage.mode === 'real' && stage.stage !== 'OFF';
      const last = output.enabled && (snapshot.outputs || []).filter(item => item.enabled && item.can_enable).length === 1;
      request({action: 'set_target', target: output.name, enabled: !output.enabled}, activeReal && (!output.enabled || last));
    },
    update(value, connected) {
      snapshot = value; online = connected;
      const stage = current();
      if (!stage) return;
      if ((serverID && serverID !== stage.server_id) || (inflight && inflight.server_id !== stage.server_id)) {
        inflight = null; lastCommand = null; lastOutcome = null; remember(null); el('stage-rehearsal').checked = false;
        message = 'Server restarted. State refreshed; previous commands were not replayed. Review the state and choose a fresh action.';
      }
      serverID = stage.server_id;
      const outcome = stage.outcomes?.find(item => item.id === (inflight?.id || lastCommand));
      if (outcome && JSON.stringify(outcome) !== lastOutcome) result(outcome);
      else if (inflight) reconcile();
      render();
    }
  };
})();
