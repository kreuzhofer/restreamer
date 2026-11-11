# Animation-aligned scene length

Verified in an isolated authenticated fixture (dummy stage-test credentials,
no destinations) at 1440×900, 1024×768, and 390×844.

- Scene length switches between seconds and whole animation loops. Two Arcade
  loops calculate to 32s; a 0.5s crossfade at 25fps gives 32.52s of rendered scene and
  preserves 32s of sequence advance. Two scenes of two and one loops total 48s.
- Saved loop counts and calculated lengths survive reload. Returning to seconds
  keeps the calculated duration as an editable value.
- Zero loops produces an explicit field error; correcting it restores validation.
- Narrow desktop Properties exposes the same controls. Phone keeps authoring
  disabled as designed and fits 390px without horizontal document overflow.
- No browser console errors occurred in the final verification instance.
- Authenticated API tests cover theme adoption, unsupported/static/one-shot themes,
  invalid counts, persistence, generation capture, and an encoded Pac-Man scene
  boundary. The latter fails when the old background-reset behavior is restored.
- 59 web tests pass; focused race tests and go vet pass. Full local make check ran
  and retained the ten documented FFmpeg 7.1.5 transition failure groups. GitHub
  Linux CI remains the complete build/release gate.

Screenshots: desktop.png, narrow.png, phone.png. The separate full-app design
parity review is not represented by these scoped feature checks.
