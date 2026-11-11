---
name: "Restreamer studio"
description: "Restrained dark controls around expressive broadcast media."
colors:
  primary: "#a7ebcc"
  on-primary: "#112b21"
  canvas: "#17191c"
  sidebar: "#111316"
  panel: "#202328"
  raised: "#292d33"
  selected: "#293730"
  text: "#f0f1f3"
  muted: "#b2b8c1"
  border: "#383e46"
  error: "#ffaba5"
  error-surface: "#352626"
  danger-border: "#80544e"
  warning-surface: "#302b22"
  warning-border: "#66543b"
  media-surface: "#101214"
  badge-text: "#a3b0c6"
  badge-surface: "#1b2434"
  badge-border: "#374357"
typography:
  headline:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif"
    fontSize: "28px"
    fontWeight: 650
    lineHeight: 1.2
    letterSpacing: "-1px"
  title:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: 1.5
    letterSpacing: "-0.3px"
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif"
    fontSize: "13px"
    lineHeight: 1.5
  command:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif"
    fontSize: "14px"
    lineHeight: 1.5
  status:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif"
    fontSize: "12px"
    lineHeight: 1.5
rounded:
  badge: "5px"
  control: "6px"
  media: "7px"
  dialog: "12px"
spacing:
  tight: "6px"
  compact: "8px"
  control: "12px"
  section: "16px"
  phone-gutter: "18px"
  group: "20px"
  workspace: "24px"
  desktop-gutter: "34px"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.command}"
    rounded: "{rounded.control}"
    padding: "8px 14px"
  button-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.text}"
    typography: "{typography.command}"
    rounded: "{rounded.control}"
    padding: "8px 14px"
  button-danger:
    backgroundColor: "transparent"
    textColor: "{colors.error}"
    typography: "{typography.command}"
    rounded: "{rounded.control}"
    padding: "8px 14px"
  field:
    backgroundColor: "{colors.sidebar}"
    textColor: "{colors.text}"
    rounded: "{rounded.control}"
    padding: "10px 12px"
  navigation-current:
    backgroundColor: "{colors.selected}"
    textColor: "{colors.primary}"
    rounded: "{rounded.control}"
    padding: "12px"
  local-tab:
    backgroundColor: "transparent"
    textColor: "{colors.muted}"
    typography: "{typography.command}"
    padding: "10px 0"
  status-badge:
    backgroundColor: "{colors.badge-surface}"
    textColor: "{colors.badge-text}"
    rounded: "{rounded.badge}"
    padding: "6px 10px"
  form-container:
    backgroundColor: "{colors.panel}"
    rounded: "{rounded.control}"
    padding: "24px"
  dialog:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.text}"
    rounded: "{rounded.dialog}"
    padding: "24px"
---

# Design System: Restreamer studio

## Overview

**Creative North Star: "Restrained production studio"**

The accepted prototype keeps application controls restrained and lets broadcast media carry expressive themes. The production implementation preserves that direction with charcoal surfaces, mint actions, labelled navigation and compact rows. This is an operational workspace, with content previews providing the visual emphasis.

This inventory describes the production source, not a new aesthetic direction or a claim of complete release acceptance. PRODUCT.md and the approved UX plan retain authority over playback, selection and desktop/mobile contracts.

**Key Characteristics:**
- Persistent labelled navigation; underlined local views; filled task commands.
- Flat sections and list dividers, with containment reserved for forms, media and dialogs.
- Readable status text and explicit next-use versus playing-now identities.

Source of truth: `internal/relay/web/studio-system.css`, loaded after the older sheets by both HTML entry points. Samples also include `dashboard.css`, `generator.css`, `workspace-layout.css`, `mobile-remote.css`, `studio-system.js` and semantic navigation in `index.html` / `generator.html`. Frontmatter records reused source values; it is not a complete computed-style audit. Existing qualified selectors can override the newer sheet.

Evidence scope: `docs/design/ux-review/implementation-review.md` passes three scored fixes only: BRB result identity/draft truth, shared properties location and useful disclosures. `implementation-evidence.md` records fixture/browser and physical-iPhone monitor playback evidence. Neither document establishes broad accessibility, runtime or release approval. This documentation pass did not rerun the browser or detector.

## Colors

Charcoal and cool neutrals frame a pale mint action accent; warnings and errors remain contextual.

### Primary

- **Mint** (`primary`): action fill, selected navigation, links, caret and baseline focus. Source aliases `--cyan` and `--green` resolve to the same value.
- **Ink on mint** (`on-primary`): text on filled actions and selected text.
- **Selected green charcoal** (`selected`): active navigation surface.

### Neutral

- **Canvas / sidebar / panel / raised**: page background, persistent navigation and fields, form/dialog containment, and secondary-command hover respectively.
- **Text / muted / border**: primary reading, supporting copy, and thin structural dividers.
- **Media surface**: dark backing for contained draft/result previews.
- **Error / error surface / danger border**: error notices and destructive-command outlines. Warning surface and border support the existing alert treatment; do not invent an additional warning accent.
- **Badge text / surface / border**: surviving diagnostic badge primitives, documented only for that component. Existing live/warning variants still have local colors in `dashboard.css`.

**The Navigation and Command Rule.** Page links, local view switches and task commands retain distinct visual treatments.

**The Media Owns the Theme Rule.** Expressive Arcade and Neon artwork belongs inside media previews; application controls retain the studio palette.

## Typography

**Body and application headings:** the system sans stack in the frontmatter. There is no independently loaded display face or geometric scale. This is the accepted operational treatment; the system stack is not a new expressive display-font rule.

The headline role is the default page heading; phone headings reduce to 25px. Default second-level headings use the title role, while editor titles use 23px, collection titles 22px and selected design names 19px (17px on phone). Supporting labels use the label role; commands use the command role. Status and diagnostics generally use the status role. Body text inherits the body line height; local source overrides remain.

Inherited heading tracking is included in frontmatter. Forms inherit the body face; studio controls use 15px desktop / 16px phone. Technical ingestion fields retain a local monospace treatment. BRB draft-status text is limited to 55ch; there is no global prose measure.

## Layout

The full-window desktop shell has a fixed 208px labelled sidebar, 34px content gutters and 24px vertical padding. At 601–1199px, the sidebar becomes 170px and horizontal content gutters become 24px. At 600px and below, a fixed 58px header and four-column bottom navigation replace the sidebar; main padding is 76px 18px 100px, with bottom-navigation safe-area padding. These are source measurements, not universal device categories.

At 1200px and above, scene authoring uses 180px scenes, 10px dividers, a bounded preview and 300px properties. BRB has preview plus 300px properties without a scene sequence. Narrow BRB uses Preview/Properties switching at 601–1199px. Scene editor switching still comes from the older 1150px breakpoint, with a separate Scenes choice at 800px; the newer grid rule also applies through 1199px. This mixed breakpoint implementation is a limitation, not a reusable breakpoint doctrine.

Live uses a preview/operation area and signals area on wide desktop; 601–1199px uses Operation/Signals switching. Phone retains local panel switching, stacks content and permits document scrolling. Clip shortcuts are bounded and scroll within their area. Authoring forms, generation and technical configuration remain desktop-only; phone exposes operation and prepared-media browsing/selection.

Setup uses divided rows with a 160×90 preview, content and a command on desktop; phone uses 94×53 previews and moves the command below the text. Resource lists are rows, with search and contextual creation. Main desktop authoring areas use internal overflow and bounded grids; do not infer that all zoom/reflow cases have been verified.

## Elevation & Depth

Ordinary panels are transparent with no border, radius or shadow; dividers and tonal contrast provide structure. Forms and open maintenance disclosures use panel fill. Dialogs use a border and a dark `#000a` backdrop. The existing layout-options popover retains `0 12px 30px #0008`; this is a local floating-control shadow, not a general card style. Connection indicators also retain a small legacy halo.

**The Flat Section Rule.** Ordinary sections use whitespace and dividers; do not give every section a raised card.

## Shapes

Controls and form containers use the control radius, media previews the media radius, and dialogs the dialog radius. Diagnostic badges retain their smaller radius. Structural rows and local tabs have square edges and a lower divider. Preview surfaces use a 16:9 ratio with contained media. Circles remain appropriate for connection-state dots; do not generalize them into decorative icon containers.

## Components

### Buttons

Commands are explicit and text-labelled. Primary buttons use mint with dark text and weight 600. Secondary buttons use transparent fill, text color and a border; hover uses the raised surface. Destructive commands use error text and the danger border. Base padding is in frontmatter; minimum height is 40px, rising to 44px for these shared controls on phone. Disabled primary/secondary controls have opacity .45. There is no new global hover animation.

The baseline focus rule is a 3px mint outline with 3px offset. Older, more-specific studio, field and Settings selectors retain 2px outlines and sometimes legacy hues; the baseline is not uniformly applied. Do not document these inconsistencies as interchangeable approved focus variants.

### Inputs / Fields

Generic fields use sidebar fill, a border and the control radius. Labels remain visible. Textareas and controls fit available width. Studio/Settings selectors still retain local padding, color and font overrides; frontmatter describes the shared generic field only. Phone studio fields increase to 16px. Validation and error behavior must remain explicit and preserve recoverable input.

### Navigation

Main destinations are real anchors in a labelled navigation landmark. The current destination uses `aria-current="page"`, selected fill and mint text. Desktop links have 12px padding and a 46px minimum height; phone links use 12px text, 12px 4px padding and a 44px minimum height.

Local collections, editor views and remote panels use underline treatments. The code uses buttons with `aria-current` or `aria-pressed` for local views; Settings section links use anchors and `aria-current`. This is not a uniformly implemented ARIA tablist. `studio-system.js` adds Left/Right/Home/End handling to studio tabs, collection buttons and Settings links; do not assume all switches share that behavior. Editor navigation provides a return breadcrumb.

### Rows / Containers

Design, asset, track and playlist rows use a lower border and text hierarchy rather than repeated cards. Contextual forms use panel fill and 24px padding. Show setup rows give preview and design-name links distinct actions; selection remains an explicit command. Prepared results include creation time, captured music, change summaries and a stale-draft label when applicable.

### Status / Disclosures

Status badges are non-interactive text with an outline and compact padding. Use explicit wording alongside color. Named disclosures expose maintenance and technical detail; scene count/duration remains visible outside Scene limits. The generation queue is compact when closed. Existing CSS can still give specific diagnostic badges tighter padding than the generic badge sample.

### Dialogs / Motion

Dialogs are bounded to viewport width minus 32px and 90dvh height, with scrolling and accessible names. Contextual upload and playlist dialogs return to the originating task. The local toggle thumb retains a .15s transform transition. Reduced-motion mode disables transitions, animation and smooth scrolling globally. The sidecar records these extensions; synthesized tonal ramps are panel previews only, not new production tokens.

## Do's and Don'ts

### Do:
- Do preserve semantic page links and their current-page state; use buttons for commands and local state changes.
- Do keep supporting status in words, including selected-for-next-use and playing-now distinctions.
- Do use bounded grid tracks, wrapping text and contained clip scrolling.
- Do preserve visible keyboard focus, reduced-motion overrides and the desktop-only authoring boundary.

### Don't:
- Don't use content-theme styling as application chrome.
- Don't turn every list row or section into an elevated card.
- Don't imply generation selected a result or replaced current playback.
- Don't copy legacy specificity, small helper text or inconsistent focus treatments into new components.

Not canonized or repaired: remaining legacy small text, eyebrows/glyph icon treatments, system-display styling outside the accepted utility hierarchy, qualified focus/color overrides and mixed editor breakpoints. These require scoped review; recording the production system does not approve or repair them. Full acceptance and exact-SHA CI/image-publication checks remain the delivery owner's responsibility.
