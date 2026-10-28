# Approved prototype parity correction

The approved October UX prototype remains the visual reference. This pass restores five areas identified by two independent reviewers, prioritizing Music and the shared editor.

| Area | Corrected behavior |
| --- | --- |
| Music | Tracks / Playlists views, current-view search and retained deep links, primary action in Library header, compact rows and usage counts. |
| Shared editor | Content / Appearance / Music in scene and BRB designs; scene thumbnails and one Add scene entry; latest video with captured music, date and match status beneath the draft; exact result review with keyboard focus. |
| Live | Explicit Rehearse / Start broadcast with starting-source selection; stage controls and Stop before clips at desktop and phone widths; mint selection; secondary playback details collapsed. |
| Show setup | Separate stage context, larger preview affordance, ready state and closed fallback summary; Open Live as a navigation link. |
| Settings | Flat destination rows; primary Add, neutral Edit and quiet destructive Remove; credentials and confirmation behavior preserved. |

## Evidence

Screenshots are from the isolated copied test fixture on 127.0.0.1:18886, using dummy credentials and synthetic library data (100 music tracks, ten playlists). Any session shown is a preview-only rehearsal; all destinations remained disabled. The rehearsal was stopped after verification.

- Desktop 1440 × 900: all five areas, plus both editor types and stopped/active Live.
- Narrow: Show setup at 800 × 900, both editors at 1024 × 768 with panel switching.
- Phone 390 × 844: Live, Show setup, Music and open generation activity; no horizontal overflow. Stop remains directly beneath stage buttons. Phone authoring actions stay hidden.
- Browser checks: Tracks/Playlists switching and filtered deep-link restoration; both Review video actions focus the exact video and start playback; preview-only start/stop confirmations; no captured browser console errors.
- The browser approval reviewer rejected adding a scene because it could autosave. No browser mutation of that copied design was performed. Latest-result match behavior is covered by automated tests; preview rendering and thumbnails were inspected read-only.
- Two independent follow-up reviews closed the scoped findings; their reports are retained here. Parent-executed browser checks are attributed separately in the reports.

## Automated validation

- 62 Node web tests pass, including new current-view search, latest-result identity and explicit session-mode cases.
- Focused Go race checks for authenticated workspace navigation, Live controls and BRB preparation pass.
- go vet passes.
- Full local make check encountered ten existing FFmpeg 7.1.5 transition failure groups previously reproduced on unchanged main. It also identified the old rehearsal-checkbox assertion; that assertion was updated for the explicit controls and its focused race rerun passed. The full local suite is not claimed green. Exact-SHA GitHub CI remains the required delivery gate.
- Final static layout detector: eight cramped-padding findings. Rendered/source inspection identifies flat containers and cascade limitations, with no corresponding scoped spacing defect. No blanket suppression was added.

## Boundaries

The pass does not alter API schemas, credential storage, generation formats, playlist pinning, exact-revision assignment, or broadcast delivery semantics. Rehearsal and real-start actions use the existing context-bound confirmation path. Scene thumbnail requests are sequential, cancelled on edits and discarded when stale; only a bounded current-design cache is kept.

Physical iPhone playback was verified in the previous release, not repeated for this CSS/interaction pass. These captures verify browser viewport behavior; they do not claim comprehensive assistive-technology or every-browser coverage.
