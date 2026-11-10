# Stream Deck destination controls: design and verification

This records the narrow destination-control extension for plugin 1.2.0. It preserves the incumbent [product](../../PRODUCT.md) and [design system](../../DESIGN.md); it does not establish new global tokens or replace either document. The independent documenter inspected the shipping inspector, device UI sources, `integrations/streamdeck/src/keys.ts`, and the captures below. Runtime observations and test results are attributed to the implementing agent; this documentation pass did not rerun them.

## Incumbent conformance

The inspector keeps the restrained dark utility treatment: system typography, mint commands, visible field labels, outlined secondary commands and a disclosure for the connection shared by all keys. Destination selection and the optional platform-logo override precede that disclosure. The instruction specifies a two-second hold followed by release and explicitly says a short press does nothing. The existing compact inspector measurements are local to its host, not additions to dashboard control tokens.

Connected devices stays within Settings, using the established charcoal form surface, supporting copy, code emphasis, labelled checkboxes and explicit Approve connection / Decline commands. The optional permission says that it enables or disables existing destinations, including while live. It remains distinct from confirming that the pairing codes match. Phone pairing exposes this form within the existing mobile shell, with wrapping action rows and 44px minimum button height.

Key faces use a platform mark, destination name and written status together. Long names truncate; status remains separately visible. Platform colors identify the service, while the border and status color describe relay state. `ENABLED` while broadcast is off differs from `STREAMING`; `PAIR AGAIN`, `OFFLINE`, `ERROR` and `CONNECTING` make non-delivery states explicit. The key palette and compact typography remain local hardware-display conventions. Green represents relay delivery, not confirmed public playback on the platform.

## Browser and visual evidence

- [Keys and shipping inspector](screenshots/streamdeck-target-keys.png): illustrative 72px and 96px samples show platform marks, long-name truncation and status text. The harness at `127.0.0.1:18904` embeds the actual shipping inspector in a 360px iframe with a simulated Stream Deck host. The implementing agent changed the selected destination from Twitch makers to YouTube. The saved capture shows the initial Twitch selection, not proof of physical key operation.
- [Desktop pairing](screenshots/streamdeck-target-pairing-desktop.png): the isolated server at `127.0.0.1:18895` used dummy credentials, no destinations and stage OFF. The implementing agent observed destination permission initially unchecked and approval disabled until code confirmation, then checked both boxes. No approval button was executed and no device token was granted in the browser. The capture shows the checked state before approval.
- [Mobile pairing](screenshots/streamdeck-target-pairing-mobile.png): the implementing agent scrolled a 390×773 viewport and observed both action buttons reachable. In the full-document capture, fixed bottom navigation overlaps its original viewport region; that capture alone cannot demonstrate button reachability. This was browser viewport verification, not physical-phone testing.

These observations support conformance for this extension. They do not constitute a full keyboard, assistive-technology, contrast, zoom or all-breakpoint audit.

## Automated evidence and review

The implementing agent reports 16 passing plugin tests, with the latest focused enable/disable and inspector-extension tests also passing. Go Device HTTP tests with the race detector cover explicit permission opt-in, denial for old tokens, restart persistence, revocation and exclusion of destination secrets. Browser approval execution is not covered by the visual evidence above.

An independent finish reviewer returned “ship” with no material fixes for this scope. A generic agent performed the named review role; this is a scoped review result, not physical-device or release acceptance.

## Delivery limits

At this documentation checkpoint, full `make check` and GitHub CI were still pending. Plugin 1.2.0 had not been installed or deployed. The installed 1.1.1 icon fix had previously been visually verified in the actual macOS application; that does not verify these new controls. Physical Stream Deck presses and Windows behavior remain unverified. Exact-final-SHA CI and any required image-publication evidence remain the delivery owner's responsibility under [delivery requirements](../agents/delivery.md).


## Sidebar correction and local installation

After the initial review, the user identified that all action-list entries still used the generic Restreamer logo. Plugin 1.2.1 assigns a distinct authored SVG to each action and its initial key image: clock, broadcast waves, break cup, finish flag, clip player, status bars, stop square and destination switch. Official packaging validated all 19 files. The implementing agent installed the update through Finder and visually verified all eight distinct sidebar icons in the actual macOS Stream Deck app; existing keys continued showing current status. No broadcast command or destination toggle was invoked. This supplementary native check does not extend the independent review verdict above, and production server deployment remains pending.
