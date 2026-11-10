# Continuous output integration evidence

Status: implementation reviewed for merge on 2026-11-10; platform opt-in remains a separate step. Passthrough remains the default. This document does not authorize production opt-in or claim that the YouTube green-screen incident is fixed.

## Implementation and verification boundary

The Go server owns the source selector, 25 Hz clock, bounded queues, stage intent and worker recovery. FFmpeg decodes each source and runs one shared 1080p25 H.264/AAC encoder. Each destination keeps an independent connection and queue. The continuous monitor subscribes to that encoded output, so source status describes emitted media rather than merely an accepted command.

The candidate output uses x264 veryfast, three encoder threads, zerolatency, 8 Mbps CBR, a 16 Mbps VBV buffer, 50-frame GOPs, and 160 kbps stereo AAC at 48 kHz. The existing streaming profile still governs input compatibility and prepared assets. Continuous output has its own fixed 1920×1080p25 format; this release does not add more encoded profiles.

Compressed source admission is bounded to 512 events and 16 MiB. Decoded queues hold at most 130 live frames and 50 prepared frames. Raw encoder admission holds at most 50 video frames and their paired audio blocks (two seconds at 25 fps), with at most 66 admitted frames tracked through pipe/codec buffering until emission. Worker cancellation closes its pipes and waits for exit. Status exposes current decoder queue lengths, drops for the current decoder instances, source-admission overflows, encoder restarts and nullable estimated relay delay. The estimate matches decoded presentation timestamps back to compressed input arrivals, including reordered B-frames; it excludes platform playback latency and is not an independently measured ingress-to-sink gate.

## Automated behavioral evidence

Tests use authenticated HTTP commands/status, local RTMP receivers and captured outgoing media. They cover:

- Legacy configuration defaults, invalid mode rejection, OFF/pending guards, persisted destination modes and omitted-field compatibility.
- Continuous decoded output and 40 ms frame cadence; ready OBS/BRB changes within one second of committed readiness; independent destination recovery and authenticated monitor media.
- Input and audio outages, deliberate BRB, automatic return to OBS, emergency media, supervised encoder failure, passthrough isolation and Stop cancellation.
- Fixed-ending final-frame delivery and music-driven looping, including rehearsal with no destination connections.
- Short synchronized flash/chirp media, including B-frames, within 80 ms (a one-microsecond comparison tolerance accommodates floating-point representation).
- Running preparation cancelled for broadcast capacity, explicit user cancellation, queued restart after Stop, unchanged captured revision despite draft edits, and retained previous ready media.
- Browser-controller tests for mode/profile guards, pending starts, standard MediaSource and iPhone ManagedMediaSource, and intentional monitor-path cancellation.

Heavy preparation is serialized and deferred whenever a continuous broadcast or rehearsal reserves capacity. Retained generator/library/theme jobs restart from their captured inputs after Stop. HTTP uploads and preview requests keep their existing request deadlines; a disconnected or expired request must be submitted again. Application restart retains the existing interrupted-job/retry behavior.

## Intended-host replay

An isolated container on the authorized four-vCPU, 16 GiB VPS replayed the operator's 1080p25 H.264/AAC recording through the production Go engine for 180 seconds. The original recording was mounted read-only. Both RTMP sinks were loopback-only inside a container with networking disabled, a three-CPU ceiling and a 2 GiB memory limit; no platform credentials or production settings were used.

This first smoke binary came from checkpoint `1c050f5` plus settings/monitor changes, before the later decoder queue-limit and ingress-PTS refinement. It is evidence for integration feasibility, not final-SHA qualification.

- Two manual BRB/OBS cycles and one input disconnect/return completed.
- One deliberately disconnected target made two connections; the healthy target kept one connection.
- All three captured segments decoded with FFmpeg `-xerror`, with zero video timestamp discontinuities. The healthy capture contained 4,522 video frames at 40 ms intervals.
- Total host CPU ranged from 36.37% to 40.10% over the recorded rolling one-minute windows, including other host applications.
- No encoder restart occurred during the run.

Raw evidence is retained locally in `output/continuous-output-checkpoint-20261101/integrated-180s-report.json` and on the VPS under `/var/tmp/restreamer-continuous-integration-20261101/data/run-180s/`. Reports and source recordings are not committed. The earlier four-hour experimental clock run remains separate evidence; it did not exercise this production implementation.

### Committed-engine repeat

The same isolated 180-second schedule passed again with a clean build of
`72d48aef86683f350a37fbefd87646fbfe54c19f` (`vcs.modified=false`), including the
final decoder queue/PTS changes. All three captures decoded; the healthy sink
again received 4,522 frames with zero timestamp discontinuities and one
connection. The faulted sink made exactly two connections. There were no encoder
restarts, decoded-frame drops or source-queue overflows. Sampled queue peaks were
127 live frames and three prepared frames. Peak relay-process RSS was 869.6 MiB
(this excludes FFmpeg child processes). Total-host one-minute CPU windows ranged
from 33.96% to 38.94%. The engine's estimated OBS relay delay had a median of
5.348 s and maximum of 5.549 s; this remains an estimate, not independent matched
ingress-to-sink measurement. Raw evidence is retained in
`output/continuous-output-checkpoint-20261101/integrated-72d48ae-report.json`
and VPS `data/run-72d48ae/` under the same scratch root.

## Endurance failure and brief-stall correction

The clean `fbc420f` candidate completed four hours on November 3 but failed
qualification: 13 encoder restarts caused 15 connections at the deliberately
faulted sink and 14 at the healthy sink. The deferred render resumed after Stop,
and there were no source-admission overflows. Peak rolling-minute host CPU was
51.19%, but samples around the restarts showed brief CPU bursts of roughly
80–97%. All restarts occurred near a wall-clock minute boundary. This correlation
does not identify which host workload caused the bursts. The run did not reach
final captured-media analysis and must not be reported as passing A/V or delay
evidence. Original captures remain recoverable as hash-verified gzip archives.

A short local reproduction pauses only the synthetic fixture's encoder process.
With the old two-frame input queue, a one-second pause consistently reaches the
200 ms input-write deadline, cancels the worker, and reconnects both destinations.
The same fixture without the pause remains connected. Targeted temporary tracing
confirmed the input-queue deadline as the cancellation path; tracing was removed.

The corrected queues absorb two seconds of transient scheduling delay while the
Go clock continues selecting paired frames. The worker can then catch up without
adding a permanent pause to its media timeline. Buffers remain bounded; a longer
stall still triggers recovery. `TestContinuousBriefEncoderStallsPreserveSessionAndCatchUp`
checks repeated one- and two-second stalls through HTTP status and local RTMP:
one session, 40 ms timestamp cadence, catch-up, decoded flash/chirp sync within
80 ms, relay delay below eight seconds, and prompt Stop with a stalled backlog.
The recovery test additionally covers a worker that stays paused, actual process
termination, passthrough isolation and cancellation of pending restart.

The replacement-host endurance run below tests the same corrected engine; the
short regression alone is not its qualification evidence.

## Browser verification

Authenticated desktop Chrome verified destination import/edit/save, mode selection, rehearsal, both monitor paths and actual H.264/AAC playback. This found a layout defect: inserting monitor controls directly into the existing four-row grid assigned the flexible video row to the selector. Grouping those controls in the heading restores the original video row. Intentional path cancellation also no longer appears as a playback failure; its regression test failed before the correction and passes afterward.

Native Chrome access recovered on November 9. The isolated authenticated fixture was checked at full desktop, 900px narrow desktop and 390px emulated phone width, including actual passthrough/continuous playback and destination-blocked rehearsal. This exposed a second layout defect: the fixed minimum monitor track could clip its caption when stage controls expanded. Content-sized minimum tracks with size containment on the video now preserve the monitor status and caption without making the video force its intrinsic dimensions. The corrected layouts were inspected in Chrome; emulation is not physical-iPhone playback evidence. Earlier screenshots in `output/continuous-output-checkpoint-20261101/screenshots/` predate this correction.

## Remaining qualification and platform trial

The November 9 run below completes the sustained media checks on the replacement
host. The remaining aggregate-memory uncertainty should be checked with the
bounded observer during an extended test-channel trial. Heavy rendering remains
deferred; concurrent heavy rendering is not qualified. Physical iPhone playback
of this candidate and real-platform ingest/quality assessment are not claimed.

Then agree a test-channel session with the operator: YouTube continuous, Twitch passthrough, with controlled source changes and platform ingest-health/video/audio review. Keep platform latency separate from relay delay. A subsequent comparison may use both destinations on the shared continuous encoder. Rollback is explicit: stop the broadcast and pending start, change the destination to Passthrough, save, then start again. Never change production selections automatically.

For a binary rollback to a version predating this feature, stop delivery first, back up the configuration and saved destinations privately, and remove their optional `output_mode` fields before launching the older binary. Switching the mode in the UI alone does not remove a field that older strict JSON readers reject. Preserve destination keys and permissions.

Delivery additionally requires successful GitHub CI for the exact pushed SHA. Branch CI does not imply main image publication or deployment.

## Additional regression corrections from diagnosis

The complete local suite exposed an ending-frame defect at 30 fps: predictive encoding left eight RGB samples at value 1 instead of exact black. Encoding the final faded frame as a keyframe preserves the exact-black endpoint and the original duration. The unchanged strict frame-boundary regression passes at 24, 25 and 30 fps with FFmpeg 7 and 9.

The playlist integration test also assumed that four seconds of wall-clock capture guaranteed four seconds of received audio. A late keyframe join could leave a shorter capture and let tone analysis read past its buffer. Capture now waits for the required received AAC timestamp span with a hard wall-clock deadline, and validates the complete decoded analysis window. Both continuous and passthrough music tests passed three consecutive runs.


## Replacement-host endurance and timing reassessment — November 10

The operator supplied a fresh eight-logical-CPU AMD EPYC host with 32 GB RAM.
The clean `55555897ec190bb5c697bdd8b4062129fa207e6f` binary ran for
14,400.610 seconds on November 9, with the same representative recording, runtime
layers and three-CPU/two-GiB container limits. There were no external destinations,
published ports or platform credentials. The engine is unchanged by the subsequent
benchmark/documentation corrections.

- Zero encoder restarts, source overflows, outgoing decode errors or cadence gaps.
  The healthy sink kept one connection and received 360,170 frames; the deliberately
  disconnected sink made exactly two connections.
- Matched ingress-to-sink delay: median 4.655 s, maximum 5.794 s. Maximum A/V marker
  error: 0.521 ms over 26,692 events. The deferred render completed after Stop.
- Peak rolling-minute CPU: 17.287% of the eight-logical-CPU host, equivalent to
  34.574% of four logical CPUs in CPU-time units; the container quota was three
  CPUs. This does not qualify the original contended Hostinger host.
- All 24 ready BRB commands reached changed-source sink frames in 43–162 ms.
  All 24 Go Live commands did so in 0.840–1.995 s, including the fresh-keyframe wait.

The original report is preserved as rejected: its `ready_command_response` gate
incorrectly applied a one-second limit from command submission to both actions.
The operator accepted two seconds total for Go Live with the expected two-second
GOP on November 10. The committed
[reassessment](../../experiments/continuous-output/evidence/integrated-20261109-reassessment.json)
records the original report SHA-256, original gates and revised gates. It does not
invent a readiness timestamp: post-readiness Go Live delay was not measured
separately in the endurance capture. The existing authenticated integration
regression asserts emitted-source status within one second after committed LIVE
readiness; ready BRB retains its one-second limit.

After ten minutes of warm-up, eight successive half-hour (last: twenty-minute)
relay RSS medians were 712.06, 712.36, 712.79, 711.72, 714.13, 716.07, 713.45 and
712.45 MiB. Median live queue occupancy stayed at 110 frames. There is no sustained
relay RSS/backlog growth in these samples. Whole-container anonymous memory
first/last-quartile medians rose by 25.69 MiB, with a 1,039.25 MiB peak. That metric
includes the Python observer, which retained all 14,356 samples in memory. Future
captures retain only the last 122 samples for the CPU safety check while writing
all samples to disk. The aggregate rise is confounded; these measurements do not
prove a whole-container memory plateau or attribute every byte to the observer.
Keep that limitation visible during the extended platform trial.

The separate race-tested regressions cover encoder termination/prolonged stalls,
Stop cancellation, passthrough isolation and final-ending drain. Native Chrome
verification covers full desktop, narrow desktop and emulated phone layouts as
described above. Independent standards/spec review found one unsafe installer
smoke-test cleanup path; it was corrected before merge with occupied-host and
failed-query regression coverage. Final main CI and image publication remain the
release gate; see the merged PR checks for the delivered SHA.

Private raw capture and telemetry are retained under
`/var/tmp/restreamer-qualification-20261109/data/endurance-5555589/` on the test
host and `output/continuous-new-host-20261109/` locally. No public certificate
issuance, deployment or platform broadcast is part of this verification.
