# Assessment A — Restreamer current interface

Method: independent design-review sub-agent `/root/ux_baseline_design`; no detector output inspected. Operate mode. Screenshot-backed limited walkthrough assessment, 2026-10-28. Browser operation was performed by the parent agent; this reviewer independently inspected its screenshots without detector output. **Full baseline coverage remains incomplete.**

Target: `internal/relay/web/index.html`, `generator.html`, and associated workspace/editor modules. Intended local fixture: `http://127.0.0.1:18871`, disposable documented authentication, zero destinations. No app settings or repository files changed.

## Evidence and limitations

This reviewer's fresh Chrome tab attempts through CUA failed with `The browser access host is unavailable`, including a runtime reset. The parent subsequently restored browser access in its own context and performed the actual authenticated Show setup → BRB editor → Music walkthrough at 1440×900 against the local fixture. This reviewer inspected these supplied actual browser screenshots independently, without detector findings:

- `/tmp/ux-baseline-setup.png`: Show setup with all three selected videos.
- `/tmp/ux-baseline-brb.png`: BRB editor initial view.
- `/tmp/ux-baseline-music.png`: Music tracks and beginning of playlist list.

The parent additionally reported DOM confirmation of two Music volume controls (50% and 100%), separate Prepare music presentation and Prepare preview commands, and the always-present playlist form. Those are attributed parent observations, not interactions performed by this reviewer. Source inspection independently corroborates them.

The screenshots verify the listed desktop composition and visible content. They do not establish completed generation/selection, preview playback, phone fit, keyboard sequence, recovery behavior, or current settings. No screenshot-backed claims are made here for Live, Starting soon/Ending editors, chooser, Media, Themes or Settings. Parent navigation is actual runtime evidence; the user-friction interpretation is this reviewer's judgment. Full desktop/phone baseline coverage remains incomplete, and physical iPhone playback is a separate gate.

Read the full Impeccable critique and Operate references, approved UX plan, glossary and ADRs 0007/0008. The approved plan supplies intended outcome, not defect evidence. No repository files or settings were changed by this reviewer.

## Design specificity

The interface has a product-specific foundation: selected stage versus on-air source, separate outgoing/input monitors, deliberate rehearsal, exact prepared-media review, and retained source identities. These are broadcasting concepts with operational value. Its main weakness is structural accumulation: resource management and authoring have been attached to a Studio shell, while three different BRB operations coexist. Precision and continuity of task would contribute more than stronger branding. The screenshots confirm a coherent dark neutral console, restrained status accents and readable primary labels. Show setup is a good existing foundation: three selected-video cards and direct actions fit in the 1440×900 viewport. BRB loses that clarity: several navigation bands precede a very tall form, with empty left space and the music controls below the first viewport.

## Heuristic scores — limited coverage

All ten apply to this Operate surface. These are judgments from three rendered desktop views plus source inspection. Scores for unvisited workflows remain provisional; do not treat this as a complete desktop/mobile benchmark.

| # | Heuristic | Score / 4 | Evidence and practical limit |
|---|---|---:|---|
| 1 | Visibility of system status | 3 | Global stage/source/freshness and stale alerts; generation/save states. Readable status is visible in all screenshots. BRB shows a broken preview image beside Protection ready, leaving preview failure unexplained. |
| 2 | Match with real world | 2 | Understandable broadcast stages coexist with Prestream, exact revision, render, prepared preview, presentation, and activation vocabulary. |
| 3 | User control and freedom | 3 | Preview closure, explicit selection, cancellation, dirty-form guards and recovery actions exist. Collection navigation replaces history entries, weakening expected Back behavior. |
| 4 | Consistency and standards | 1 | BRB has appearance activation and music-presentation preparation; other designs generate saved drafts. Navigation, editor switches and commands use overlapping button vocabulary. |
| 5 | Error prevention | 3 | Exact selections, stale gating, protected deletion and destination consequences are explicit in source. BRB's separate current-visual capture remains a serious expectation trap. |
| 6 | Recognition rather than recall | 2 | Named results, thumbnails, dates and change summaries help. Generating requires discovering Render history; music and BRB dependencies require remembering prior state. |
| 7 | Flexibility and efficiency | 2 | Resizable layouts and contextual edit links exist. No search/filter or bulk flow is evident in reviewed collection markup; long catalogs imply substantial scanning. Keyboard efficiency not tested. |
| 8 | Aesthetic and minimalist design | 1 | Persistent creation forms, multiple nav rows and technical prose compete with the task. Setup is clear, but BRB repeats navigation and leaves its useful lower controls out of the initial viewport; Music switches from compact tracks to large playlist cards. |
| 9 | Error recovery | 3 | Retained drafts, retry/reload/copy, explicit stale information and destination uncertainty recovery exist in source. Failure paths not exercised. |
| 10 | Help and documentation | 2 | Abundant contextual explanations cover real consequences, but paragraphs repeatedly teach implementation details instead of sequencing the task. |
| **Total** | **Limited-coverage score** | **22 / 40** | **Acceptable band: significant improvements needed. Three-view desktop evidence only; not a complete baseline or like-for-like future trend.** |

## Strengths to preserve

1. **Honest operational state.** `workspace.js` distinguishes selected stage, actual source, rehearsal delivery blocking, stale information, pending transitions and destination failures. The UI does not equate the local feed with confirmed platform playback.
2. **Exact result review and next-use protection.** `show-setup.js` labels next-use assignments, provides previews and meaningful history changes, gates stale actions, and preserves current/suspended playback. The redesign should improve recognition without collapsing these contracts.
3. **Useful recovery scaffolding.** The ordinary editor exposes retry save, reload saved draft and save a local copy; destination errors retain the draft and ask for a refreshed review. Explicit mobile authoring boundaries and labelled controls already exist.

## Priority issues

No P0 is established. Three P1 and two P2 findings follow. A1/A2/A4 have supporting rendered evidence; A3/A5 remain source-derived until their workflows are inspected.

### A1 — P1: BRB editing does not describe one complete output

**Task:** Change BRB message/theme and playlist, generate, preview and use it.

**Source fact:** `generator.html:108–133` exposes Prepared preview / Activate prepared BRB, Content & music, legacy Apply uploads & activate, a separate music presentation mount and an Appearance preparation action. `brb-presentations.js:21` explicitly says “Use the current BRB visual”; its submit at line 83 captures `dashboard.brb_assets.generation`. That is separate from unactivated message/theme draft edits. The music form alone presents seven properties plus its submit action.

**Rendered evidence:** Setup shows “Browser QA break · Render 4” selected for BRB. Opening its editor instead presents “Fallback BRB is prepared”, “Default arcade animation · 32-second loop · still preview · Silent audio”, and a broken Assigned screen image (the “Current BRB screen” alt text is visible) under a green “Protection ready” badge. The specific selected music presentation is not identifiable in the initial editor view. Screenshot: `/tmp/ux-baseline-brb.png`; compare `/tmp/ux-baseline-setup.png`. This does not prove fallback output is broken; it proves the editor preview fails to show its image and does not explain that failure. Parent DOM inspection confirms separate 50% and 100% music volume controls.

**Predicted friction:** The operator can reasonably expect Prepare music presentation to include the message just edited on the same screen. Instead, understanding the process requires distinguishing draft, candidate, active fallback and separately assigned presentation. Appearance activation is an extra prerequisite with different on-air consequences.

**Correction:** Implement the approved unified draft → Generate video → exact preview → Use for BRB flow. Keep playback protection explicit and make source-unavailable override a separate contextual choice. Collapse advanced fades/behavior with meaningful summaries.

**Retest:** From setup, change message and playlist without activating appearance first; the generated video contains both, generation leaves current/selected content unchanged, and use updates only next use. Owner: prototype/unified-editor slice. Suggested command: `$impeccable shape`.

### A2 — P1: Studio navigation requires a mental map that does not match the task

**Task:** Return after a week, locate show setup, edit one stage, then return to the same context.

**Source fact:** `generator.html:27–41` contains global Live/Studio/Settings, six Studio collection buttons, shared queue and an additional editor-stage navigation row. The editor also adds preview/history and Scene/Design/Template property tabs. `generator.js:39–57` uses `history.replaceState` for collection movement. `show-setup.css` attempts to hide `body[data-studio-view=editor]>.studio-collections`, but the collection nav is under `main`, so this direct-child selector does not match that structure.

**Rendered evidence:** The BRB screenshot visibly retains the six collection items above the queue, then another Show setup / Design editor / Prestream / BRB / Ending row. Global navigation and status precede all of these. The editing card begins about 268px down in a 900px-high view. `/tmp/ux-baseline-brb.png`.

**Predicted friction:** Navigation levels remain visible together and use similar affordances. “Studio” gives no direct recognition of show setup versus reusable resources. Browser Back cannot retrace each collection selection because those entries are replaced.

**Correction:** Approved four labelled destinations; dedicated editor with one clear originating breadcrumb; local tabs visually attached to content; route state and Back preserve useful context. Remove the redundant stage/collection navigation from editor work.

**Retest:** Navigate Live → Show setup → editor → Music picker → editor → originating setup using controls and browser Back, with no coaching or loss of selection. Owner: shell/prototype slice. Suggested command: `$impeccable distill`.

### A3 — P1: The next step after editing is hidden behind history

**Task:** Edit a Starting soon or Ending design and create its new video.

**Source fact:** `generator.html:145–151` defaults to Scene preview; the Generate saved draft button lives inside hidden `#studio-results`, reached via the Render history tab. Music is inside Design properties (`generator.html:169–175`), while the preview and properties compete for the same panel on narrower widths (`generator.css:123` onward).

**Predicted friction:** “History” reads as past outputs, not the place to create the next one. Operators must discover that change of view before generating, and narrower layouts require a further switch to inspect music and then review the result. This is avoidable task friction even though the command is technically available.

**Correction:** Keep one Generate video action and draft/save/readiness state in the editor's persistent task area. History is secondary. Keep music in the same conceptual location for every stage and identify which changes a ready result contains.

**Retest:** Fresh operator edits title/music and finds Generate without opening history; completing generation does not silently select it. Owner: dedicated-editor prototype slice. Suggested command: `$impeccable layout`.

### A4 — P2: Resource browsing is mixed with creation and maintenance

**Task:** Find an existing playlist/theme/design and inspect it without starting a new item.

**Source fact:** Designs starts with a new-design form (`generator.html:47`); Music appends playlists after the track list (`:138`), and `music-playlists.js:19` always appends a playlist edit/create form after the playlist list. Themes presents settings/publishing controls alongside browsing (`generator.html:92`). Media exposes technical upload limits and permanent upload form before the list (`:53–66`). The inspected views provide no collection search/filter controls.

**Rendered evidence:** `/tmp/ux-baseline-music.png` shows two useful compact track rows, followed by a Playlists heading, explanation, Refresh command and large playlist cards. The second playlist is already cut at the viewport bottom, even with only two tracks above it. The parent confirms the creation/edit form remains below the list. The mixed density and resulting vertical burden are observed; catalog-scale measurements are not.

**Predicted friction:** Existing content is preceded or followed by unrelated creation controls. A long track list separates the Music entry from playlists; a long playlist list separates Edit playlist from its form. This is an evidence-backed scale risk, not a measured scroll count.

**Correction:** Consistent Tracks/Playlists views; clear New/Upload/Edit commands opening contextual forms; search/filter and compact resource lists; make shared-resource publishing distinct from browsing.

**Retest:** With the approved representative catalog, find and edit one playlist, select a theme and return to a design without losing position. Owner: Library prototype slice. Suggested command: `$impeccable distill`.

### A5 — P2: Stage choice opens an archive before it offers a ready candidate

**Task:** Pick the latest ready video, or deliberately use an older one.

**Source fact:** `show-setup.js:66–86` initially creates collapsed groups per design; each expansion contains every retained render. Latest retained appears within an expanded row. The main card exposes technical readiness/duration/date, but not the selected music or complete playback consequence. Existing date/change labels and previews are valuable.

**Predicted friction:** The user must first inspect group names/counts, expand, then scan historical outputs before making the common latest-ready choice. They may have to preview or recall music to distinguish candidates.

**Correction:** Default to latest ready video per design, with selected older result retained visibly; reveal Older videos separately. Show music and “Loops until stopped” / “Stops broadcast when finished” where choosing.

**Retest:** Select latest and older outputs in a 10+ history fixture by visible identity; verify the selected marker never moves on generation. Owner: chooser/history prototype slice. Suggested command: `$impeccable clarify`.

## Cognitive load

Source-supported checklist: single focus **fails**; chunking **fails**; grouping **passes in structure** (panels, headings and controls are grouped); visual hierarchy **fails in BRB; passes in Show setup**; one decision at a time **fails**; minimal choices **fails**; working-memory support **fails**; progressive disclosure **fails**. Seven checklist weaknesses apply to the BRB/editor composition, supported by screenshot evidence for hierarchy and navigation density and by source for workflow dependencies. Show setup is markedly better. This is a heuristic diagnosis, not a measured human capacity threshold or user study.

More than four visible choices are encoded at several decision points: six Studio collections (plus Return to open design when applicable), five theme sample views, seven BRB music properties plus preparation, and multiple fully expanded authoring forms. The strongest memory bridge is BRB: remember whether the current visual was activated before preparing a new soundtrack. Another is switching music/library/Design properties/history to complete a single edit-to-use task.

Intrinsic load is unavoidable: a broadcaster must distinguish preparation, selection and actual playback. Extraneous load comes from forcing operators to learn the separate implementations that currently realize those concepts. The approved exact-video contract should be taught with visible identities, not repeated revision explanations.

## Emotional journey (design interpretation)

The rendered Show setup surface offers a reassuring overview of what is selected. Confidence is likely to drop when its BRB Edit design action leads to a different fallback identity, a broken still image and a much denser screen. Live confidence remains a source-derived prediction from its monitors, status, Stop and rehearsal. Technical assurances provide safety but also make ordinary changes feel consequential. Exact preview and explicit use provide a good confidence peak; the end of the flow should return to setup with an unmistakable next-use selection and persistent current-playing identity. No actual hesitation, time-on-task or completion rate was measured.

## Persona red flags

- **Alex, returning power operator:** The common Generate action is hidden under Render history; library browsing carries permanent authoring forms; collection navigation replaces browser history. Existing adjustable panes and direct setup links are positives.
- **Jordan, occasional first-time setup operator:** “Prepare preview”, “Activate prepared BRB”, “Prepare music presentation”, “Review / assign BRB” and “Use current fallback BRB” require learning relationships before trusting a simple music change. Prestream versus Starting soon also requires translation.
- **Sam, keyboard/low-vision operator:** Source includes focus-visible styles, native controls and live status, so do not assert absent accessibility. Dense 10–12 px supporting text, multiple scroll containers and repeated nav groups create risks at zoom; actual keyboard order, dialog focus restoration and screen-reader announcements need browser/device testing.

## Minor observations and unverified risks

- Rehearsal is a checkbox next to start/stage operations, requiring the operator to interpret the same stage command in a mode. Separate stopped-state Rehearse / Start broadcast actions would express intent more directly. Confirmation behavior was not exercised.
- Repeated explanatory prose contains codec granularity, profile limits, retained revisions and independent music/visual loops. Preserve the consequences in primary views and move technical mechanics into details.
- Phone CSS provides labelled Operate / Signals & destinations panels, 44px live controls and desktop-only explanations. It also reduces some text to 10–11px and stacks show cards. These are source facts, not proof of fit, legibility or usability on a real phone.
- `show-dialog` headings are present but the markup shown lacks an explicit `aria-labelledby` on chooser/preview dialogs. A semantic audit should verify their accessible names; this is not a tested screen-reader failure.
- P3 polish: Music playlist metadata uses larger body text than the track metadata above, giving card metadata disproportionate weight. Fix after task structure; retain the compact track-row treatment. Screenshot: `/tmp/ux-baseline-music.png`.

## Questions for parent synthesis

The approved plan already answers product direction. Use these as prototype stress tests, not a reason to reopen settled decisions:

1. Can an operator distinguish what they edited, what they generated, what is selected for next use and what is playing without reading a paragraph?
2. Can the first visible candidate answer “Is this the correct soundtrack and ending behavior?” before playback?
3. Can the four-destination shell remain legible and operable when status is stale or generation fails, without pushing Stop out of reach?

## Remaining browser follow-up

The parent completed the supplied three-view desktop walkthrough. Remaining coverage: Live stopped/zero-destination state; Show setup; BRB editor; Starting soon and Ending editors; Music tracks/playlists; Media; Themes; Settings. Capture actual screenshots at desktop and phone dimensions when available. Exercise navigation and harmless previews only; no setup changes or broadcasts. Record actual routes, number of avoidable view changes, internal/whole-page scrolling, and labels that produce wrong expectations. Actual iPhone playback remains a separate acceptance gate.
