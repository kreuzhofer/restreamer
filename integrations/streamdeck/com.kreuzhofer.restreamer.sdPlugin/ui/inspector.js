'use strict';
let socket, context, action, settings = {}, clips = [];
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
// Entry point called by the Stream Deck property-inspector host.
window.connectElgatoStreamDeckSocket = (port, uuid, registerEvent, _info, actionInfo) => {
  const info = JSON.parse(actionInfo); context = uuid; action = info.action; settings = info.payload.settings || {};
  const clip = action.endsWith('.play-clip'); $('clip-section').hidden = !clip;
  const kind = action.split('.').at(-1);
  $('action-help').textContent = ({'stop-now':'Hold for two seconds, then release to stop forwarding immediately. A short press does nothing.','status':'Shows the broadcast stage and relay delivery. Press to open Live in your browser.','brb-return':'Enter a deliberate break. Press again to return to the stage remembered by the server.','end-stream':'Play the ending selected in Show setup. Fixed-length endings stop delivery when finished; music-driven endings loop until you stop.'})[kind] || 'Uses server-owned broadcast controls. Prepare stage media in Show setup before the show.';
  socket = new WebSocket(`ws://127.0.0.1:${port}`);
  socket.onopen = () => {socket.send(JSON.stringify({event:registerEvent,uuid}));send({command:'load'});};
  socket.onclose = () => message('Stream Deck is disconnected. Reopen this action’s settings.', true);
  socket.onmessage = event => {
    const received = JSON.parse(event.data);
    if (received.event === 'didReceiveSettings') {settings = received.payload.settings || {};renderClips();return;}
    if (received.event !== 'sendToPropertyInspector') return;
    const data = received.payload; $('save').disabled = false; $('refresh').disabled = false;
    if (data.error) {message(data.error, true);return;}
    if (data.config) {
      $('endpoint').value = data.config.endpoint; $('username').value = data.config.username; $('insecure').checked = data.config.allowInsecureHttp;
      $('password').placeholder = data.config.hasPassword ? 'Saved — leave blank to keep' : 'Dashboard password';
      $('password').required = !data.config.hasPassword;
      if (!data.config.endpoint) $('connection').open = true;
    }
    if (data.clips) {clips = data.clips;renderClips();}
    $('library-message').textContent = data.libraryError || (clips.some(c => c.state === 'ready') ? '' : 'No ready videos. Prepare a video in Restreamer, then refresh.');
    message(data.message || '');
  };
};
$('connection-form').addEventListener('submit', event => {
  event.preventDefault(); $('save').disabled = true; message('Saving connection…');
  send({command:'save',settings:{endpoint:$('endpoint').value.trim(),username:$('username').value,password:$('password').value,allowInsecureHttp:$('insecure').checked}});
  $('password').value = '';
});
$('refresh').addEventListener('click', () => {$('refresh').disabled = true;send({command:'load'});});
$('clip').addEventListener('change', () => {
  const selected = clips.find(c => c.id === $('clip').value && c.state === 'ready');
  settings = selected ? {revision:selected.id,name:selected.name} : {};
  socket?.send(JSON.stringify({event:'setSettings',context,payload:settings}));
});
