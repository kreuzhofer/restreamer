## Problem Statement

Restreamer has grown from a relay dashboard into a broadcast production application, but its interface still places live operation, media preparation, connection setup, and diagnostics in long scrolling pages. Operators cannot easily see the outgoing picture, control the broadcast, and monitor destinations together. Media authoring mixes library administration with scene editing. On phones, the desktop interface largely stacks into an even longer page.

The operator needs a full-window desktop production studio and a focused mobile broadcast remote, with clear distinctions between editing a draft, preparing media, and changing what is on air.

## Solution

Reorganize the application into **Live**, **Studio**, and **Settings**, following the dark production-console visual direction approved during the design interview.

- **Live** brings the outgoing broadcast monitor, a smaller OBS input monitor, stage controls, clip shortcuts, destination health, and off-air readiness/rehearsal into one operating workspace. Detailed playback and diagnostics appear on demand.
- **Studio** separates Designs, Media, Themes, and Templates. Opening a design presents a scene list, central preview, and properties panel, with generation results and history kept distinct from the active broadcast. BRB preparation joins prestream and ending preparation here.
- **Settings** provides desktop connection and technical setup, including ingestion-key management, destination configuration, and the shared streaming profile.
- **Desktop** uses the full window, with adjustable panel dividers, stable panel positions, browser-local layout preferences, and drawers or tabs when space is limited. It does not reserve space for OBS beside the app.
- **Mobile** prioritizes broadcast operation, destination delivery switches, and browsing, previewing, and selecting prepared media. Authoring, uploads, regeneration, and technical configuration require desktop in the initial version.

Broadcast status and actionable failures remain visible across workspaces. Preparation never silently changes the media on air.

## User Stories

1. As an operator, I want separate Live, Studio, and Settings workspaces, so that each screen supports the job I am doing.
2. As an operator, I want compact broadcast status across workspaces, so that I can prepare media without losing awareness of the broadcast.
3. As a desktop operator, I want the application to use the full window, so that the studio has room for meaningful monitoring and control.
4. As a desktop operator, I want preview areas to resize while controls retain usable minimum sizes, so that window resizing does not make operation impractical.
5. As a desktop operator, I want to resize panels and retain their positions, so that I can tune space allocation without losing familiar controls.
6. As a desktop operator, I want panel sizes remembered in my browser and a Reset layout action, so that my preferred arrangement survives revisits and is easy to recover.
7. As an operator in a smaller window, I want secondary panels available through drawers or tabs, so that primary controls remain accessible without a long stacked desktop page.
8. As an operator, I want the outgoing broadcast monitor to be prominent and distinct from the smaller OBS input monitor, so that I understand the difference between the incoming source and selected broadcast feed.
9. As an operator, I want to pause browser previews independently of delivery, so that monitoring need not consume continuous video bandwidth or change the show.
10. As an operator, I want selected broadcast stage, actual on-air source, and pending transition shown separately, so that a requested change cannot look like a completed transition.
11. As an operator, I want stage controls near the broadcast monitor, so that I can see the result of a stage change without scrolling between sections.
12. As an operator, I want deliberate BRB and automatic fallback to remain distinguishable, so that I understand why BRB is on air and what will happen when OBS returns.
13. As an operator, I want applicable Return and Stop controls and existing confirmations retained, so that interrupting or ending a show remains deliberate and predictable.
14. As an operator, I want compact destination health and delivery switches in Live, so that I can manage delivery independently of connection configuration.
15. As an operator, I want destination failures identified individually, so that one broken output does not make healthy outputs appear broken.
16. As an operator, I want connection and relay-delivery status distinguished from platform-side public playback, so that the interface does not claim viewers can watch without evidence.
17. As an operator, I want stale or disconnected application status visibly marked, so that an old healthy snapshot is not mistaken for current health.
18. As an operator, I want pinned clip shortcuts in Live, so that commonly used prepared clips are immediately available.
19. As an operator, I want the full prepared-media browser available on demand, so that I can choose an unpinned clip without permanently occupying monitoring space.
20. As an operator, I want contextual pause, seek, loop, and return controls for clip playback, so that detailed operation is available when relevant.
21. As an operator, I want the original return stage preserved when replacing clips or taking BRB, so that the redesign does not change existing playback semantics.
22. As an operator while OFF, I want a concise readiness check for input, stage media, and destinations, so that I can see what is ready before starting.
23. As an operator while OFF, I want clear choices to start a real broadcast or enter a preview-only session, so that rehearsal is easy to find.
24. As an operator rehearsing, I want an unmistakable indication that destination delivery is blocked, so that I do not confuse rehearsal with a real broadcast.
25. As an operator, I want readiness and alerts interpreted in the context of the selected stage, so that absent OBS during planned prepared-media playback does not imply the broadcast is failing.
26. As a media author, I want Designs, Media, Themes, and Templates organized into distinct Studio sections, so that collection management does not clutter scene editing.
27. As a media author, I want selecting a design to open a scene list, preview, and properties panel, so that I can edit content while seeing its effect.
28. As a media author, I want existing scene layouts, timing, transitions, soundtrack, and style controls retained in the focused editor, so that the redesign does not reduce authoring capability.
29. As a media author, I want BRB creation and styling in Studio alongside prestream and ending, so that creative preparation has one home.
30. As an operator, I want BRB readiness and activation retained in Live, so that relocating the editor does not hide live controls.
31. As a media author, I want templates, independent show designs, and versioned style themes to keep their existing meanings, so that reusing content or adopting a theme does not unexpectedly change another design.
32. As a media author, I want to edit and generate during a broadcast, so that I can correct upcoming media without interrupting what is playing.
33. As a media author, I want validation, saving, and conflicts clearly reported in the relevant editor context, so that navigation or failed writes cannot masquerade as a saved change.
34. As a media author, I want each design's latest generated result and readiness easy to find, so that I can judge whether it is ready for review.
35. As a media author, I want older generated revisions in design history and a compact shared generation queue, so that prior outputs and background work remain accessible without dominating the editor.
36. As a media author, I want existing generation cancellation and retry behavior retained, so that I can recover failed or interrupted work without losing the captured revision.
37. As an operator, I want draft, prepared output, and selected broadcast media visibly distinct, so that saving or generating cannot be confused with going on air.
38. As an operator, I want to preview an exact ready revision and explicitly activate it against current broadcast state, so that a background job or stale confirmation cannot replace on-air media unexpectedly.
39. As an operator, I want persistent failure alerts while in Studio or Settings, with a direct route to Live, so that I can respond without the application automatically moving me away from my work.
40. As an operator, I want detailed counters and diagnostic graphs on demand, so that troubleshooting remains possible without crowding routine operation.
41. As a media author, I want deletion controls available in the relevant Studio collections and result histories, so that cleanup remains straightforward after the reorganization.
42. As a media author, I want design deletion to remove eligible generated files while preserving uploaded assets, so that cleanup follows the established ownership rules.
43. As a media author, I want uploaded assets managed separately and referenced or playing media protected, so that collection cleanup does not break a design or broadcast.
44. As a desktop operator, I want ingestion-key generation, regeneration, and copying in Settings, so that connection setup belongs in a dedicated workspace.
45. As a desktop operator, I want destination definitions and credentials managed in Settings, so that Live can focus on their readiness and delivery.
46. As a desktop operator, I want streaming-profile changes in Settings with the existing OFF-only restrictions, so that configuration cannot bypass broadcast protections.
47. As a mobile operator, I want the outgoing preview, stage controls, clips, and destination health arranged for touch operation, so that my phone is a useful broadcast remote.
48. As a mobile operator, I want to browse, preview, and select prepared media, so that I can operate a show without opening a desktop editor.
49. As a mobile operator, I want destination delivery switches and Stop available with the established confirmations, so that I can respond to broadcast problems remotely.
50. As a mobile operator, I want a clear desktop-required explanation for unavailable authoring and setup tasks, so that hidden controls do not look like broken features.
51. As an operator using keyboard navigation or assistive technology, I want labeled controls, visible focus, accessible resizing and drawers, and status conveyed through text as well as color, so that the production interface remains usable.
52. As an existing user, I want saved designs, selections, media revisions, configuration, and bookmarked entry points preserved or compatibly routed, so that the redesign does not require rebuilding my studio.

## Implementation Decisions

1. Implement a shared application shell for Live, Studio, and Settings using the project's embedded, self-contained web assets. Preserve the native Go relay and existing server-owned broadcast controller. A frontend build-toolchain migration is not part of this work.
2. Treat this as an interface and workflow reorganization, preserving existing authenticated HTTP contracts wherever possible. Do not introduce parallel client-owned stage logic or bypass command confirmations, version checks, or same-origin protections.
3. Use the approved dark production-console aesthetic: compact chrome, clear typography, useful density, and strong state hierarchy. Arcade and Neon aesthetics remain media styles rather than the application theme. Illustrative artwork and simulated values from the concept are not product data.
4. Desktop panels keep consistent positions, have adjustable dividers, and retain browser-local sizes. Provide Reset layout and clamp restored dimensions to the current available space. Preview images retain their aspect ratios. Long collections and detailed properties may scroll within their panels; core live controls and compact health belong together in the supported full-screen desktop layout.
5. Use responsive content fit to choose breakpoints. In narrower desktop and tablet layouts, reveal secondary panels through drawers or tabs instead of shrinking controls excessively or stacking the entire desktop workspace. Final dimensions are verified against real content rather than copied mechanically from the mockup.
6. Mobile capability limits are presentation and workflow choices, not a replacement for server-side authentication or authorization. Desktop-only activities in the initial version are text editing, scene composition, theme editing, uploads, regeneration, ingestion-key management, destination configuration, and streaming-profile changes. Mobile retains prepared-media review/selection and operational broadcast controls.
7. Live prioritizes the actual selected broadcast feed and distinguishes it from OBS input. Preserve optional browser-only preview controls. Show requested stage, actual source, pending changes, and delivery separately, including OFF, no eligible destinations, rehearsal, waiting for a source, and stale status.
8. Preserve all existing stage rules: keyframe readiness, deliberate versus fallback BRB, clip replacement and original return context, suspended playback, explicit immediate-stop confirmation, ending completion/draining restrictions, and preview-only delivery isolation. Readiness information must not add a new universal OBS prerequisite to prepared-media starts or silently enable destinations.
9. Put compact per-destination health and existing delivery actions in Live. Surface active failures across workspaces without forced navigation, and provide access to detailed metrics on demand. Alerts must distinguish expected source absence from a source loss that affects the intended stage. Do not keep stale health looking current or claim platform-side public playback from relay evidence.
10. Place pinned clip shortcuts in Live. Open the full library and detailed clip playback controls on demand, retaining existing selection, version identity, loop, seek, pause, return, and confirmation behavior.
11. While OFF, show readiness and entry to real broadcasting or preview-only rehearsal in Live. Preserve the requirement to end rehearsal before starting a real broadcast; the mode and blocked destination delivery must remain visible throughout rehearsal.
12. Studio navigation separates Designs, Media, Themes, and Templates from the focused editor. Preserve independent show designs, content templates, immutable theme revisions, explicit theme adoption, and all existing layout, scene, soundtrack, transition, loop, ending-fade, validation, save/conflict, and recovery behavior. Do not introduce a new whole-show bundle entity.
13. Move BRB preparation and styling into Studio without implying that BRB becomes a scene-sequence show design. Live retains readiness and stage control; the existing BRB preparation/activation contract remains authoritative.
14. Associate generated results and revision history with their design, while presenting the shared bounded queue compactly. Retain cancellation/retry and exact captured inputs. A failed or in-progress generation cannot erase the last ready revision. Existing prepared media remains on air until exact review and explicit valid activation.
15. Preserve deletion ownership and reference protection: deleting a design removes its eligible generated results and files, never implicitly uploaded assets. Keep separate upload management and existing protections for selected, referenced, playing, and return-stage media. Continue surfacing persistence/cleanup errors rather than reporting false success.
16. Settings contains ingestion management, destination management, and streaming-profile configuration. Keep credentials masked and restricted to their explicit management flows; never expose them in global status, alerts, previews, metrics, logs, or layout storage. Preserve validation, persistence, active-show confirmations, and profile restrictions.
17. **Connection-management dependency:** GitHub main and the inspected checkout are at `abd9cfeff54534738138849491c69c2fe7acbd84`. Ingestion-key management (#33) and destination management (#35) are still open, and their management interfaces are absent in this baseline. Those tickets own their backend behavior, secret storage, and migration requirements. The Settings portions of this redesign must integrate their real completed behavior; placeholders or environment-only instructions do not satisfy the final Settings acceptance. Unrelated Live/Studio work can proceed independently. Dependency edges must be made explicit during ticket breakdown.
18. The approved redesign changes the requested UI placement in #33 from the old ingestion card to Settings, and supersedes #33/#35's mobile-management UI expectation with desktop-only setup. Their security, persistence, migration, and broadcast behavior requirements remain intact. Do not silently close or rewrite those issues as part of publishing this spec.
19. Preserve existing route access or provide compatible routing to new workspace locations. Workspace changes must not issue broadcast commands, lose unsaved state silently, or cancel server-owned generation. Authentication remains required for dashboard surfaces; the health endpoint remains public.
20. Preserve observable error and recovery states: loading, empty collections, validation failure, persistence failure, stale control context, unavailable previews, generation failure, and status disconnects. Avoid inert production buttons or silently hidden degradation. Accessibility must cover focus movement for drawers/dialogs, keyboard operation, readable contrast, and touch targets.

## Testing Decisions

The user explicitly approved this testing approach during specification preparation.

1. Use the **authenticated application boundary** as the main test seam. Browser tests operate the real application; existing HTTP/status and local RTMP tests verify externally observable consequences. Prefer behavior assertions over DOM implementation details, CSS snapshots, private helpers, or duplicate miniature state machines.
2. Reuse the existing authenticated dashboard test helpers and disposable configuration/media fixtures. Their prior coverage includes authentication, same-origin mutation rejection, stale command context, persistent destination preferences, and secret redaction.
3. Reuse the existing stage-command, media-revision, generator, rehearsal, and local RTMP sink tests for server behavior. Existing timed-tick tests remain the seam for deadlines and ending behavior; do not replace deterministic tests with long browser sleeps.
4. Browser coverage must exercise Live/Studio/Settings navigation, primary controls near the output monitor, clip shortcuts and detailed controls, resource browsing, design editing, validation/conflicts, generation history/queue, exact preview/activation, BRB preparation, and Settings integration. Verify actual behavior and status, not merely that HTML contains labels.
5. Verify full-screen desktop, narrower desktop/tablet, and phone layouts with real content. Representative checks include 1920×1080 and 1440×900 desktop, a narrow window/tablet around 1024×768, and phone around 390×844, with additional widths where content exposes a breakpoint problem. Exact breakpoints are implementation choices. Confirm no clipped controls or unintended horizontal overflow; desktop core operation should not require document scrolling between monitoring and primary controls.
6. Verify divider bounds, browser-local size restoration, Reset layout, viewport changes, keyboard resizing or an accessible equivalent, drawer/tab focus, and restoration of a usable layout after a narrower window opens. Phone workflows must work through visible touch controls, while desktop-only activities show a clear explanation.
7. Verify health presentation for healthy LIVE, deliberate BRB, prestream without OBS, fallback, pending transition, partial and total destination failure, OFF, no eligible destination, status disconnection, and recovery. Inject failures while editing and verify visible alerts without forced navigation or loss of draft state.
8. Verify broadcast safety through existing public behavior: preview-only sessions open no destination deliveries; generation while broadcasting retains active media; exact-revision review and stale confirmations are enforced; clip returns and ending completion remain correct. Preview pause or workspace navigation must not change delivery.
9. Retain physical file checks for deletion: remove eligible generated files and metadata, preserve uploads and shared/referenced media, reject protected playback deletion, and report failed cleanup/persistence accurately. Use the existing deletion and recovery tests rather than asserting only that a row disappeared.
10. Connection-management acceptance includes #33/#35's authenticated management, persistence, migration, redaction, and local RTMP checks when those dependencies land. Verify desktop forms and mobile operational delivery controls according to the newly approved capability boundary. Use dummy credentials and local sinks only; never broadcast tests to real platforms or change user credentials for fixtures.
11. Follow test-first development for behavior changes, run the repository's normal race tests and vet checks, and visually inspect desktop and mobile with screenshots. Update documentation for the new workspace locations and device boundaries.
12. Delivery requires pushing the final implementation commit and successful GitHub CI for that exact SHA. A main release additionally requires successful image building and publication. Local checks, a local commit, or an individually green CI job are insufficient. Publishing this specification is planning, not a release claim.

## Out of Scope

- Freely detachable or rearrangeable docks, multiple independent production windows, or cross-browser/account synchronization of panel layout.
- Full mobile authoring, uploads, regeneration, theme editing, credential management, destination configuration, and streaming-profile editing in the initial version.
- A new whole-show package model, new scene renderer or media theme artwork, replacing OBS, or changing the existing stage and media-revision semantics.
- Multi-user support, a Stream Deck plugin, native RTMPS ingestion, platform audience analytics, or platform API verification of public playback.
- A standalone rewrite of the relay, a new frontend build system, or unrelated diagnostic/backend refactoring.
- Re-specifying the credential persistence and migration work owned by #33 and #35. Their completed capabilities are dependencies of Settings, not implicitly delivered by this UI spec.

## Further Notes

- The design interview is complete. The user approved the visual direction, all sixteen numbered decisions, and the consolidated scope, then explicitly approved the testing approach above.
- The clickable concept and Live/Studio screenshots are illustrative references, not implemented features. This issue is self-contained for workflow and acceptance requirements; its behavior contracts take precedence over incidental mockup details. Preserve the visual references with the implementation handoff without treating local-only mockup locations as public links.
- Related issue #2 proposes an On Air overview. This spec incorporates the approved focused Live experience into a broader workspace redesign. Its original idea of retaining a separate detailed dashboard does not prescribe the new navigation; diagnostics remain accessible on demand. Publishing this spec does not close #2 or promise its separate Stream Deck/shared-health-contract work.
- Existing broadcast sequencing and generator work in #6, #4, and #5 supplies the behavior being reorganized. Tracker open/closed status alone is not proof that a behavior is absent; implementation must inspect the actual baseline and preserve existing tested contracts.
- Break down this specification into independently verifiable end-to-end tickets with genuine blocking relationships. Reconcile the outstanding connection-management dependencies before claiming the complete redesign delivered.
