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

## Remaining browser verification

The browser-control host was unavailable. Desktop/mobile visual checks,
screenshots, native iOS Safari and Android Chrome playback, and measured
end-to-end player latency remain unverified. FFmpeg decoding does not substitute
for those checks. The 10–20-second live delay is a design target, not a measured
result. The task must retain these acceptance items until browser verification
is available.

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
