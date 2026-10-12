(() => {
  'use strict';
  const live = document.querySelector('#live-console');
  const studio = document.querySelector('.studio-editor-grid');
  if (!live && !studio) return;
  const key = 'restreamer.workspace-layout.v1';
  const defaults = {scenes: 220, inspector: 330, signals: 320};
  const limits = {scenes: [180, 360], inspector: [280, 480], signals: [260, 440]};
  let sizes = {...defaults};
  let recovery = '';
  try {
    const raw = localStorage.getItem(key);
    if (raw !== null) {
      const saved = JSON.parse(raw);
      if (!saved || saved.version !== 1 || !saved.sizes || Object.keys(defaults).some(name => !Number.isFinite(saved.sizes[name]) || saved.sizes[name] < limits[name][0] || saved.sizes[name] > limits[name][1])) throw new Error('invalid layout');
      sizes = Object.fromEntries(Object.keys(defaults).map(name => [name, saved.sizes[name]]));
    }
  } catch (_) { recovery = 'Saved layout unavailable or invalid. Default sizes restored for this window; Reset layout retries local storage.'; }
  const toolbar = document.createElement('details'); toolbar.className = 'layout-controls';
  const summary = document.createElement('summary'); summary.textContent = 'Layout';
  const form = document.createElement('div'); form.className = 'layout-options';
  const help = document.createElement('p'); help.className = 'layout-help'; help.textContent = 'Panel widths apply to the full desktop layout. Drag dividers, use arrow keys, or adjust these sliders.'; form.append(help);
  const status = document.createElement('p'); status.className = 'layout-status'; status.setAttribute('role', 'status'); status.textContent = recovery;
  const reset = document.createElement('button'); reset.type = 'button'; reset.className = 'preview-button'; reset.textContent = 'Reset layout';
  toolbar.append(summary, form); document.querySelector('.workspace-header').append(toolbar);
  document.querySelector('#workspace-status').after(status); status.hidden = !recovery;
  const controls = new Map();
  const dividers = new Map();
  function report(message) { status.textContent = message; status.hidden = !message; }
  function persist() {
    try { localStorage.setItem(key, JSON.stringify({version: 1, sizes})); report(''); }
    catch (_) { report('Layout changed for this window. Browser storage is unavailable; sizes cannot be restored after reload.'); }
  }
  function maximum(name) {
    let max = limits[name][1];
    if (studio && studio.clientWidth && name !== 'signals') {
      const other = name === 'scenes' ? sizes.inspector : sizes.scenes;
      max = Math.min(max, studio.clientWidth - other - 380);
    }
    if (live && live.clientWidth && name === 'signals') max = Math.min(max, live.clientWidth - 570);
    return Math.max(limits[name][0], max);
  }
  function apply() {
    const values = {...sizes};
    // Clamp rendered widths at each viewport without overwriting desktop preferences.
    for (const name of Object.keys(values)) values[name] = Math.max(limits[name][0], Math.min(values[name], maximum(name)));
    if (studio) { studio.style.setProperty('--scene-width', `${values.scenes}px`); studio.style.setProperty('--inspector-width', `${values.inspector}px`); }
    if (live) live.style.setProperty('--signals-width', `${values.signals}px`);
    for (const [name, input] of controls) { input.value = values[name]; input.max = maximum(name); }
    for (const [name, divider] of dividers) { divider.setAttribute('aria-valuenow', String(values[name])); divider.setAttribute('aria-valuemax', String(maximum(name))); divider.setAttribute('aria-valuetext', `${Math.round(values[name])} pixels`); }
  }
  function resize(name, value, save = true) { sizes[name] = Math.max(limits[name][0], Math.min(value, maximum(name))); apply(); if (save) persist(); }
  function add(name, label, grid, before, direction) {
    const field = document.createElement('label'); field.textContent = label;
    const input = document.createElement('input'); input.type = 'range'; input.min = limits[name][0]; input.max = limits[name][1]; input.step = 10; input.setAttribute('aria-label', `${label} width`);
    input.addEventListener('input', () => resize(name, Number(input.value))); field.append(input); form.append(field); controls.set(name, input);
    const divider = document.createElement('div'); divider.className = `layout-divider layout-${name}`; divider.tabIndex = 0; divider.setAttribute('role', 'separator'); divider.setAttribute('aria-label', `${label} width`); divider.setAttribute('aria-orientation', 'vertical'); divider.setAttribute('aria-valuemin', String(limits[name][0])); divider.title = `${label}: drag or use arrow keys; Home and End select limits`;
    grid.insertBefore(divider, before); dividers.set(name, divider);
    divider.addEventListener('keydown', event => {
      let value = Number(divider.getAttribute('aria-valuenow'));
      if (event.key === 'Home') value = limits[name][0];
      else if (event.key === 'End') value = maximum(name);
      else if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') value += (event.key === 'ArrowRight' ? 1 : -1) * direction * (event.shiftKey ? 40 : 10);
      else return;
      event.preventDefault(); resize(name, value);
    });
    let drag;
    divider.addEventListener('pointerdown', event => { if (event.button !== 0) return; drag = {x: event.clientX, width: Number(divider.getAttribute('aria-valuenow'))}; divider.setPointerCapture(event.pointerId); divider.focus(); event.preventDefault(); });
    divider.addEventListener('pointermove', event => { if (drag) resize(name, drag.width + direction * (event.clientX - drag.x), false); });
    divider.addEventListener('pointerup', () => { if (drag) { drag = null; persist(); } });
    divider.addEventListener('pointercancel', () => { drag = null; persist(); });
  }
  if (live) add('signals', 'Signals panel', live, live.querySelector('.live-sidebar'), -1);
  if (studio) {
    add('scenes', 'Scenes panel', studio, studio.querySelector('.studio-monitor'), 1);
    add('inspector', 'Properties panel', studio, studio.querySelector('.studio-inspector'), -1);
  }
  form.append(reset);
  reset.addEventListener('click', () => { sizes = {...defaults}; apply(); persist(); report(status.hidden ? 'Default layout restored.' : status.textContent); });
  let tabs;
  if (live) {
    live.querySelector('.live-primary').id = 'live-primary'; live.querySelector('.live-sidebar').id = 'live-sidebar';
    tabs = document.createElement('nav'); tabs.className = 'live-secondary-tabs'; tabs.setAttribute('aria-label', 'Live panels');
    for (const [name, label] of [['operation', 'Broadcast'], ['signals', 'Signals & health']]) {
      const button = document.createElement('button'); button.type = 'button'; button.textContent = label; button.dataset.layoutPanel = name; button.setAttribute('aria-controls', name === 'operation' ? 'live-primary' : 'live-sidebar'); button.setAttribute('aria-pressed', String(name === 'operation'));
      button.addEventListener('click', () => { live.dataset.layoutPanel = name; for (const tab of tabs.children) tab.setAttribute('aria-pressed', String(tab === button)); }); tabs.append(button);
    }
    live.dataset.layoutPanel = 'operation'; live.before(tabs);
  }
  function viewportChanged() {
    const active = document.activeElement;
    if (studio && innerWidth > 800 && studio.dataset.panel === 'scenes') {
      const preview = document.querySelector('[data-editor-panel=preview]'); preview?.click(); preview?.focus();
    }
    apply();
    if (active && active !== document.body && !active.getClientRects().length) {
      if (innerWidth <= 600) document.querySelector('.workspace-nav a')?.focus();
      else if (live && tabs && innerWidth < 1000) tabs.querySelector('[aria-pressed=true]').focus();
      else if (studio) document.querySelector('.studio-panel-switch [aria-pressed=true]')?.focus();
      else summary.focus();
    }
  }
  window.addEventListener('resize', viewportChanged);
  const observer = new ResizeObserver(apply); observer.observe(live || studio);
  apply();
})();
