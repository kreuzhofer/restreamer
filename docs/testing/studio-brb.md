# BRB preparation in Studio (#45)

Approved seam: authenticated application browser, HTTP/status and local RTMP with disposable fixtures and dummy credentials. No new broadcast state machine or media entity.

## Scenario written before implementation

1. Live shows BRB readiness and exact prepared-candidate review/activation, plus a direct Studio link. BRB message, image/music uploads and theme preparation live only in Studio, separate from scene designs.
2. Edit the BRB message and volume in Studio; collection navigation retains unsaved changes. Validation errors remain visible. Prepare & activate retains the current BRB during processing and reports the existing activation contract explicitly.
3. Choose an exact shared-theme revision, prepare it, navigate away and return while server-owned work continues. Current BRB remains unchanged until exact candidate preview and explicit activation. Stale candidate activation remains rejected.
4. Verify missing/loading/stale status, preparation error/cancellation, unsupported streaming profiles, and disabled BRB. Readiness and current/fallback source remain visible in Live.
5. Verify desktop, narrow/tablet and phone layouts and keyboard focus. Phones retain prepared-candidate preview/activation and live BRB controls; editing, uploads and theme preparation explain that desktop is required.
6. Reuse HTTP/RTMP tests for exact candidate generation and activation, persistence, profile restrictions, source loss/fallback, publisher disconnect, and active media retention.

## Evidence (2026-10-12)

- The new authenticated workspace regression failed before implementation because the BRB form was still in Live; it passes after relocation. Targeted race tests for the complete BRB theme suite plus upload/profile atomicity, authentication, publisher reconnection and the workspace boundary passed in 20.6 seconds. `go vet ./...` and JavaScript syntax checks passed.
- Real browser on disposable `localhost:18845`: unsupported message `BRB ☃` surfaced validation while the current screen stayed ready. Saving `SHORT BREAK` showed preparation with disabled inputs, then “Saved and activated”; the persisted still preview updated.
- Prepared exact Retro revision 2 in Studio. Current BRB remained legacy styling while the candidate was marked “Prepared BRB · not active”. Exact preview reached `readyState=4`, 320-pixel native fixture width, and played through 4.01 seconds. Live independently loaded the same candidate, required its own exact preview before enabling activation, and contained no creative form. Restarting the disposable fixture visibly marked the earlier candidate stale and disabled activation.
- Inspected 1920×1080, 1440×900, 1024×768 and 390×844. Browser review found oversized checkbox layout inherited from the generator; fixed and visually rechecked. Phone document and scroll width both measured 390; authoring was hidden while exact preview and activation remained available. No browser permissions changed.
- Screenshots: [wide Studio](screenshots/brb-studio-wide.jpg), [desktop theme workflow](screenshots/brb-studio-desktop.jpg), [narrow exact preview](screenshots/brb-studio-narrow.jpg), [phone review](screenshots/brb-studio-phone.jpg), [Live review](screenshots/brb-live-review.jpg).
- Browser extension initially stalled on the obsolete #43 deletion dialog; native Chrome inspection dismissed that disposable confirmation and recovered browser access. This pass did not submit a new native activation confirmation or use the blocked browser file chooser. Authenticated HTTP/local RTMP tests cover activation, upload, source-loss/fallback, and unchanged output during candidate preparation. Final composed browser verification should revisit those confirmation/upload interactions.

- After merging the prepared-media drawer and Themes/Templates collections, the focused authenticated workspace regressions passed again; all changed JavaScript passed syntax checks. The integration preserves both the drawer and standalone BRB review.
