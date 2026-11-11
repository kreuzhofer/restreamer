# Intuitive production studio: UX simplification plan

Status: approved. All four interview rounds and the consolidated plan were confirmed by the user. Baseline review and an interactive prototype are authorized; production implementation follows prototype acceptance. This is a product and delivery plan, not a claim of implementation or completed design review.

## 1. Outcome and scope

An operator returning after a week should recognize where to prepare the show, understand what will play, edit its content and music, and operate the broadcast without recalling technical concepts or receiving verbal coaching. Navigation, local views and commands must be visually distinguishable before their labels are read.

The redesign covers Live, Show setup, the dedicated editor, designs, generated videos, tracks, playlists, uploads, themes, templates, Settings, responsive behavior and all associated loading, failure, conflict and cleanup states. Existing capabilities remain accessible. The approved additions needed for a coherent experience are unified BRB preparation, shared BRB/fallback selection with an optional override and independent emergency output, optional friendly video names, contextual resource creation, and explicit bulk cleanup.

Desktop remains a full-window production workspace, with no space reserved for OBS. Mobile focuses on operation and browsing, previewing and selecting prepared content. Editing, uploads, generation and technical configuration remain desktop-only. The application retains restrained dark studio styling; Arcade, Neon and other content themes belong in media previews.

No new whole-show bundle, full mobile authoring, automatic media retention, automatic adoption of new shared-resource versions, silent on-air replacement, freely detachable docks, frontend-framework migration or CI-infrastructure redesign is included. Keep the current embedded web architecture unless a separately justified requirement makes a change necessary.

## 2. Evidence and accepted decisions

The source audit found stacked global/collection/editor/panel navigation, the same button styling for navigation and commands, permanently visible creation forms, repeated technical explanations, duplicate assignment locations, and two separate BRB preparation paths. Existing human-readable video labels and music lists should be retained and improved. A fresh authenticated browser design review is still required; source inspection is not browser evidence.

All recommendations were accepted in four rounds:

| Round | Decisions |
| --- | --- |
| 1: Q1–Q3 | Four destinations; progressive disclosure; complete-task acceptance and design review. |
| 2: Q4–Q8 | Unified BRB preparation; shared fallback default with optional override; plain vocabulary; upcoming selection in Show setup; major-finding review gates and real iPhone verification. |
| 3: Q9–Q14 | Setup/edit/generate/use flow; latest-ready chooser with meaningful history; Library structure; contextual templates and explicit updates; responsive navigation; explicit bulk cleanup. |
| 4: Q15–Q20 | Independent emergency output; next-use versus active replacement consistency; recoverable errors/background generation; Live/Settings hierarchy; visual and usability standards; prototype-first, reviewed migration and release. |

Related decisions: ADR 0007 replaces only ADR 0006's three-workspace navigation choice; its desktop/mobile boundary remains. ADR 0008 governs unified BRB and fallback behavior. ADR 0003's exact-reviewed-media contract, ADR 0004's scene authoring and ADR 0005's independent shared themes remain applicable. Old design specs provide historical context, not a current feature inventory.

## 3. Information architecture

| Destination | Responsibility | Primary content and actions |
| --- | --- | --- |
| Live | Operate the broadcast | Outgoing monitor, smaller OBS input, stage/source/delivery status, stage controls, clips, destination health, Stop, rehearsal and readiness. |
| Show setup | Choose what plays next | Starting soon, BRB and Ending selections; previews; links to edit; change selection; optional fallback override. |
| Library | Manage reusable resources | Designs, Music, Uploads and Themes. Templates are managed from Designs and offered when creating a design. |
| Settings | Technical configuration | Ingestion, Destinations and Streaming profile, with purposeful add/edit forms. |

Use one persistent labelled desktop sidebar. The dedicated editor replaces the main work area and has a clear breadcrumb/back path; it must not add another collection-navigation row. Local tabs stay attached to their content. Do not repeat top-level navigation inside panels.

The main entry opens Live. When stopped, Live explains readiness and offers Set up show. Direct URLs open their requested context; refresh and browser Back preserve useful location and selection. Returning from an editor or picker restores the originating view, filters and relevant scroll position. Broadcast state changes never forcibly navigate the user away.

A compact persistent status identifies broadcast/rehearsal/stopped state and relevant failures. Detailed healthy telemetry and the generation queue open on demand. Alerts must not make the whole application jump or push primary controls out of reach.

## 4. Language and visual interaction system

| Concept | Visible language |
| --- | --- |
| Prestream stage | Starting soon |
| Deliberate break stage | BRB |
| Closing stage | Ending |
| Editable content | Design |
| Generated result | Video |
| Changed draft | Changes not generated |
| Valid prepared result | Ready |
| Saved stage assignment | Selected for next use |
| Active content | Playing now |
| Continuous playback | Loops until stopped |
| Finite ending | Stops broadcast when finished |

Keep canonical internal vocabulary and public API contracts intact. Hashes, revision IDs, preparation pipelines and codec details belong in Technical details, never as the main way to identify a video. Names, thumbnails, timestamps, music and plain change summaries identify results. Uploaded source videos must be visibly distinguished from generated results when both appear in a chooser.

| Interaction | Required presentation |
| --- | --- |
| Page navigation | Labelled sidebar item with unmistakable current-page treatment; semantic links. |
| Local view switch | Underlined text tabs with clear selection and appropriate semantics. |
| Main task command | One prominent filled action in its task area. |
| Secondary command | Quieter text or outline control. |
| Infrequent maintenance | Accessible, labelled overflow menu. |
| Optional properties | Named disclosure with a useful collapsed summary. |
| Status | Non-interactive text/badge with an icon or wording; never color alone. |
| Destructive action | Clear wording, visually separated from routine actions and a consequence-specific confirmation. |

Use a consistent type scale, spacing, control sizes and focus treatment. Readability outranks compression. No hover-only essential actions, icon-only primary navigation, low-contrast helper text or motion-dependent feedback. Exact typography, spacing and breakpoints are prototype decisions validated against content; do not encode incidental mockup dimensions as product requirements.

## 5. Show setup and video selection

Show setup presents three compact stage rows. Each shows the selected video's thumbnail, design name, music and playback behavior/readiness. Clicking the thumbnail previews; clicking the design name opens the editor. Change opens the video chooser. Creation is offered contextually when no design/result exists. History and maintenance do not compete with these actions.

If a stage is currently playing a different video from its saved selection, show Playing now and Selected for next use distinctly. Updating upcoming content never silently changes current playback. The optional automatic-source-loss override belongs with BRB and explains its scope.

The chooser initially shows the latest ready video per design, with thumbnail, name, creation time, music and duration/playback behavior. Keep the currently selected result visible even if older. Older videos reveals the design's history, optional friendly names and meaningful differences such as Changed music. Generation does not move the selected marker. Preview and Use this video are separate actions; Use captures the exact reviewed result. Missing/incompatible/protected states explain what to do next rather than presenting a mysterious disabled button.

Imported playable videos remain selectable where supported. Their source and readiness are clear. A design edit must not be suggested for an uploaded video with no editable design.

## 6. Dedicated editor and preparation flow

The shared editor uses a content/scene area, a large preview, and relevant properties. Starting soon and Ending retain ordered scene sequences; BRB retains animation-focused content controls. Music selection and result preparation occupy the same conceptual locations for all three. Library management does not occupy the editor workspace.

1. Open the design from setup or Library, preserving the return context.
2. Edit content, theme, music and relevant playback choices. Autosave shows saving/saved/error state. Preview clearly distinguishes a draft approximation from a prepared video when applicable.
3. Generate video captures the intended draft and exact selected resources. For BRB, this prepares appearance and soundtrack together; it must not capture a previously activated appearance or require prior activation.
4. Generation continues after leaving the page. A compact indicator opens progress, cancellation where supported, and failure details.
5. Review the exact ready video, its soundtrack and playback behavior. If the draft changed during generation, identify the result as based on earlier changes; do not imply that it contains newer edits.
6. Use for Starting soon / BRB / Ending explicitly updates next use, then returns to Show setup. Retain download access and history without making them prerequisites for selection.

Generation failure leaves the prior ready/selected video usable. No automatic assignment or live replacement follows success. Existing source audio controls, gain, fades, scene timing and transitions remain discoverable in relevant properties; advanced controls use meaningful collapsed summaries.

Music supports none, one uploaded track or a saved playlist. Existing Fixed and Follow music behavior remains, with the resulting playback consequence in plain language. A Follow music download contains one music pass; on-air visual/music looping continues independently. Fixed Ending can stop broadcasting on completion; Follow music Ending keeps playing until explicitly stopped or left. This distinction must be visible during review and selection.

## 7. BRB, automatic fallback and emergency behavior

The selected BRB is used by default for deliberate breaks and all existing automatic fallback reasons: input loss/stall, failed prepared playback, paused clips and applicable return transitions. The override label is Use a different video when a source is unavailable, with a short explanation. Deliberate BRB stays selected until the operator leaves it; automatic fallback retains its existing recovery behavior when its intended source returns.

A separate, independently prepared silent Be right back emergency visual protects against failure of the selected BRB or override itself. It must not depend on the same failing music process or files and is not another routine setup choice. Validate emergency readiness for the active streaming profile. Show a persistent degraded-state error and explicit recovery action; do not endlessly restart the failed presentation. If emergency output is unavailable too, surface output failure honestly and keep Stop and recovery controls usable. Do not claim uninterrupted delivery without evidence.

Selecting a BRB or override changes future entries only. Already playing or suspended media retain exact captured results. Replacing current automatic fallback or a suspended return requires separate review and confirmation with current/replacement identities and consequences. Retire the legacy immediate-activation exception from the ordinary authoring flow.

The fallback override and emergency dependencies must participate in readiness, reference/use displays, deletion protection and recovery. This is a runtime and persistence change, not merely UI relocation.

## 8. Library and reusable resources

| View | Browsing and creation | Important details |
| --- | --- | --- |
| Designs | Search/filter list; New opens Blank / From template / Duplicate existing | Open editor; latest ready video; history; rename/duplicate/delete; Save as template and template management. |
| Music | Consistent Tracks and Playlists lists; upload/create/edit on demand | Listen, duration, order, uses; add/reorder/remove playlist tracks with accessible controls; explicit shared-version adoption. |
| Uploads | Images and videos; search/type filters; contextual upload | Use in design or Prepare for playback where supported; clear readiness, source identity and usage. Music has its primary home in Music. |
| Themes | Preview/browse, then edit or duplicate intentionally | Scene samples, supported customization, theme updates, and transparent OBS border preview/download. |

Resource pickers allow adding a missing resource and returning to the original task without losing edits. Templates remain independent starting points; updating a template does not modify its existing copies. Themes remain separate from content. Shared-theme and playlist changes show Update available; adopting them is deliberate and existing videos never change.

Browsing, editing and publishing shared resources must be visibly distinct. Keep all currently supported theme/template operations accessible even after removing permanent forms and top-level Templates navigation.

Designs and generated-video history support selection-based bulk cleanup. Confirmation names the selected items and generated-file scope. Protected items show why and where they are used. Show per-item outcomes on partial failure, retain failed selections and offer retry. No automatic deletion policy is added. Deleting a design removes eligible generated files but never uploads; tracks/uploads have their own deletion and reference checks. No force-delete bypass is introduced.

## 9. Live and Settings

Live prioritizes the outgoing monitor, stage intent, actual source, delivery status, stage controls and Stop. OBS input is smaller. Clip shortcuts remain available, with the full chooser and detailed playback controls on demand. Upcoming stage assignments have one home in Show setup, reachable directly from Live. Replace current video remains an explicit operational flow.

When stopped, show readiness and separate Rehearse / Start broadcast choices. Preserve: OFF on restart; rehearsal never opens destination delivery; stopping rehearsal before real broadcasting; Starting soon without OBS where valid; pending fresh-keyframe Go live with cancellation/expiry; original clip return-stage behavior; suspended playback; finite Ending drain versus music-driven loops; independent destination failures. Preview pause, browsing and generation must not affect broadcast playback.

Settings has Ingestion, Destinations and Streaming profile views. Add/edit forms open on demand. Keep Twitch/YouTube/Kick presets and name prefilling. Preserve explicit secret reveal/copy/regenerate, missing-key disabling, initially disabled new destinations and current-context confirmation for disruptive changes. Explain actual effects: ingestion rotation affects future connections while an existing publisher remains connected; removing the last delivery destination can leave the local show running; profile changes require OFF and can require preparation again. Preserve existing stage-specific restrictions rather than hiding them behind inert controls.

## 10. Responsive behavior, accessibility and failures

Desktop stage setup and primary live controls fit without whole-page scrolling at the agreed normal desktop test sizes. Long lists and editor properties scroll within their own areas. Keep primary actions and status reachable. Secondary panels become drawers on narrow desktop/tablet; preserve useful minimum control widths and remembered/resettable layout behavior. Do not simply shrink everything or stack every panel into one long page.

Phones use labelled Live / Show setup / Library / Settings navigation, operational controls and useful prepared-content/read-only views. Unsupported editing or technical configuration is explained in context. Physical iPhone preview/playback is a separate check from a resized desktop viewport.

Keyboard navigation, focus visibility and return after dialogs, semantic controls, screen-reader labels/status announcements, sufficient contrast, touch targets, reduced motion and zoom/reflow are acceptance concerns. Do not impose a no-scroll rule at the expense of zoom accessibility. Validate representative full desktop, narrow desktop/tablet and phone sizes; initial fixtures can use 1440×900, 1280×800, 1024×768 and 390×844, adjusted based on real content fit.

| State | Required behavior |
| --- | --- |
| Empty | Explain the missing resource and provide its contextual first action. |
| Loading | Preserve layout/context; identify pending data and avoid presenting unknown readiness as ready. |
| Stale/disconnected | Mark information as stale; do not claim command success; gate actions requiring fresh context. |
| Validation failure | Identify the field, correction and retained input. |
| Autosave/network failure | Preserve edits, show explicit failure/retry and warn before leaving with unsaved work. |
| Concurrent edit/conflict | Offer reload or save a copy; do not overwrite another edit silently. |
| Queued/generating | Show job status without blocking navigation; retain the prior ready selection. |
| Failed/interrupted generation | Explain failure and offer a deliberate retry; no selection or false success. |
| Pending stage transition | Show intended and actual state distinctly with cancellation/expiry where supported. |
| Playback/fallback failure | Explain degraded source/output and recovery; keep Stop responsive. |
| Partial deletion | Show per-item outcomes and remaining protected/failed items. |
| Shared resource update | Show update availability and explicit adoption; preserve pinned resources. |

Important broadcast failures persist until resolved or acknowledged; acknowledgement must not conceal an ongoing unhealthy state. Routine success feedback is brief. Technical error details are available without leaking secrets.

## 11. Design-skill review and prototype gates

Use **Impeccable** Operate guidance and critique for interaction hierarchy, discoverability, cognitive load, consistency and complete-task evaluation. Supplement it with **Vercel web-design-guidelines** for semantic navigation/actions, focus, forms, keyboard/touch, responsiveness and accessibility.

The actual skill instructions were inspected during planning. Neither skill has yet been installed or run for this redesign. Inspect and pin a project-scoped installation, record source/version, and avoid adding automatic hooks as an incidental dependency. Impeccable currently uses a native launcher/detector and isolated design/evidence assessments. Respect the available browser-tool restrictions; report unavailable detectors or overlays honestly. Reuse these project documents for context rather than inventing competing product requirements.

Sources:
- [Impeccable skill](https://github.com/pbakaus/impeccable/blob/main/.agents/skills/impeccable/SKILL.md)
- [Operate guidance](https://github.com/pbakaus/impeccable/blob/main/.agents/skills/impeccable/reference/operate.md)
- [Critique procedure](https://github.com/pbakaus/impeccable/blob/main/.agents/skills/impeccable/reference/critique.md)
- [Vercel web-design-guidelines](https://github.com/vercel-labs/agent-skills/blob/main/skills/web-design-guidelines/SKILL.md)

| Gate | Evidence and exit condition |
| --- | --- |
| Baseline | Fresh authenticated browser walkthrough of current app with representative data. Record actual task friction, navigation/scroll burden, confusing consequences and screenshots. Separate observation from source-derived inference. |
| Interactive prototype | Linked complete workflows covering all four destinations, unified editor, selection/history, fallback and recovery. Use realistic content and simulated state, clearly identified as a prototype. Review with both relevant skill methods. |
| User prototype walkthrough | User performs core tasks without coaching; record detours, confusion and mistaken expectations. Resolve task-blocking/major findings before implementation proceeds. |
| Implemented app | Repeat the same tasks, skill critique and accessibility checks in the authenticated real application; verify behavior rather than matching screenshots alone. |
| Release acceptance | All task-blocking and major findings fixed and retested. Smaller exceptions explicitly listed for user acceptance. Include physical iPhone playback evidence or state the unresolved blocker; do not substitute responsive screenshots. |

Each finding records task, observation, severity, screenshot/source, correction, owner/work item and retest evidence. Review scores support judgment; they do not replace the user's walkthrough. A future review that reveals a consequential product conflict returns to an explicit decision rather than silently changing this plan.

The prototype must cover at least setup → edit BRB → choose/create playlist → generate → preview → use; older-video selection; source loss/emergency output; and protected/partial deletion. Include empty, loading, stale and failed states alongside healthy screens. The approved interaction rules govern it; an attractive happy-path dashboard alone does not pass.

## 12. Acceptance matrix

| Scenario | Pass condition |
| --- | --- |
| Return after time away | Identify and preview each stage's selected video without coaching; distinguish navigation from actions. |
| Edit each stage | Find content/theme/music in consistent places; generate and explicitly use the intended video. |
| Change music only | Choose a track/playlist and generate a new result; retain existing visual reuse and old ready result. |
| Draft changes during generation | Correctly identify which changes the completed video contains; no automatic assignment. |
| Select older video | Find the intended result by preview/name/time/change summary without hashes. |
| Setup during broadcast | Change next use while current and suspended playback remain unchanged. |
| Replace current/return | Review exact old/new result and consequence; stale confirmation fails clearly. |
| BRB preparation | New message/theme/music captured together without first activating appearance. |
| Source loss and recovery | Correct shared/override BRB, accurate source status and existing deliberate/automatic recovery distinction. |
| BRB failure | Independent silent emergency output or explicit output failure; no retry loop; Stop remains responsive. |
| Rehearsal and live | Rehearsal blocks delivery; real start is deliberate; pending/failed transitions stay intelligible. |
| Ending behavior | User can distinguish finite-stop from music-looping Ending before activation. |
| Library at scale | Search/filter and reuse 20 designs, 100 tracks/uploads, 10 playlists and histories with 10+ videos; no card sprawl or lost selection. Use fixtures within current limits. |
| Shared resources | Template copies remain independent; theme/playlist updates require explicit adoption. |
| Cleanup | Generated files removed as requested; uploads retained; protected items explained; partial failures recoverable. |
| Settings | Destination presets/save, key copy/reveal/regeneration and profile consequences are understandable and retain protections. |
| Failure/unsaved states | Input survives recoverable failures; field errors and retries are actionable; no silent success. |
| Keyboard/zoom/touch | Primary workflows work with keyboard, visible focus, zoom/reflow and touch without hidden essential controls. |
| Real phone | Live monitor actually plays; stage operation and prepared-content preview/selection work; desktop-only boundaries are clear. |
| Migration/restart | Existing content/selections/fallbacks preserved; no unintended generation or deletion; restart remains OFF. |

Use observed task completion, wrong turns, misleading actions, and unnecessary navigation/scrolling as comparison evidence. Reuse identical scenarios in baseline, prototype and implemented reviews. Do not invent numerical improvement claims without measurement.

## 13. Migration and rollback

The existing deliberate-BRB selection and fallback appearance are stored separately, and old state readers reject unsupported schemas. Old fallback generations can be cleaned after activation. Compatibility therefore requires deliberate state and file-lifetime handling, not assumptions that old binaries can read new files.

Before migration, stop the broadcast and back up relevant state and referenced files. Preserve designs, generated videos, playlists and versions, uploads, stage selections, custom fallback images/music and existing distinct fallback choices. Installations with no explicit BRB selection retain their prior effective appearance. Never regenerate, rotate credentials, select newer videos or delete old files merely because the UI changed.

Represent existing distinct fallback as an explicit override. Validate referenced files/profile/digests and protect imported fallback assets before cleanup can remove them. Failed migration must be explicit and leave the original state recoverable. Re-running migration must not duplicate resources or change selections. Document the state mapping and verify rollback using a copy of the old binary/state plus restored files; do not promise in-place downgrade compatibility across a schema change.

Old supported URLs/bookmarks should resolve to their equivalent task where feasible. Preserve public APIs and schemas unless a necessary change is documented with compatibility handling. Add the new fallback/override references to deletion protection, use listings and recovery before exposing them to users.

## 14. Implementation sequence and dependencies

| Slice | Deliverable | Prerequisites / completion evidence |
| --- | --- | --- |
| 0. Baseline and review setup | Scoped skills, current-app audit, representative fixtures, capability/state inventory | Consolidated plan confirmation; recorded baseline and review limitations. |
| 1. Interactive prototype and design system | Four destinations, full task flows, responsive rules, component states, typography/spacing | Baseline; skill reviews and user walkthrough accepted; major findings resolved. |
| 2. Shared shell and navigation | Routes/return paths, sidebar/tabs/actions, compact global status, responsive/accessibility primitives | Approved prototype; existing links/state protections remain usable. |
| 3. Setup, chooser and history | Stage rows, explicit next-use selection, meaningful video/history choices | Shell; exact-review contracts preserved and browser-tested. |
| 4. Library and contextual resource flows | Lists/search, music/playlists, uploads, themes/templates, contextual creation, safe bulk cleanup | Shell and reviewed chooser patterns; file/reference regression tests. |
| 5. Unified editor and BRB runtime | Shared authoring flow; BRB draft+music preparation; shared/override/emergency fallback; exact active protection | Prototype; music/theme selection flows; migration and reference protection designed and tested before exposure. |
| 6. Live and Settings integration | Focused operation, removal of duplicate setup, deliberate replacement, configuration forms | New setup/editor and BRB behavior; local RTMP safety checks and responsive verification. |
| 7. Migration, final review and release | Backups/restore procedure, end-to-end acceptance, minor-finding disposition, release | All slices coherent; real-device evidence; successful final-SHA CI and main image publication. |

Slices are implementation work units on the delivery branch, not permission to expose a half-migrated experience in production. Build complete user-testable paths within each slice, and reconcile any prototype-to-code changes through the same review criteria. Commit small reversible changes. Preserve current operation while development proceeds.

## 15. Verification and delivery evidence

Write meaningful regression tests first for changed behavior, particularly unified BRB capture, fallback failure isolation, immutable selections, current/suspended replacement, migration/restart and deletion/reference protection. UI tests should verify user-visible behavior and semantics rather than mirroring markup. Use authenticated browser verification for real layouts and interactions.

Use local RTMP sinks and dummy credentials for broadcast tests; never send test broadcasts to real platforms. Reuse existing HTTP/status, playback and timed-tick tests where appropriate. Run normal race tests and go vet (`make check`), plus scoped checks justified by the change. A physical iPhone test is required for actual playback; viewport emulation alone is insufficient.

Before declaring implementation delivered: push the final delivery commit, verify successful GitHub CI for that exact SHA, and verify the actual merged main SHA and image publication for a release. Follow docs/agents/delivery.md. Record the SHA, run URL, test results, design-review reports, browser/device evidence, accepted minor findings, migration/rollback instructions and release image status. Do not close implementation tickets before their acceptance and release gates pass.

## 16. Planning close-out and next step

All product branches raised in the four rounds have answers. Remaining work is execution evidence: baseline review, installation/version verification of review tools, prototype validation, implementation details and tests. Exact component dimensions and code organization are determined within the accepted constraints rather than requiring another product interview.

The next action after confirmation of shared understanding is baseline review and the complete interactive prototype. Production implementation follows the prototype review gate. This planning document does not claim that reviews, prototype, migration or implementation have already been performed.
