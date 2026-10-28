# Assessment B — detector and source evidence

Target: `internal/relay/web`. Independent of Assessment A; no A findings were provided. Read-only baseline. No production edits. Ignore list: `.impeccable/critique/ignore.md` absent.

## Deterministic scan

Executed exactly once:

```sh
IMPECCABLE_HOME=/tmp/restreamer-impeccable sh .agents/skills/impeccable/scripts/impeccable detect --json internal/relay/web > /tmp/ux-baseline-detector.json
```

Exit 2 (findings), valid full JSON saved at `/tmp/ux-baseline-detector.json`. 43 raw findings: 30 warnings, 13 advisories. These are repeated rule matches across files, initial markup and templates, **not 43 independently verified visible defects**. Most markup findings report line 0; source anchors below supply real locations where possible.

| Rule | Count |
| --- | ---: |
| undersized-ui-text | 21 |
| tiny-text | 9 |
| cramped-padding | 4 |
| overused-font | 3 |
| kicker-above-heading | 2 |
| side-tab | 1 |
| broken-image | 1 |
| skipped-heading | 1 |
| em-dash-overuse | 1 |

Files: `dashboard.css` (2 findings), `generator.html` (12), `index.html` (29). The JSON retains every rule name, description, severity, snippet, file and raw line location.

### Credible detector signals

- `internal/relay/web/mobile-remote.css:6` and `:10`: mobile connection, mode and freshness text explicitly uses 10px; `:24` makes broadcast status 10px. These are operational state cues. Prioritize readable status text rather than enlarging every decorative label indiscriminately.
- `internal/relay/web/dashboard.css:5`: secondary metric labels shrink to 9px below 650px. `:7` has 10px frame labels; `:8` reduces detailed counter text to 10px. Relevant markup: `index.html:113`, `:187`. These are real size declarations; actual readability/contrast needs browser validation.
- `internal/relay/web/index.html:124` and `generator.html:43`: uppercase eyebrows repeat the heading context (“CONNECTIONS & SETUP” / Settings; “YOUR BROADCAST” / Show setup). Low-priority redundancy, not proof of generic design.
- `internal/relay/web/index.html:132`: profile section starts at h3 despite being a peer of dynamically created h2 sections. Promote it to h2 for consistent navigation. The detector's literal “h1 followed by h3” is incomplete because ingestion and destination scripts insert intervening h2 elements.

### False positives and caveats

- **Broken image is a false positive**: `generator.html:105` marks `#brb-image` hidden initially. `brb-media.js:29` assigns a real authenticated image URL before showing it. Do not report a broken visible image. Separately, the image lacks intrinsic dimensions and its CSS (`show-setup.css:6`) only constrains maximum height, so reserving its layout space is worthwhile.
- **Cramped padding is not established**: the raw four hits include two unidentified `.panel` sections, `.secondary-stat`, and `.input-profile`. Descendant padding, breakpoint overrides and hidden state matter. `dashboard.css:2` gives secondary stats left padding, `:19` gives profile top padding; `workspace.css:13` intentionally removes the profile's nested border/padding. Do not make these a confirmed priority without a rendered reproduction.
- **Side accent is contextual**: `dashboard.css:14` uses an amber left border for the unsaved-changes warning. It has an operational purpose. Changing it solely because the detector calls side accents “AI” would weaken the evidence standard.
- **Font counts overlap**: `dashboard.css:1` declares Inter followed by system fallbacks; the two markup matches echo the same stack. Three hits are not three problems, and the stack alone says nothing reliable about design authorship.
- **Em dash count is misleading**: initial unknown metrics use `—` placeholders throughout `index.html`; the 13-count does not establish verbose prose.
- Hidden drawers, template content, missing runtime API state and initial loading labels limit which findings describe the displayed product. Do not claim live state or screenshots from this scan.

## Current Vercel guidance review

Source fetched during this review: [Vercel Web Interface Guidelines](https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md), 2026-10-28. Findings below are source-confirmed unless marked visual follow-up. Apply priorities using product impact, not treating every guideline as equally important.

- `internal/relay/web/generator.js:324` — **P1**: scene removal immediately autosaves; add undo or confirmation.
- `internal/relay/web/generator.js:265` — **P2**: list-item removal also autosaves without undo.
- `internal/relay/web/generator.html:44` — **P2**: chooser dialog lacks accessible name; connect its heading with `aria-labelledby`. Same at `:45` for preview.
- `internal/relay/web/index.html:32` / `generator.html:24` — **P2**: no skip link past repeated workspace navigation.
- `internal/relay/web/prepared-media.css:2` — **P2**: scrolling drawer lacks overscroll containment. Same for confirmation dialog at `mobile-remote.css:42`.
- `internal/relay/web/generator.html:163` — **P3**: design-name input lacks `name` and autocomplete policy; repeated across editor controls.
- `internal/relay/web/dashboard.css:9` — **P3**: `.preview-button` has focus styling but no hover feedback; primary stage buttons likewise at `:21`.
- `internal/relay/web/generator.html:105` — **P2**: image has no reserved dimensions; source assigns it asynchronously.
- `internal/relay/web/dashboard.js:13` — **P3**: user-facing decimals use `toFixed`, ignoring locale.
- `internal/relay/web/index.html:6` / `generator.html:4` — **P3**: dark color scheme exists, but matching `theme-color` is absent.

The highest-confidence behavioral issue is accidental content removal. `generator.js:277–279` schedules saving after 700ms; `:418–419` writes the changed draft. Browser reload is therefore not a dependable recovery path. Other destructive operations already use confirmation, so this inconsistency has a concrete local fix.

Positive source evidence: native buttons/inputs and wrapped labels are prevalent; focus-visible styles exist; reduced-motion disables transitions (`dashboard.css:6`); stage dialogs and real destination mutations have explicit review; dirty navigation has guards; collections/designs have URL state; mobile live operations establish 44px minimum targets. Do not report missing keyboard support, total absence of confirmation, missing reduced-motion, or color-only status as blanket defects.

## Browser evidence and limits

Parent reports browser host unavailable and instructed this agent not to drive browser yet. No fresh interactive browser tab was created; no desktop/mobile screenshots, keyboard walk-through, runtime state tests, or pixel contrast measurement were performed by Assessment B. No mutable-injection preflight; no detector overlay injected; **no user-visible overlay exists**. No Impeccable live server was started, so none needs stopping. Fallback evidence is the single detector JSON plus direct HTML/CSS/JS inspection. Browser functionality must remain explicitly unverified rather than being called passed.

Temporary artifacts retained for parent synthesis: this report and `/tmp/ux-baseline-detector.json`; runtime detector home is `/tmp/restreamer-impeccable`. Parent owns final retention/cleanup.
