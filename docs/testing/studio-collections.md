# Studio Templates and Themes verification

Approved seams: authenticated real browser, public authenticated HTTP/status and existing local RTMP regression coverage. Disposable fixtures and dummy credentials only.

## First scenario (written before implementation)

Open Templates without an editor open. Browse built-in starters and custom templates with their stage, version and scene count. Choose a template to configure an independent design with a separate exact theme revision. Open the design and navigate to Templates to explicitly save/update its content, while the focused scene inspector contains only a link to this collection workflow. Autosave must never update templates.

Baseline: the Templates collection renders only custom template delete buttons. Built-in starters and the use-template action are absent; save/update administration remains inside the scene editor's Template inspector. This fails the collection workflow above.

## Subsequent scenarios

- Browse exact immutable theme revisions; duplicate a built-in and publish a custom revision. Adopt the selected saved revision explicitly into the open design, retaining scene overrides. Publishing alone preserves its pinned revision and prepared output.
- Show loading and errors at the relevant collection, retain user edits after failure, and permit an explicit refresh. Built-ins cannot be deleted; referenced themes and uploads retain existing server protections.
- Desktop/narrow layouts keep collection browsing and administration readable. Phone retains collection summaries while creation, edits, adoption and deletion remain desktop-only. Keyboard focus follows collection shortcuts and template use.

## Green evidence (2026-10-12)

The real authenticated app at disposable localhost:18848 reproduced the baseline before implementation: Templates showed one custom delete row and no built-in browsing/use or save/update workflow.

After implementation:

- Used a saved template from its collection card, which focused the name field in Designs and retained the selected content and separate exact theme. Created `Collection independent copy` with its own identity and content.
- The editor Template shortcut focused Template name in the collection. Explicit save produced `Collection saved content` v1; explicit update produced v2. The source design and previously saved template remained independently available.
- Duplicated Retro revision 2 to `Collection colors` revision 1, adopted it, changed the default font and published revision 2. The design stayed pinned to revision 1 until explicit adoption; a subsequent page reload showed its saved revision 2. The first browser pass exposed adoption remaining disabled after publishing; the corrected pass verified it re-enabled and succeeded.
- Stopped the disposable server and refreshed Templates. The collection reported the connection error and retained its existing choices. Restarting and refreshing restored its counts. No native confirmation was needed for these workflows; existing authenticated deletion tests cover protection and physical ownership effects.
- Desktop 1440×900, narrow 820×900, and phone 390×844 screenshots were inspected. Narrow and phone document widths matched their viewports. Phone exposed read-only template summaries and theme revision browsing, with zero rendered Templates authoring controls. Keyboard focus moved to the design name and template name at their collection shortcuts. Built-in template cards expose no deletion; built-in themes disable publish/delete.

Screenshots: [Templates desktop](screenshots/templates-desktop.jpg), [Themes desktop](screenshots/themes-desktop.jpg), [Themes narrow](screenshots/themes-narrow.jpg), [Templates phone](screenshots/templates-phone.jpg).

Focused race coverage passed: `TestContentTemplateCopiesPersistIndependentlyOfDraftsAndThemes`, `TestContentTemplateStartersAreEditableAndUpdatesRequireExplicitVersion`, `TestTemplateUpdatesCannotChangeCapturedGenerationInputs`, `TestGeneratorThemeRevisionsAreImmutableAndAdoptionIsExplicit`, `TestGeneratorThemeAdoptionRevalidatesTypography`, and `TestDeleteCustomTemplatesAndThemesPreservesReferencedRevisionsAndUploads`. Build, `go vet ./...`, JavaScript syntax, whitespace checks and all 18 web tests passed. Root owns the final combined race/vet and exact-SHA release gate.
