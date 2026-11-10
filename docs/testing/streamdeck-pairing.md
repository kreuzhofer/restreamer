# Stream Deck browser pairing verification

Verified 2026-11-10 on the implementation branch for #3.

The bundled plugin ran against a simulated Stream Deck WebSocket host. Its shipping property inspector connected to an isolated Restreamer at `127.0.0.1:18894`, with dummy credentials, no destinations and stage OFF. No real streaming service was contacted.

- Desktop: entered the server address, opted into local HTTP and started browser pairing. The plugin and dashboard displayed the same short code. Approval stayed disabled until the code-match checkbox was selected.
- Approved the explicitly authorized test device in the browser. The plugin completed the exchange and showed Connected. Refreshing Settings listed the device and its connection time.
- At an emulated 390 × 773 phone viewport, inspected Connected devices and its revoke confirmation. Content and controls stayed within the viewport with bottom navigation intact.
- Revoked the test device. It disappeared from the list; the plugin's next library request showed Access denied and instructed reconnecting in the browser. The server stayed OFF.
- Inspected status-key artwork at 72px and 96px and the shipping inspector at 360px width. Labels, counts and symbols remain readable independently of color.

Native Chrome was used because the browser extension host was unavailable. Current captures are [desktop approval](screenshots/streamdeck-pairing-desktop.png), [390px approval](screenshots/streamdeck-pairing-mobile.png), and [360px inspector](screenshots/streamdeck-pairing-inspector.png). The phone image is a full-document capture: fixed navigation appears at the original viewport boundary, and content below it is reached by scrolling. The inspector is scrolled to its pairing section. The existing `screenshots/streamdeck-plugin.png` predates the pairing inspector and is only historical key-renderer evidence. Phone emulation does not substitute for a physical iPhone or Stream Deck installation test. Physical Stream Deck installation and key interaction remain an explicit validation limitation.

Automated tests exercise public plugin events and HTTP boundaries. They include rejection of privileged routes/actions, expiration, single-use exchange, polling bounds, persistent hashed storage, restart, revocation, storage failure, and cancellation of a held Stop after connection loss, slow-body isolation, and continuous-encoder failure/recovery health. Local and exact-commit CI results are recorded in PR #67.

## Design conformance

The narrow Operate extension was compared with `PRODUCT.md`, `DESIGN.md` and `studio-system.css`, using the three captures above and the shipping devices and inspector HTML, CSS and JavaScript. The captured approval and inspector surfaces retain the studio's charcoal surfaces, mint accent, system sans typography, labelled commands and restrained form containment. The dashboard keeps its existing navigation and flat section structure; the phone approval form wraps within the available width. Matching codes are prominent in both surfaces, and the approval requirement is stated in text.

This design pass inspected device-list rows and revocation confirmation in source only; it did not independently repeat the interaction verification recorded above. Captures do not establish focus, error-state or physical-device acceptance. The inspector's compact typography, 36px controls and 2px focus outline remain local treatments rather than new shared tokens. Existing legacy focus/color overrides, small helper text and mixed editor breakpoints documented in `DESIGN.md` were neither repaired nor approved by this pass. No new visual world, branding or system rule was introduced; `PRODUCT.md`, `DESIGN.md` and the design sidecar are unchanged.
