# Workspace layout verification

Approved seams: authenticated application browser and existing HTTP/status/local RTMP regressions. Disposable configuration and media only.

## First scenario, written before implementation

On a full desktop, resize Live's secondary panel and Studio's scene/property panels with a divider and keyboard. Keep panels in place, minimum control sizes usable, and monitor content proportional. Reload the page and restore the same widths. Reset layout restores defaults. Existing baseline has fixed 320px Live and 220px/330px Studio columns with no dividers, persistence, or reset, so it cannot perform this scenario.

## Follow-up scenarios

- Narrow desktop substitutes accessible panel tabs without losing draft text, playback, or selection. Resize back to desktop; preserve preferred widths and ensure focused controls do not become invisibly stranded.
- Show queue, errors and multiline shared alerts; the remaining workspace uses actual available height rather than fixed header subtraction. Long panels scroll inside their areas. Media exact preview retains its native aspect ratio.
- Malformed or unavailable browser-local storage visibly reports recovery. Save/reset errors remain visible; no secrets, design text, or broadcast state enter layout storage.
- Desktop, narrow, and phone screenshots; phone continues the separate mobile remote behavior and never displays desktop resize/admin controls.

## Green evidence (2026-10-12)

The authenticated disposable app at localhost:18847 verified the actual controls:

- Live keyboard resizing changed Signals from 320 to 370 pixels; reloading restored 370. End clamped to 440, the menu slider handled Home, and Reset restored 320 with visible feedback. Pointer dragging moved the divider from 320 to 370 without moving panel positions.
- PRESTREAM rehearsal played the previously selected exact revision with destination delivery blocked. The outgoing video reached readyState 4 and advanced time. At 1440×900 the console bottom was exactly 900; at 1280×720 its bottom was 720, the stage controls ended at 702, and the visible monitor retained 122px height. No horizontal overflow occurred.
- At 820×900, Broadcast and Signals & health tabs changed only the visible secondary panel. Playback stayed ready; when the focused divider disappeared, focus moved to Broadcast. Desktop preferences survived narrow widths.
- Studio Scenes/Properties reached 360/340 pixels and survived reload. Edited text survived a 760→1024 transition and autosaved. The first browser pass exposed lost focus when Scenes became unavailable above 800px; the corrected pass explicitly focused Preview, with the editor still showing the retained draft. At 1024×768 an expanded 129.5px queue and visible stale-status warning left the editor bottom at 752, inside the window.
- After merging the mobile/BRB slices, the phone remote retained its own panels. At 390px, desktop controls/dividers/tabs had no rendered rectangles and document width remained 390. Full phone operation was already verified by #48; this check covered coexistence only.
- A disposable synthetic uploaded MP4 was prepared through the authenticated HTTP API. Browser exact playback reached readyState 4 and completed at 2.987 seconds. Intrinsic 1280×720 video displayed at 720×405 on desktop and 320×180 on phone, preserving 16:9. The screenshot shows a reviewed frame, not a loading spinner; the collection scroll reveals the complete player.

Screenshots: [Live desktop](screenshots/layout-live-desktop.jpg), [Live narrow](screenshots/layout-live-narrow.jpg), [Studio desktop](screenshots/layout-studio-desktop.jpg), [Media exact playback](screenshots/layout-media-preview.jpg), [phone coexistence](screenshots/layout-phone.jpg).

Storage failure injection used focused Node tests against the actual UI script's DOM/events and localStorage boundary, following the existing repository UI test pattern approved for this slice. Malformed JSON, unsupported versions and invalid dimensions visibly restore defaults; Reset repairs writable preferences. Unavailable storage leaves temporary resizing/reset usable with visible persistence failure. A red test exposed a crash from an unexpected saved field; restoration now copies only known size fields. These are boundary tests, not claims that browser settings were changed to disable storage.

Three layout boundary tests passed, alongside the existing web suite, application build, JavaScript syntax and `go vet ./...`. Focused race tests `TestWorkspaceNavigationPreservesBroadcastAndAuthentication`, `TestDashboardAuthenticationAndControl`, and `TestDashboardRejectsCrossOriginControl` passed. Root owns the final combined race/vet and exact-SHA CI/release verification. The disposable fixture was stopped after evidence capture; data remains available.
