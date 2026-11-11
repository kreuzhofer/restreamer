# Restreamer for Stream Deck

Operate Restreamer from physical keys while the server continues handling playback and destination delivery. OBS is controlled separately.

## Install

Use **Stream Deck 7.1 or later** on **macOS 13+ or Windows 10+**. The plugin uses Stream Deck's bundled Node 20 runtime; end users do not install Node. It supports display keys on Stream Deck, MK.2, Mini, XL, Neo and Stream Deck +, and keys in Stream Deck Mobile connected to the desktop app. Dials, touch strips, Pedal and multi-actions are not supported. Key faces are rendered as scalable SVG and visually checked at 72px and 96px. The 1.2.1 installer, distinct sidebar icons and dynamic key faces were verified in the macOS Stream Deck app. Physical hardware presses and Windows installation still require user verification. [Earlier key-renderer proof](../../docs/testing/screenshots/streamdeck-plugin.png) records the actual-size keys; its password inspector predates browser pairing. Current pairing and revocation verification is recorded in [the test report](../../docs/testing/streamdeck-pairing.md).

1. Open the successful [GitHub CI run](https://github.com/kreuzhofer/restreamer/actions/workflows/ci.yml) for the desired commit. Download the `restreamer-streamdeck-<commit SHA>` artifact and unzip it.
2. Double-click `com.kreuzhofer.restreamer.streamDeckPlugin` to install it in Stream Deck. No Marketplace account is needed.
3. Drag an action from **Restreamer** onto a key. Expand **Server connection**, enter your Restreamer server origin and a recognizable device name, and select **Connect in browser**. Sign into Restreamer in the browser, compare the displayed code with Stream Deck, and approve only when they match. Every Restreamer key shares this connection.
4. Prepare the starting-soon, BRB and ending videos in Restreamer's **Show setup**. For **Play clip**, select a ready video in that key's settings. Add several Play clip keys for independent assignments.

Prefer HTTPS. The explicit HTTP checkbox is for a trusted local network; HTTP sends credentials without encryption. Embedded URL credentials, query strings, fragments and path prefixes are rejected. The plugin does not follow HTTP redirects or weaken server origin checks.

### Device access

New connections use a separately revocable token. Your dashboard password stays in the browser. Open **Settings → Connected devices** to revoke a lost or unused device; this does not stop your broadcast or change your password. Pairing codes expire after five minutes and pending requests are cancelled when Restreamer restarts. Approved tokens remain valid until revoked. Only approve a code you just requested and verified in your plugin.

The token permits reading broadcast status and ready videos, and the broadcast actions below. Browser approval also offers an unchecked **Control destinations** permission. Approving it allows enabling or disabling existing destinations, including while live; it does not permit editing destination URLs/keys, ingestion credentials, media, configuration or other devices. Existing tokens keep their original permissions. To use Target toggle, reconnect in the plugin and explicitly approve Control destinations. Revoke the superseded device entry afterward. Before downgrading to a server version without this permission, revoke devices that have destination control; older versions cannot read those new permission records. The raw token is saved only in Stream Deck global settings, never sent to the inspector, browser URL or exported profiles. Protect the desktop user account because its local settings are accessible to that user. Do not enable SDK trace logging: it includes settings messages. Existing password-based connections continue working until you explicitly pair again; pairing replaces that saved connection.

Restreamer requires dashboard authentication and persistent `TARGET_STATE_FILE` storage for pairing. It stores token hashes and device metadata in `<TARGET_STATE_FILE>.devices` with permissions `0600`, alongside normal state in the existing data volume. Include that file in protected backups; never edit credentials by hand. A missing file means no connected devices; a malformed file fails initialization explicitly. Up to 64 devices and 32 pending requests are supported; token polling is limited to once per three seconds per request. This is a purpose-built pairing protocol, not an OAuth server.

## Keys

| Key | Behavior |
| --- | --- |
| Prestream | Start the selected starting-soon loop. Starting from OFF uses the server's real-broadcast mode and requires an eligible destination. |
| Go live | Request OBS; show WAIT OBS until a fresh keyframe arrives. The server retains the current source if OBS is unavailable, and the key reports NO OBS on timeout. |
| BRB / Return | Enter deliberate BRB; while in BRB, return to the server's remembered stage. |
| End stream | Start the configured ending. Fixed-duration endings stop forwarding on completion; music-driven endings continue looping until explicitly stopped. |
| Play clip | Play the exact selected ready revision. Finishing returns to the stage remembered by the server. A new render never silently replaces the assignment. |
| Status | Show broadcast stage and relay delivery. Press to open `/live` in the browser; browser authentication remains separate. |
| Target toggle | Choose an existing destination in the key’s settings. **Hold for two seconds, then release** to enable/disable it. A short press does nothing; changes or loss of connection cancel the hold. |
| Stop now | **Hold for two seconds, then release.** A short press does nothing. Changing broadcast state or losing connection cancels the reviewed stop. |

Active and pending appearances follow server status, including changes from other clients. Go live is ACTIVE only while OBS is actually on air; fallback shows NO OBS. Prepared playback distinguishes ACTIVE, PAUSED and FAILED. Prestream, clip completion and ending run on the server even if Stream Deck exits. Other control keys perform their labeled action immediately; keep protected Stop now and Target toggle on their own keys. Target keys show Off, Enabled (broadcast stopped), Rehearse, Connecting, Streaming, Finishing or Error based on confirmed relay status. Platform logos are inferred from recognizable ingest hostnames; choose a logo override for shared/custom hosts such as Kick’s AWS ingest address. Destination names are the saved identity: after a rename or deletion, select the destination again. The plugin preserves an already-running rehearsal's server mode rather than silently starting a real broadcast.

## Reading status

The large label is LIVE, PRE, BRB, CLIP, END or OFF. The fraction counts streaming destinations out of enabled, eligible destinations, using the same intended set as the Live overview. It does **not** prove that a platform has made the stream public.

- Green check: all intended destinations sending; BRB can be healthy.
- Amber warning: partial delivery, unavailable OBS using fallback, or no destinations.
- Red cross: no intended destination delivering after failure, or a reported playback/fallback failure.
- Blue waiting dots: expected connection/keyframe startup without a recorded failure. Reconnecting after a failure stays alarming until delivery resumes.
- Gray question mark with UNKNOWN: no current status. Failed requests invalidate status immediately; a hung request becomes unknown within about 3.5 seconds. OFF is gray; rehearsal is blue and never claims delivery success.

INVALID means imported key settings are malformed: reselect that key’s clip or recreate the key; other keys keep working. AUTH means access was rejected: use Connect in browser again (or check an existing legacy password connection). NOT READY means prepare compatible stage media. MISSING means the assigned revision was removed. CHANGED means review the state or choose the clip again; renamed clip assignments require reselection. No commands are queued or replayed after network recovery or server restart. A lost command response can still have taken effect: inspect the current stage before pressing again.

## Development and verification

Use Node 24+ for build tooling. From this directory:

```sh
npm ci --ignore-scripts
npm test
npm run pack
```

The installer is written to `dist/com.kreuzhofer.restreamer.streamDeckPlugin`. The official CLI validates the manifest and package during packing. `make check-streamdeck` at the repository root installs locked dependencies, checks TypeScript, tests and packs; normal `make check` and GitHub CI include it. The bundled SDK uses a patched Zod override because its upstream dependency range includes a vulnerable validator; `npm audit` is clean with the committed lockfile.

Tests run the actual bundled plugin process against a simulated Stream Deck WebSocket host and authenticated local HTTP service. They cover all command mappings, immutable clip assignments, live/pending/error rendering, shared polling, credentials, staleness/auth/restart recovery, duplicate presses, browser pairing with no secret disclosure, protected stop, and destination holds with explicit pairing permission. Go HTTP tests cover approval, denial, expiration, replay, cross-origin protection, route/action scopes, bounded requests, failed persistence, restart and durable revocation. Existing Go stage/RTMP tests cover server sequencing; no test targets a real streaming platform.

For a development-only browser proof page:

```sh
npm run build
node test/preview.mjs
```

Open `http://127.0.0.1:18903`. It shows the shipping key renderer at 72/96px and the shipping inspector connected to a simulated local host. Its sample clips and credentials are dummy data, and it sends no RTMP traffic. To exercise pairing against an isolated local Restreamer, start it with `STREAMDECK_TEST_SERVER=http://127.0.0.1:18894 node test/preview.mjs`, then enter that origin in the inspector. This mode prints the approval URL; open it in a browser. Key illustrations remain synthetic. Revoke the test device afterward.

Implementation references: [official plugin runtime and manifest](https://docs.elgato.com/streamdeck/sdk/introduction/plugin-environment/), [global versus action settings security](https://docs.elgato.com/streamdeck/sdk/v2/guides/settings/), [packaging command](https://docs.elgato.com/streamdeck/cli/commands/pack/).

Platform logo paths come from [Simple Icons](https://github.com/simple-icons/simple-icons), distributed under CC0. Platform marks remain subject to their owners’ trademark policies.
