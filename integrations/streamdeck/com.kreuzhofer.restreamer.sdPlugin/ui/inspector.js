'use strict';
let socket, context, action, settings = {}, clips = [], targets = [];
const $ = id => document.getElementById(id);
const message = (text, error = false) => { $('message').textContent = text; $('message').classList.toggle('error', error); };
function send(payload) {
  if (socket?.readyState !== WebSocket.OPEN) { message('Stream Deck is disconnected. Reopen this action’s settings.', true); return; }
  socket.send(JSON.stringify({event:'sendToPlugin',context,action,payload}));
}
function renderClips() {
  const select = $('clip'); select.replaceChildren(new Option('Choose a ready video…', ''));
  const ready = clips.filter(c => c.state === 'ready');
  ready.forEach((clip, index) => {
    const seconds = Math.round(clip.duration || 0);
    const duration = `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
    const duplicates = ready.filter(c => c.name === clip.name).length > 1;
    select.add(new Option(`${clip.name} · ${duration}${duplicates ? ` · render ${index + 1}` : ''}`, clip.id));
  });
  if (settings.revision && !ready.some(c => c.id === settings.revision)) {
    select.add(new Option(`${settings.name || 'Selected video'} — unavailable; choose another`, settings.revision));
  }
  select.value = settings.revision || '';
}
function renderTargets() {
 const select=$('target');select.replaceChildren(new Option('Choose a destination…',''));
 for(const target of targets)select.add(new Option(target.name+(target.can_enable?'':' — configure in Restreamer'),target.name));
 if(settings.target&&!targets.some(t=>t.name===settings.target))select.add(new Option(settings.target+' — unavailable; select again',settings.target));
 select.value=settings.target||'';$('platform').value=settings.platform||'auto';
}
// Entry point called by the Stream Deck property-inspector host.
window.connectElgatoStreamDeckSocket = (port, uuid, registerEvent, _info, actionInfo) => {
  const info = JSON.parse(actionInfo); context = uuid; action = info.action; settings = info.payload.settings || {};
  const clip = action.endsWith('.play-clip'); $('clip-section').hidden = !clip;
  const kind = action.split('.').at(-1);$('target-section').hidden=kind!=='target-toggle';
  $('action-help').textContent = ({'target-toggle':'Hold for two seconds, then release to switch this destination on or off. A short press does nothing. Green means relay delivery, not confirmed platform playback.','stop-now':'Hold for two seconds, then release to stop forwarding immediately. A short press does nothing.','status':'Shows the broadcast stage and relay delivery. Press to open Live in your browser.','brb-return':'Enter a deliberate break. Press again to return to the stage remembered by the server.','end-stream':'Play the ending selected in Show setup. Fixed-length endings stop delivery when finished; music-driven endings loop until you stop.'})[kind] || 'Uses server-owned broadcast controls. Prepare stage media in Show setup before the show.';
  socket = new WebSocket(`ws://127.0.0.1:${port}`);
  socket.onopen = () => {socket.send(JSON.stringify({event:registerEvent,uuid}));send({command:'load'});};
  socket.onclose = () => message('Stream Deck is disconnected. Reopen this action’s settings.', true);
  socket.onmessage = event => {
    const received = JSON.parse(event.data);
    if (received.event === 'didReceiveSettings') {settings = received.payload.settings || {};renderClips();renderTargets();return;}
    if (received.event !== 'sendToPropertyInspector') return;
    const data = received.payload; $('save').disabled = false; $('refresh').disabled = false;
    if (data.error) {message(data.error, true);return;}
    if (Object.hasOwn(data, 'pairing')) {
      $('pairing').hidden = !data.pairing;
      $('pairing-code').textContent = data.pairing?.code || '';
      $('save').disabled = !!data.pairing;
      if (data.pairing) $('connection').open = true;
    }
    if (data.config) {
      $('endpoint').value = data.config.endpoint; $('insecure').checked = data.config.allowInsecureHttp;
      if (!data.config.endpoint) $('connection').open = true;
    }
    if(data.targets){targets=data.targets;renderTargets();$('target-message').textContent=!data.controlDestinations?'Reconnect in Server connection and approve Control destinations in your browser.':targets.length?'': 'No destinations. Add one in Restreamer Settings, then refresh.';}
    if (data.clips) {clips = data.clips;renderClips();}
    $('library-message').textContent = data.libraryError || (clips.some(c => c.state === 'ready') ? '' : 'No ready videos. Prepare a video in Restreamer, then refresh.');
    message(data.message || '');
  };
};
$('connection-form').addEventListener('submit', event => {
  event.preventDefault(); $('save').disabled = true; message('Opening a secure browser connection…');
  send({command:'pair',settings:{endpoint:$('endpoint').value.trim(),name:$('device-name').value.trim(),allowInsecureHttp:$('insecure').checked}});
});
$('open-pairing').addEventListener('click', () => send({command:'openPairing'}));
$('cancel-pairing').addEventListener('click', () => send({command:'cancelPairing'}));
$('refresh').addEventListener('click', () => {$('refresh').disabled = true;send({command:'load'});});
$('clip').addEventListener('change', () => {
  const selected = clips.find(c => c.id === $('clip').value && c.state === 'ready');
  settings = selected ? {revision:selected.id,name:selected.name} : {};
  socket?.send(JSON.stringify({event:'setSettings',context,payload:settings}));
});

$('refresh-targets').addEventListener('click',()=>send({command:'load'}));
function saveTarget(){settings={target:$('target').value||undefined,platform:$('platform').value};socket?.send(JSON.stringify({event:'setSettings',context,payload:settings}));}
$('target').addEventListener('change',saveTarget);$('platform').addEventListener('change',saveTarget);
