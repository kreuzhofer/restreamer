# Public event validation

Validation performed on 11 November 2026 for issue #84.

## Local media and concurrency

The test machine runs macOS 27.0.1 on Apple Silicon, Go 1.27.4, and FFmpeg
9.0.2. All publishing and viewing used localhost with generated media and dummy
credentials; no streaming platform was contacted.

- A 32-second source used 640 × 360 H.264 at 25 fps, two-second keyframes,
  approximately 900 kbit/s video, and 48 kHz AAC at 96 kbit/s. Live HLS and the
  complete replay both decoded successfully with FFmpeg's error-fatal mode.
  The retained media occupied 4,005,414 bytes, approximately 1 Mbit/s overall.
- Eight simultaneous FFmpeg clients each decoded eight seconds of the replay
  without errors. The run took 7.72 seconds on localhost. This is a concurrency
  smoke test, not a measurement of the server's maximum viewer capacity or
  performance over a real network.
- The automated prepared-programme test plays Starting soon, BRB, a clip, and
  Ending, decodes the resulting replay, and verifies that Ending closes the URL
  to further broadcasting. Other event tests exercise live input, reconnects,
  rehearsal isolation, target pauses, passwords, publication, deletion,
  corruption, restart recovery, and independent RTMP delivery on storage failure.

The server allows 64 simultaneous media responses. Required outbound bandwidth
scales with source bitrate and simultaneous viewers: for example, 6 Mbit/s ×
100 viewers is 600 Mbit/s before protocol overhead. This arithmetic does not
establish that a particular deployment can support 100 viewers.

## Browser verification

The desktop browser-control host was unavailable. With the user's approval,
Playwright used fresh, isolated browser profiles against the localhost app:

- Chrome 157.0.8103.39 completed a 30.12-second replay across an OBS-to-BRB
  source/initialization change and sought backward successfully. The bundled
  hls.js path was explicitly selected for a separate successful run.
- WebKit 27.2 completed the same replay using native HLS and sought backward.
  Its 390 × 844 touch viewport and Chrome's desktop/mobile viewports showed no
  horizontal overflow in the player or archive.
- Browser interactions created a password-protected target, authenticated a
  viewer, rotated the password and observed session revocation, and deleted the
  event and observed HTTP 404 for its URL. Archive withdrawal blocked public
  replay; authenticated private preview still played; publication restored
  replay at the same URL.
- A live Chrome touch-viewport sample showed source time 20.04 seconds after
  approximately 32.5 seconds of real-time source execution: roughly 12 seconds
  of end-to-end delay on localhost. Live seeking controls were absent. This is
  one sample, not a latency guarantee. Concurrent local tests loaded the host;
  headless Chrome reported dropped presentation frames (187 of 751 frames in
  the hls.js replay run), so this setup does not establish smooth playback or
  device capacity under normal deployment conditions.

Screenshots: [create target](testing/public-events/create-desktop.png),
[mobile live](testing/public-events/live-mobile.png),
[mobile replay](testing/public-events/replay-mobile.png),
[archive](testing/public-events/archive-mobile.png), and
[delete confirmation](testing/public-events/delete-mobile.png).

Physical iOS Safari and Android Chrome playback, audible output on those
devices, and real-network performance remain unverified. Desktop WebKit and
touch emulation do not substitute for those acceptance checks; retain them in
the live-playback ticket.

### FFmpeg source-transition limitation

FFmpeg 9.0.2 decodes every retained segment with its corresponding initialization
section, but its HLS demuxer reports decoding errors when this replay changes
initialization sections at the OBS-to-BRB transition. An independent control
repackaged by FFmpeg reproduces the same error. Both browser paths above play
that transition. This is consistent with the FFmpeg developers' discussion of
[HLS initialization changes and discontinuities](https://ffmpeg.org/pipermail/ffmpeg-devel/2022-April/295275.html).
The successful single-source FFmpeg replay check above must not be interpreted
as validation of all source transitions in FFmpeg's playlist decoder.

## Local full-suite limitation

The repository's untracked `output/continuous-tools/go` directory is picked up
by `go test ./...`; its Go distribution fixtures are not application packages.
Checks therefore also run from a separate snapshot of tracked project files,
without modifying that directory.

On this machine, the existing
`TestGeneratorEndingFinishUsesFrameRoundedBoundaryAtSupportedRates/30` test
reports “short ending did not finish black.” The same failure reproduces on the
unchanged starting commit `c61349393efab38b0668f72387d8321a71df43b0` with this
FFmpeg version. That commit's Linux CI passed. The final implementation still
requires successful GitHub CI for its exact SHA, including image publication.
The integration with newer main also includes its existing exact-black-ending
fix; the integrated full suite is checked separately from the original baseline.
