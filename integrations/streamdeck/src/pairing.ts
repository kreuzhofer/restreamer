import {serverSettings, type ConnectionSettings} from './client.js';

type PairingView = {code: string; expires: number} | null;

// Owns only the short-lived exchange. The token goes directly to global
// settings; neither it nor the device secret is sent to the inspector/browser.
export class Pairing {
  view: PairingView = null;
  private abort?: AbortController;
  private timer?: ReturnType<typeof setTimeout>;
  private browserURL = '';

  constructor(
    private notify: (pairing: PairingView, message: string) => Promise<unknown>,
    private complete: (settings: ConnectionSettings) => Promise<void>,
    private open: (url: string) => Promise<unknown>
  ) {}

  cancel() {
    this.abort?.abort();
    clearTimeout(this.timer);
    this.view = null;
    this.browserURL = '';
  }

  async openBrowser() { if (this.browserURL) await this.open(this.browserURL); }

  async start(input: ConnectionSettings & {name?: string}) {
    const settings = serverSettings(input);
    const name = input.name?.trim() || 'Stream Deck';
    if (name.length > 80) throw new Error('Use a device name of at most 80 characters.');
    this.cancel();
    const controller = new AbortController();
    this.abort = controller;
    const request = async (path: string, body: object) => {
      const response = await fetch(new URL(path, settings.endpoint), {
        method: 'POST', redirect: 'error', cache: 'no-store',
        signal: AbortSignal.any([controller.signal, AbortSignal.timeout(5000)]),
        headers: {'Content-Type': 'application/json', 'X-Restreamer-Control': '1'},
        body: JSON.stringify(body)
      });
      return {status: response.status, data: await response.json()};
    };
    let result;
    try { result = await request('/api/devices/pair', {name}); }
    catch { throw new Error('Cannot reach pairing. Check the server address and try again.'); }
    if (result.status !== 201) throw new Error(result.status === 429 ? 'Too many pending connections. Try again in five minutes.' : 'Pairing unavailable. Update Restreamer and enable persistent storage.');
    const {device_code, user_code, expires_in, interval} = result.data;
    if (typeof device_code !== 'string' || typeof user_code !== 'string' || !/^[A-Z2-7]{4}-[A-Z2-7]{4}$/.test(user_code) || !Number.isFinite(expires_in) || expires_in <= 0 || expires_in > 300 || !Number.isFinite(interval) || interval < 1 || interval > 30) throw new Error('Invalid pairing response. Update Restreamer and try again.');
    if (controller.signal.aborted) return;
    this.view = {code: user_code, expires: Date.now() + expires_in * 1000};
    this.browserURL = new URL('/settings#devices?code=' + encodeURIComponent(user_code), settings.endpoint).href;
    await this.notify(this.view, 'Compare this code in your browser, then approve the connection.');
    await this.openBrowser();
    const poll = async () => {
      if (controller.signal.aborted) return;
      try {
        if (!this.view || Date.now() >= this.view.expires) throw new Error('expired');
        const response = await request('/api/devices/token', {device_code});
        if (controller.signal.aborted) return;
        if (response.status === 200) {
          if (typeof response.data.access_token !== 'string' || !/^[a-f0-9]{64}$/.test(response.data.access_token)) throw new Error('invalid');
          await this.complete({...settings, token: response.data.access_token});
          this.cancel();
          await this.notify(null, 'Connected. This device has its own revocable access.');
          return;
        }
        if (response.status !== 202 && response.status !== 429) throw new Error('denied');
        this.timer = setTimeout(() => void poll(), (response.status === 429 ? interval + 5 : interval) * 1000);
      } catch {
        if (!controller.signal.aborted) {
          this.cancel();
          await this.notify(null, 'Connection expired, was declined, or could not complete. Connect again in your browser.');
        }
      }
    };
    this.timer = setTimeout(() => void poll(), interval * 1000);
  }
}
