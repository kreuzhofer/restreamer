/* Operator account controls within Settings. Passwords never enter storage. */
(() => {
  'use strict';
  const form = document.getElementById('account-password-form');
  if (!form) return;
  const fields = document.getElementById('account-fields');
  const feedback = document.getElementById('account-feedback');
  const current = document.getElementById('account-current-password');
  const replacement = document.getElementById('account-new-password');
  const confirmation = document.getElementById('account-confirm-password');
  async function load() {
    try {
      const response = await fetch('/api/account', {cache: 'no-store', signal: AbortSignal.timeout(10000)});
      if (!response.ok) throw new Error('Could not load your account. Reload this page to retry.');
      const account = await response.json();
      document.getElementById('account-identity').textContent = 'Signed in as ' + account.username;
      fields.disabled = !account.password_change_available;
      if (fields.disabled) feedback.textContent = 'Password changes require persistent account storage on the server.';
    } catch (error) { feedback.textContent = error.message; }
  }
  form.addEventListener('submit', async event => {
    event.preventDefault();
    if (replacement.value !== confirmation.value) {
      feedback.textContent = 'The new passwords do not match.';
      confirmation.focus();
      return;
    }
    const body = JSON.stringify({current_password: current.value, new_password: replacement.value});
    fields.disabled = true;
    feedback.textContent = 'Changing password…';
    try {
      const response = await fetch('/api/account/password', {method: 'POST', cache: 'no-store', signal: AbortSignal.timeout(10000), headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'}, body});
      if (!response.ok) throw new Error(await response.text() || 'Could not change the password.');
      form.reset();
      feedback.textContent = 'Password changed. Sign in again with your new password when prompted.';
    } catch (error) {
      form.reset();
      feedback.textContent = error.message + ' If the connection was interrupted, try signing in with the new password before retrying.';
    } finally { fields.disabled = false; }
  });
  window.addEventListener('pagehide', () => form.reset());
  load();
})();
