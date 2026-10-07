'use strict';
const $ = (selector, root = document) => root.querySelector(selector);
const cards = new Map();
const samples = [];
const windowMs = 15 * 60 * 1000;
const states = {idle: 'Ready', disabled: 'Disabled', stopping: 'Stopping', connecting: 'Connecting', waiting_for_keyframe: 'Waiting for keyframe', streaming: 'Streaming', retrying: 'Retrying'};
const issues = {connect_failed: 'Could not connect or publish. Check the target server, stream key, and network access.', queue_overflow: 'The destination could not keep up; its output queue filled. Reconnecting independently.', connection_closed_or_write_failed: 'The destination disconnected or stopped accepting data. Reconnecting automatically.'};
let snapshot;
let connected = false;
let controlError = '';
let refreshSequence = 0;
const pending = new Set();
const rate = bps => (bps / 1e6).toFixed(2);
const timeLabel = milliseconds => new Date(milliseconds).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit', second: '2-digit'});

function node(tag, attrs, text) {
 const element = document.createElementNS('http://www.w3.org/2000/svg', tag);
 for (const [key, value] of Object.entries(attrs)) element.setAttribute(key, value);
 if (text !== undefined) element.textContent = text;
 return element;
}
function chart(container, values, now, label) {
 const width = Math.max(280, container.clientWidth);
 const height = container.classList.contains('input-chart') ? 200 : 180;
 const left = 43, right = width - 8, top = 17, bottom = height - 27;
 const peak = Math.max(0, ...values.map(p => p.value / 1e6));
 const max = Math.max(1, Math.ceil(peak * 1.15 * 2) / 2);
 const x = t => left + (t - (now - windowMs)) / windowMs * (right - left);
 const y = v => bottom - v / 1e6 / max * (bottom - top);
 const svg = node('svg', {viewBox: `0 0 ${width} ${height}`, role: 'img', 'aria-label': label});
 svg.append(node('title', {}, `${label}. Current ${rate(values.at(-1)?.value || 0)} Mbps; peak ${peak.toFixed(2)} Mbps.`));
 for (let i = 0; i <= 3; i++) {
  const gy = top + i * (bottom - top) / 3;
  svg.append(node('line', {x1: left, y1: gy, x2: right, y2: gy, class: 'grid-line'}));
  svg.append(node('text', {x: left - 10, y: gy + 4, 'text-anchor': 'end'}, (max * (3 - i) / 3).toFixed(1)));
 }
 for (let i = 0; i <= 3; i++) svg.append(node('text', {x: left + (right - left) * i / 3, y: height - 5, 'text-anchor': i === 0 ? 'start' : i === 3 ? 'end' : 'middle'}, i === 3 ? 'Now' : `−${15 - 5 * i} min`));
 // Leave missing intervals blank instead of implying a constant signal.
 let segment = [];
 const draw = () => {
  if (!segment.length) return;
  const path = segment.map((p, i) => `${i ? 'L' : 'M'}${x(p.time).toFixed(2)},${y(p.value).toFixed(2)}`).join(' ');
  svg.append(node('path', {d: `${path} L${x(segment.at(-1).time)},${bottom} L${x(segment[0].time)},${bottom} Z`, class: 'graph-fill'}));
  svg.append(node('path', {d: path, class: 'graph-line'}));
  segment = [];
 };
 for (const point of values) {
  if (segment.length && point.time - segment.at(-1).time > 3500) draw();
  segment.push(point);
 }
 draw();
 if (!values.length) svg.append(node('text', {x: (left + right) / 2, y: (top + bottom) / 2, 'text-anchor': 'middle', class: 'empty'}, 'Collecting bitrate history…'));
 container.replaceChildren(svg);
}
function badge(element, state) {
 element.textContent = states[state] || state;
 element.className = `badge ${state === 'streaming' ? 'live' : ['retrying', 'connecting', 'stopping', 'waiting_for_keyframe'].includes(state) ? 'warn' : ''}`;
}
function render() {
 if (!snapshot) return;
 const now = snapshot.time;
 const last = samples.at(-1);
 const current = connected && last && now - last.time < 3500;
 $('#input-rate').textContent = current ? rate(last.input) : '—';
 $('#input-peak').textContent = samples.length ? rate(Math.max(...samples.map(p => p.input))) : '—';
 $('#active-targets').textContent = connected ? snapshot.outputs.filter(o => o.state === 'streaming').length : '—';
 badge($('#input-status'), connected ? snapshot.publishing ? 'streaming' : 'idle' : 'Unavailable');
 if (connected && !snapshot.publishing) $('#input-status').textContent = 'Waiting for OBS';
 $('#input-detail').textContent = snapshot.publishing ? 'Receiving live video & audio' : 'Connect OBS to begin · history continues while idle';
 $('#persistence').textContent = snapshot.persistent ? 'Target switches saved across restarts' : 'Target switches reset on restart';
 $('#target-count').textContent = snapshot.outputs.length;
 chart($('#input-chart'), samples.map(p => ({time: p.time, value: p.input})), now, 'Input bitrate over 15 minutes');
 for (const output of snapshot.outputs) {
  let card = cards.get(output.name);
  if (!card) {
   card = $('#target-template').content.firstElementChild.cloneNode(true);
   $('.target-name', card).textContent = output.name;
   $('.toggle', card).setAttribute('aria-label', `Enable ${output.name}`);
   $('.toggle', card).addEventListener('click', () => toggle(output.name));
   cards.set(output.name, card);
   $('#targets').append(card);
  }
  // The badge class changes, so retain its reference via the heading.
  badge($('.panel-heading .badge', card), connected ? output.state : 'Unavailable');
  const points = samples.map(p => ({time: p.time, value: p.outputs[output.name] || 0}));
  $('.target-rate', card).textContent = current ? rate(last.outputs[output.name] || 0) : '—';
  $('.target-peak', card).textContent = points.length ? rate(Math.max(...points.map(p => p.value))) : '—';
  chart($('.target-chart', card), points, now, `${output.name} bitrate over 15 minutes`);
  let message = !output.can_enable ? output.unavailable_reason : output.state === 'disabled' ? 'Output is stopped. Input continues.' : output.state === 'idle' ? 'Waiting for an input stream.' : output.state === 'waiting_for_keyframe' ? 'Connected. Waiting for the next video keyframe.' : output.state === 'retrying' ? `Next attempt in ${Math.max(0, Math.ceil((output.retry_at - now) / 1000))}s` : output.state === 'streaming' ? 'Forwarding the original stream.' : output.state === 'stopping' ? 'Closing destination connection…' : 'Opening destination connection…';
  $('.target-message', card).textContent = message;
  $('.attempts', card).textContent = `${output.attempts} connection ${output.attempts === 1 ? 'attempt' : 'attempts'}`;
  const issue = $('.issue', card);
  issue.hidden = !output.last_error;
  issue.classList.toggle('current', output.state === 'retrying');
  issue.textContent = output.last_error ? `${output.state === 'retrying' ? 'Issue' : 'Last issue'} · ${timeLabel(output.last_error_at)} — ${issues[output.last_error] || 'Destination connection failed.'}` : '';
  const button = $('.toggle', card);
  button.setAttribute('aria-checked', String(output.enabled));
  button.disabled = !connected || !output.can_enable || pending.has(output.name);
  $('.toggle-text', card).textContent = pending.has(output.name) ? 'Saving…' : output.enabled ? 'On' : 'Off';
  $('.switch-label', card).textContent = !output.can_enable ? 'Configuration required' : output.enabled ? 'Target enabled' : 'Target disabled';
  $('.switch-help', card).textContent = !output.can_enable ? 'Set a server URL and key, then redeploy.' : output.enabled ? 'Switch off to stop this destination.' : snapshot.publishing ? 'Switch on to resume from the live stream.' : 'Switch on to send your next live stream.';
 }
}
function connection(ok, message) {
 connected = ok;
 $('#connection').textContent = ok ? 'Dashboard connected' : 'Connection lost';
 $('#connection-dot').className = `dot ${ok ? 'online' : 'offline'}`;
 const text = message || controlError;
 $('#notice').textContent = text;
 $('#notice').hidden = !text;
}
async function refresh() {
 const sequence = ++refreshSequence;
 try {
  const response = await fetch(`${location.origin}/api/dashboard`, {cache: 'no-store', signal: AbortSignal.timeout(8000)});
  if (!response.ok) throw new Error(response.status === 401 ? 'Your session is no longer authenticated. Reload to sign in.' : 'Dashboard unavailable. Check the container and reverse proxy.');
  const data = await response.json();
  if (sequence !== refreshSequence) return;
  snapshot = data;
  samples.splice(0, samples.length, ...(snapshot.history || []));
  connection(true);
  $('#updated').textContent = `Updated ${timeLabel(snapshot.time)}`;
 } catch (error) {
  if (sequence !== refreshSequence) return;
  connection(false, error.message === 'Failed to fetch' ? 'Lost connection to the server. Displaying the last received history; retrying automatically.' : error.message);
 }
 render();
}
async function toggle(name) {
 const output = snapshot.outputs.find(o => o.name === name);
 if (!connected || pending.has(name)) return;
 pending.add(name);
 controlError = '';
 render();
 try {
  const response = await fetch(`${location.origin}/api/targets/${encodeURIComponent(name)}`, {method: 'PUT', headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body: JSON.stringify({enabled: !output.enabled}), signal: AbortSignal.timeout(8000)});
  if (!response.ok) throw new Error((await response.text()).trim() || 'Could not change this target.');
 } catch (error) { controlError = error.message; }
 finally { pending.delete(name); await refresh(); }
}
async function poll() { await refresh(); setTimeout(poll, 2000); }
let resizeTimer;
window.addEventListener('resize', () => { clearTimeout(resizeTimer); resizeTimer = setTimeout(render, 100); });
poll();
