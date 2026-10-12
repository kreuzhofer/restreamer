# Focused Studio editor verification

Approved seam: authenticated application browser, backed by real HTTP state and disposable media. Use documented dummy credentials and local sinks only. Existing generator HTTP and deletion tests cover persistence, conflicts, exact revisions, and physical cleanup.

## Scenario written before implementation

1. Open `/studio`: Designs, Media, Themes, and Templates are separate, working collection destinations. An empty collection explains how to create a design.
2. Create a prestream design. Opening it replaces the collection with a scene list, central quick preview, and properties. Enter a title, wait for Saved, add a list scene, reorder it, then reopen the design and verify persisted content and ordering.
3. Switch Properties between Scene and Design. Verify exact theme revision, soundtrack, loop controls and ending fade remain reachable. Save as a template from the Template properties view.
4. Enter invalid scene content; verify validation and local edits remain visible across collection navigation. Resolve validation, then verify autosave. Use two tabs to verify stale saves retain local content and offer reload/copy recovery.
5. Open Results & queue, generate a saved draft, and preview the exact result. Collection and workspace navigation must not change broadcast stage or selected media.
6. At 1920×1080 and 1440×900, verify the three editor panels fit without document scrolling. At 1024×768, use visible Preview/Properties controls with keyboard focus; property details scroll within the panel. At 390×844, verify desktop-required explanation, design browsing and prepared-result review, with authoring/upload/regeneration controls unavailable.
7. Reuse authenticated HTTP regression coverage for deleting a design: generated files removed, uploaded assets retained, selected/referenced output protected.

## Red observation

2026-10-12, baseline `3cc6778`, authenticated browser on disposable `localhost:18839/studio`: no collection navigation; creation, themes, templates and uploads share a long sidebar. The focused editor workflow and mobile boundary are absent.

## Green observations

2026-10-12, disposable fixture `localhost:18842` with documented test credentials:

- Created an independent prestream design; authored title and list content, reordered scenes, and observed saved versions. Reload restored the content and scene order.
- Switched to Media and Templates and resumed the open design without losing content. Zero duration remained visible with both scene and field validation after navigation; correcting it restored preview.
- Verified Design properties retain Retro revision 1 explicitly while offering an update, soundtrack selection and complete-cycle controls. Scene properties retain all four layout choices, duration, transitions and overrides.
- Two authenticated tabs produced a real server version conflict. The first tab retained its local title and offered recovery; Save local work as a copy produced a separate saved design. Reload restored the copied title.
- Saved a reusable content template from Template properties and saw its new version in the update selector.
- Generated a two-second saved design, observed running then ready status, and played its exact media revision. Persistent broadcast status remained OFF; prepared output remained unselected.
- DOM geometry confirmed document dimensions exactly match 1920×1080, 1440×900, 1024×768 and 390×844. At 1024, Preview/Properties buttons kept content reachable. At phone size, design browsing and exact-preview/selection links remain visible, while creation, scene fields, uploads and generation actions are absent with a desktop-required explanation.
- Browser screenshots are in `screenshots/studio-editor-{wide,desktop,narrow,phone}.jpg`. The narrow screenshot includes an intentionally invalid duration. These use real disposable designs and generated media.

The existing authenticated generator, conflict, theme revision, deletion/physical cleanup, and local RTMP regression suites remain the automated behavioral coverage; this layout change adds no new server mutation contract. Final integration owns the exact-SHA CI and release gate.
