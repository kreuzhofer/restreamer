# Independent prototype assessment B

Scope: read-only review of `/tmp/restreamer-ux-prototype/internal/relay/web/ux-prototype`. No Assessment A output was read. No application files were edited. The intended in-memory state and simulated generation, media, and broadcasts are accepted constraints, not defects.

Method: fetched the current [Web Interface Guidelines](https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md), inspected HTML/CSS/JavaScript, inspected supplied 1440×900 editor and 390×1044 phone screenshots, and executed the JavaScript state transitions in a Node VM with a minimal DOM stub. VM checks establish state/control-flow behavior, not browser layout or actual assistive-technology behavior. No independent live-browser interaction was performed.

The required installed launcher was run once:

```sh
IMPECCABLE_HOME=/tmp/restreamer-impeccable sh /Users/daniel/src/github/kreuzhofer/restreamer/.agents/skills/impeccable/scripts/impeccable detect --json /tmp/restreamer-ux-prototype/internal/relay/web/ux-prototype
```

Detector output: `/tmp/ux-prototype-detector.json`, containing `[]`. This does not establish functional or accessibility compliance.

## Findings — app.js

- `app.js:139` — **P1 accessibility: ordinary control changes discard keyboard focus.** Theme/music/mode/color/volume/delivery changes call `render()`, which replaces the entire `#app` subtree at line 34 without restoring the focused control. The inspector tabs explicitly restore focus at lines 68/141, but the forms do not. A keyboard user changing a select or pressing an arrow on the volume slider loses the control and cannot continue from the same place; library selection similarly replaces its focused checkbox. Generation completion also replaces the entire editor at line 46, interrupting ongoing input even though the UI says users can keep working. Preserve the active control and selection, or update only affected nodes. Source-confirmed DOM replacement; browser/AT reproduction remains to be performed.

- `app.js:80` — **P1 state correctness: changing a playlist silently changes existing designs while the editor still claims its video matches.** `playlist-save` mutates the global playlist object; designs store only its ID and never capture a revision. The dialog explicitly promises at line 50 that saved designs keep their selected version until the user chooses an update. The draft version does not change, so the editor reports “Matches this design” while its displayed duration and next generation have changed. VM reproduction: append `t3` to `p0`, save, and inspect `d1`: playlist duration changes from `10:56` to `16:04`; `d1.version !== latestVideo.draftVersion` remains `false`. Capture the selected playlist revision in the design and offer explicit adoption, or revise the promise and mark affected drafts changed. Generated videos already preserve their displayed music metadata; keep that protection.

- `app.js:47` — **P2 runtime: deleting a generating design leaves an orphan job that throws when opened.** Start generation for an unselected design such as `d3`, navigate to Library and delete it before generation finishes. Deletion protection at line 52 omits running jobs, and completion creates a new video referring to the deleted design. “Video ready” then dereferences `getDesign(v.design).version`. VM reproduction produced `Cannot read properties of undefined (reading 'version')`. Protect designs referenced by pending jobs or explicitly cancel their jobs before deletion; also handle a missing design in job review.

- `app.js:117` — **P2 dead end: Replace current video opens an empty chooser during clip playback.** The Live screen offers replacement whenever an active video exists, including clips. The handler passes `clip` to `chooser()`, which filters designs by stage; no design has stage `clip`. The dialog has no choices or explanation. VM reproduction returned title `Replace current video` and zero `chooser-row` entries. Route clip replacement to the clip library, or omit the action where it is unsupported. Stage-based replacement should retain its existing candidate filtering.

## Visual assessment and preserved behavior

The supplied desktop and phone views support the approved restrained dark direction: clear top-level destinations, readable muted text, restrained accent use, and a comprehensible separation between preparation and Live. The phone screen makes selected videos and Change actions easy to scan. The visible skip link in the supplied phone screenshot is a focused accessibility control; it is not treated as persistent layout chrome or a design defect.

The source includes native buttons/links, labeled controls, accessible names for icon actions, visible focus styling, a skip link, a live toast region, native modal dialogs, reduced-motion CSS, and an arrow-key implementation for the inspector tabs. These are useful foundations. None of the findings requires a visual redesign or a frontend dependency.

Minor guideline deviations (form names/autocomplete, image dimension attributes, theme-color metadata, typography preferences) are omitted from the fix list because they do not outweigh the concrete interaction failures above in this throwaway prototype.

## Focused re-review after fixes

Verdict: **all four original findings are resolved for this prototype.** Reviewed the latest source and reran targeted state-transition checks. The detector was not rerun. This section supersedes the original findings above.

- `app.js:35`, `app.js:143` — **Focus loss: resolved.** Render now captures the active app control, restores its counterpart after replacement, and restores text selection where supported. Library checkbox selection restores the specific checkbox separately. This covers the originally identified select/range/delivery changes and the message field during generation completion. The parent independently reported an actual-browser Music-select ArrowDown check that retained the active `SELECT[data-change=music]`; that browser result is supporting evidence supplied by the parent, not a browser check performed by this assessor.
- `app.js:18`, `app.js:40`, `app.js:56`, `app.js:76` — **Silent playlist changes: resolved.** Designs retain music label, duration and revision; the editor uses the pinned duration, exposes an update notice, and adopts the newer playlist through a version-incrementing action. Targeted VM check: after changing the library playlist, the design remained `10:56` while the library became `16:04`; the design revision stayed unchanged. Explicit adoption then changed the design to `16:04` and incremented its version. Snapshot generation preserves the pinned metadata. Actual audio rendering remains outside the agreed simulated scope.
- `app.js:48`, `app.js:53`, `app.js:102` — **Deleting a generating design: resolved.** The deletion dialog now marks the running job's design `Protected · generating` and disables deletion when it is the only item. VM checks confirmed both. Deleting the design after completion clears its job, and calling the job view afterward returns safely. The ready-result view also checks for a deleted video and tolerates a missing design.
- `app.js:121`, `app.js:124`, `app.js:126` — **Empty clip replacement chooser: resolved.** Replacement during clip playback now opens the populated clip chooser with `Replace clip` actions, instead of filtering for the nonexistent `clip` design stage. Source inspection confirms candidates use the existing clip library. A VM state-transition check confirmed replacing the clip keeps the interrupted stage and video as the return target.

No new findings were sought in this bounded re-review, and none of these fixes requires changing the approved visual direction.
