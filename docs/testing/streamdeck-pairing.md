# Stream Deck browser pairing verification

Verified 2026-11-10 on the implementation branch for #3.

The bundled plugin ran against a simulated Stream Deck WebSocket host. Its shipping property inspector connected to an isolated Restreamer at `127.0.0.1:18894`, with dummy credentials, no destinations and stage OFF. No real streaming service was contacted.

- Desktop: entered the server address, opted into local HTTP and started browser pairing. The plugin and dashboard displayed the same short code. Approval stayed disabled until the code-match checkbox was selected.
- Approved the explicitly authorized test device in the browser. The plugin completed the exchange and showed Connected. Refreshing Settings listed the device and its connection time.
- At an emulated 390 × 773 phone viewport, inspected Connected devices and its revoke confirmation. Content and controls stayed within the viewport with bottom navigation intact.
- Revoked the test device. It disappeared from the list; the plugin's next library request showed Access denied and instructed reconnecting in the browser. The server stayed OFF.
- Inspected status-key artwork at 72px and 96px and the shipping inspector at 360px width. Labels, counts and symbols remain readable independently of color.

Native Chrome was used because the browser extension host was unavailable. Screenshots were inspected in the task conversation; the existing `screenshots/streamdeck-plugin.png` predates the pairing inspector and is only historical key-renderer evidence. Phone emulation does not substitute for a physical iPhone or Stream Deck installation test. Physical Stream Deck installation and key interaction remain an explicit validation limitation.

Automated tests exercise public plugin events and HTTP boundaries. They include rejection of privileged routes/actions, expiration, single-use exchange, polling bounds, persistent hashed storage, restart, revocation, storage failure, and cancellation of a held Stop after connection loss. Local and exact-commit CI results are recorded in PR #67.
