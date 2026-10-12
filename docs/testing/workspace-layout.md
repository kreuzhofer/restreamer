# Workspace layout verification

Approved seams: authenticated application browser and existing HTTP/status/local RTMP regressions. Disposable configuration and media only.

## First scenario, written before implementation

On a full desktop, resize Live's secondary panel and Studio's scene/property panels with a divider and keyboard. Keep panels in place, minimum control sizes usable, and monitor content proportional. Reload the page and restore the same widths. Reset layout restores defaults. Existing baseline has fixed 320px Live and 220px/330px Studio columns with no dividers, persistence, or reset, so it cannot perform this scenario.

## Follow-up scenarios

- Narrow desktop substitutes accessible panel tabs without losing draft text, playback, or selection. Resize back to desktop; preserve preferred widths and ensure focused controls do not become invisibly stranded.
- Show queue, errors and multiline shared alerts; the remaining workspace uses actual available height rather than fixed header subtraction. Long panels scroll inside their areas. Media exact preview retains its native aspect ratio.
- Malformed or unavailable browser-local storage visibly reports recovery. Save/reset errors remain visible; no secrets, design text, or broadcast state enter layout storage.
- Desktop, narrow, and phone screenshots; phone continues the separate mobile remote behavior and never displays desktop resize/admin controls.
