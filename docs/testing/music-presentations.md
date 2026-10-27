# Music presentation verification

Issue #58 uses the previously agreed authenticated HTTP/status, exact media preview,
local RTMP, timed-tick and browser seams. Fixtures use dummy credentials and local
receivers only.

Implemented regression coverage:

- Ordered immutable playlist revisions, stale edits, restart persistence, usage
  and protected uploads; deletion preserves original music.
- Authentication and same-origin mutation requirements on new endpoints.
- Music-derived duration rounded up to a video frame; encoded visual reuse after
  changing music gain; preparation keeps OFF and changes no selection.
- Independent continuous clocks across several cycles; music-driven ENDING stays
  active until explicit Stop; restart remains OFF.
- Local RTMP delivery, rehearsal isolation, on-air playlist version pinning, and
  decodable mixed preview with the original playlist order.
- Source video audio survives and repeats with its visual under continuous music.
- Fixed endings preserve their final whole-composition fade.
- A 630-second playlist exceeds the previous ten-minute render envelope and opens
  after restart. Track count over capacity is rejected.
- Missing music and unavailable FFmpeg produce explicit failures; no substitution.
- Deleting generated results removes PCM/export/capture files, keeps shared visuals
  until last use, and preserves uploaded tracks.
- Browser-independent UI tests retain exact old selections, preserve stale playlist
  drafts, and detect reordering as unsaved work.

Browser workflow pending: authenticated desktop, narrow desktop and phone review
of playlist create/edit/reorder, both design duration modes, BRB preparation and
Show setup assignment, preview/download, plus phone operation. Chrome extension
creation timed out and native access returned `cgWindowNotFound` on 2026-10-27.
No screenshot or browser pass is claimed. A loopback fixture with zero destinations
is available at `http://127.0.0.1:18871` using documented dummy `stage-test` credentials.

Final full-suite, review and exact-SHA GitHub CI evidence belong to delivery closeout.

## Independent review

Standards review found five documented issues and one naming concern: unbounded
corrupt-video reads, interrupted-save deletion references, cross-filesystem BRB
capture, profile-insensitive BRB reuse, polling interrupting previews, and an
incorrect timestamp variable name. All were fixed. Follow-up review found a last-use
cleanup edge after interrupted publication; metadata now supplies deletion candidates
as well as protection, covered by a restart/deletion regression.

Spec review found five issues: fixed prestream circular audio semantics, BRB preview
polling, two-pass previews misrepresenting music-driven looping, BRB edit links, and
missing historical playlist duration. All were fixed. Follow-up added an exact-version
guard for late duration errors and duration lookup in the BRB picker. Fixed-mode
playlist audio now shares the existing circular crossfade/rotation and AAC warm-up.

## Local checks

The initial full race suite passed all tests except the pre-existing FFmpeg 9
30 fps ending-frame failure (`TestGeneratorEndingFinishUsesFrameRoundedBoundaryAtSupportedRates/30`).
That unchanged test passes with installed FFmpeg 7, but the full FFmpeg 7 run fails
several transition-rendering tests (including the new fixed-playlist test when
building its base visual). The unchanged baseline `3795bee` also fails
`TestGeneratorCrossfadeKeepsMusicOnShortenedSequenceTimeline` with FFmpeg 7.
Neither local full run is claimed green. The focused music/presentation race
suite with default FFmpeg 9, 39 web tests, artwork checks and Go vet passed.
The Linux GitHub CI run is the required full-suite release gate. Browser
verification is still unavailable as described above; no browser pass is inferred
from API or DOM-unit tests.

The final isolated HTTP fixture prepared prestream, ending and BRB presentations
from a two-track 2.3-second playlist. Each reports a 2.32-second one-pass export at
25 fps, and broadcast status remained OFF. No external targets are configured.
