'use strict';
(() => {
  const buttons = [...document.querySelectorAll('[data-remote-panel]')];
  const consolePanel = document.getElementById('live-console');
  buttons.forEach(button => button.addEventListener('click', () => {
    consolePanel.dataset.remotePanel = button.dataset.remotePanel;
    buttons.forEach(item => item.setAttribute('aria-pressed', String(item === button)));
  }));
  // Status and warnings may wrap on a phone. Keep both visible without assuming
  // their translated text, selected media name or alert fits a fixed height.
  const status = document.getElementById('workspace-status');
  const alert = document.getElementById('workspace-alert');
  const measure = () => {
    document.body.style.setProperty('--remote-status-height', `${status.getBoundingClientRect().height}px`);
    document.body.style.setProperty('--remote-alert-height', `${alert.getBoundingClientRect().height}px`);
  };
  const observer = new ResizeObserver(measure);
  observer.observe(status); observer.observe(alert);
  measure();
})();
