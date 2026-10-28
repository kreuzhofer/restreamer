# Studio UX prototype design system

Status: prototype direction accepted as “very clean”; production design awaits task walkthrough and final review. The authoritative interaction requirements are in docs/design/ux-simplification-plan.md.

## Operate surface

The application supports broadcast preparation and operation. Keep content artwork expressive and application controls restrained. Labelled permanent navigation, local underlined tabs and task commands use different visual treatments. Main tasks get one prominent action; maintenance is contextual.

## Prototype tokens

- Background `#17191c`; sidebar `#111316`; panel `#202328`; raised `#292d33`.
- Primary text `#f0f1f3`; supporting text `#b2b8c1`; borders `#383e46`.
- Action/selected accent `#a7ebcc` with dark text `#112b21`.
- Error `#ffaba5`; warning `#f1cc8a`; states always include text.
- System sans for application labels. Body 15px, headings about 28px, compact labels about 12–13px; media artwork carries its own type.
- 208px labelled sidebar; 34px desktop content gutters; 24px narrow and 18px phone gutters. Main panels use explicit min-width:0 and bounded grid tracks.
- Standard control radius 6px, media 7px, dialogs 12px. No decorative animation or custom scrollbar.
- Strong 3px visible focus ring; semantic links/buttons; local editor tabs support arrow navigation. Dialogs have accessible names.

## Responsive behavior

Full desktop uses scene area, preview and inspector. Narrow desktop moves inspector properties below; this prototype tests that compromise before production drawers are finalized. Phone has four labelled bottom destinations and read-only authoring information. Live's clip strip scrolls internally and must not widen the page. The prototype-scenarios control is review tooling, not product chrome.

## Evidence

User feedback and saved screenshots are in docs/design/ux-review. CSS in internal/relay/web/ux-prototype/style.css is the prototype token implementation. Production must implement the approved plan with proper tests; do not copy the prototype's state model directly.
