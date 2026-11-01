# Continuous output integration evidence

Status: implementation under validation on 2026-11-01. Passthrough remains the default. This document does not authorize production opt-in or claim that the YouTube green-screen incident is fixed.

## Implementation and verification boundary

The Go server owns the source selector, 25 Hz clock, bounded queues, stage intent and worker recovery. FFmpeg decodes each source and runs one shared 1080p25 H.264/AAC encoder. Each destination keeps an independent connection and queue. The continuous monitor subscribes to that encoded output, so source status describes emitted media rather than merely an accepted command.

The candidate output uses x264 veryfast, three encoder threads, zerolatency, 8 Mbps CBR, a 16 Mbps VBV buffer, 50-frame GOPs, and 160 kbps stereo AAC at 48 kHz. The existing streaming profile still governs input compatibility and prepared assets. Continuous output has its own fixed 1920×1080p25 format; this release does not add more encoded profiles.

Compressed source admission is bounded to 512 events and 16 MiB. Decoded queues hold at most 130 live frames and 50 prepared frames. Raw encoder admission is bounded, and worker cancellation closes its pipes and waits for exit. Status exposes current decoder queue lengths, drops for the current decoder instances, source-admission overflows, encoder restarts and nullable estimated relay delay. The estimate matches decoded presentation timestamps back to compressed input arrivals, including reordered B-frames; it excludes platform playback latency and is not an independently measured ingress-to-sink gate.

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

## Remaining qualification and platform trial

Before production opt-in, run the final production binary on the intended host for at least four hours with representative media. Verify the accepted design gates: zero outgoing decode errors and ordinary-source reconnects, stable 25 fps, matched compressed-ingress-to-sink delay no greater than eight seconds, A/V alignment within 80 ms without drift, every one-minute total-host CPU window below 75%, and bounded memory/backlog after warm-up. Include input/audio outages, decoder/encoder failure, individual destination faults, pending readiness, Stop, final-ending drain and deferred render restart. Concurrency with heavy rendering has not been qualified and is therefore not admitted.

Then agree a test-channel session with the operator: YouTube continuous, Twitch passthrough, with controlled source changes and platform ingest-health/video/audio review. Keep platform latency separate from relay delay. A subsequent comparison may use both destinations on the shared continuous encoder. Rollback is explicit: stop the broadcast and pending start, change the destination to Passthrough, save, then start again. Never change production selections automatically.

Delivery additionally requires successful GitHub CI for the exact pushed SHA. Branch CI does not imply main image publication or deployment.
