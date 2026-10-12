# Production studio redesign

Status: approved. All interview rounds are answered, and the user confirmed the consolidated scope and shared understanding. The design interview is closed; application implementation has not started as part of this interview.

Published specification: [Production studio and mobile broadcast remote — #37](https://github.com/kreuzhofer/restreamer/issues/37). The user confirmed the authenticated browser, HTTP/status, local RTMP, and exact-commit CI testing approach before publication.

The user approved the 14-ticket breakdown, published as sub-issues #38–#51 with native blocking relationships. Start with [workspace navigation and persistent status — #38](https://github.com/kreuzhofer/restreamer/issues/38). Ingestion Settings (#49) also depends on existing #33; destination Settings (#50) also depends on existing #35. Final migration and release verification is #51.

## Agreed direction

- Separate Live (broadcast operation), Studio (designs, themes, templates, and media), and Settings (connections and technical setup).
- Keep a compact broadcast status visible across workspaces. Destination health and delivery controls belong in Live; destination configuration belongs in Settings.
- Desktop is a full-window production studio. Do not reserve space for OBS beside it. Responsive behavior should resemble a desktop production application: the preview can grow and shrink while controls retain useful minimum sizes. Panels have draggable dividers and remembered sizes, with consistent positions; freely rearrangeable or detachable docks are outside the initial scope. Save panel sizes per browser and provide Reset layout. When space is insufficient, secondary panels become drawers or tabs while preview and primary controls remain accessible; do not simply shrink all controls or stack the whole desktop interface. Exact breakpoints will follow content fit during implementation and browser verification.
- Mobile primarily supports live operation: outgoing preview, destination health, stage changes, prepared clips, and stopping the broadcast. It can browse, preview, and select prepared media. Text editing, uploads, regeneration, full scene composition, and theme editing require desktop in the initial version.
- Use a polished dark production-console aesthetic with strong hierarchy and compact controls. Arcade and Neon styling belongs to media previews rather than application chrome.
- The user approved the initial concept's visual direction after viewing the Live and Studio screenshots.
- Live gives the outgoing broadcast the prominent monitor and shows OBS input in a smaller monitor alongside it.
- Studio remains available during broadcasts, with persistent broadcast status. Editing and generation preserve the revision currently on air; newly generated media requires explicit review and activation, preserving ADR 0003.
- BRB creation and styling move into Studio alongside prestream and ending preparation. Live retains readiness and activation controls. This does not introduce a whole-show bundle or redefine show design.
- Live shows pinned clip shortcuts. The full library and detailed playback controls, including pause, seek, and loop, are available on demand.
- Studio opens with distinct Designs, Media, Themes, and Templates sections. Selecting a design opens the focused scene-list, preview, and properties editor shown in the concept. Library management does not occupy the editing workspace.
- Each design surfaces its latest generated result and readiness. Older generated revisions belong in that design's history, with a compact shared generation queue. Draft, prepared output, and selected broadcast media remain visibly distinct.
- Input loss or destination failures create persistent alerts across workspaces, explain what happened, and provide direct access to Live. They do not automatically navigate away from editing. Detailed counters and graphs open on demand.
- Ingestion keys, destination configuration, and streaming-profile changes require desktop initially. Mobile retains destination health and operational delivery switches.
- While OFF, Live shows a compact readiness check for input, prepared stage media, and destinations, with choices to start broadcasting or enter a preview-only session. Rehearsal belongs in Live rather than Settings, and must unmistakably indicate that destination delivery is blocked.

## Existing behavior to preserve

- Selected broadcast stage, actual on-air source, and destination delivery are separate facts. Connected destinations do not prove public platform playback.
- Pending transitions, deliberate versus fallback BRB, original clip return-stage behavior, and ending completion rules retain their existing semantics.
- Exact ready revisions require review and explicit activation; background preparation cannot silently replace on-air media. A preview-only session must end before real broadcasting begins.
- Design deletion removes its generated results and eligible generated files. Uploaded assets remain separately managed and are never implicitly deleted with a design; existing reference and playback protections remain.
- Existing authentication, credential protections, and settings restrictions remain in force.

## Review and implementation boundaries

The visual direction and product boundaries are settled. Implementation details such as exact breakpoints, divider limits, and drawer mechanics should be verified against real content rather than treated as fixed dimensions from the mockup. The concept does not specify every validation, failure, loading, or empty state and is not a replacement for the existing behavior contracts.

Specification preparation verified that local and GitHub main both point to `abd9cfeff54534738138849491c69c2fe7acbd84`; ingestion-key management (#33) and destination management (#35) remain open and absent from that baseline. Their completed behavior is a dependency of the new Settings workspace. The approved redesign moves ingestion management into Settings and makes both management flows desktop-only initially, superseding their earlier UI placement/mobile expectations while preserving their backend, security, and migration requirements.

The initial clickable concept is illustrative, uses simulated state, and does not control a running broadcast. The approved brief governs implementation; incidental mockup details do not override it or existing behavior contracts.
