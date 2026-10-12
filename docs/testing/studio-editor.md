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
