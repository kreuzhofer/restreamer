# Continuous broadcast output — design interview

Status: shared understanding confirmed on 2026-10-30, including the one-second
ready-source response target. This is a design plan, not an implemented or
benchmarked feature.

## Problem and confirmed context

The operator observed a green picture on YouTube during an approximately
2.5-hour test stream while Twitch continued normally. YouTube recovered,
possibly after switching to BRB and back. The cause has not been reproduced or
established. Recovery after a switch does not prove that switching caused the
incident or that transcoding would fix it.

The desired behavior is continuous destination output across input interruptions
and changes among OBS, BRB, prestream, clips and ending media.

- OBS input: 1920×1080 at 25 frames per second.
- Production host: Hostinger VPS with 4 vCPU and 16 GB RAM, as reported by the user.
- Initially the user considered approximately 2–4 seconds of additional delay;
  in the first interview round they explicitly preferred allowing more delay for
  stability. The second round settled a target of about four seconds and a
  maximum of eight seconds additional relay delay during healthy operation.
  Platform playback latency is measured separately.
- No production CPU benchmark or hardware-encoder availability has been verified.

## Current implementation

The native Go relay forwards encoded media. Its broadcast session outlives OBS
publishers, remaps timestamps, requires a decodable keyframe to resume OBS, and
keeps destination connections independent. It prepares assets with FFmpeg but
does not continuously transcode live video. Released music-driven presentations
already reuse encoded visuals for music-only preparation and run an audio-only
encoder per retained playback. That audio encoder survives playlist/visual loops
and suspension, but is not a single encoder spanning all broadcast sources.

One shared width/height/frame-rate/audio-sample-rate profile currently governs OBS
compatibility and prepared media; profile changes require OFF with no pending
start, including rehearsal. The present input
stall threshold is three seconds. Preparation jobs share a bounded slot, but
there is no established CPU reservation for continuous video encoding.

The existing README proposes a future separately transcoded Twitch output for
downscaling a higher-resolution OBS stream. That proposal is narrower than a
continuously clocked program output and has not been implemented.

Current-state references: `internal/relay/brb.go`, `internal/relay/hub.go`,
`internal/relay/output.go`, and README sections “BRB and OBS disconnect
protection” and “Later: a separate 1080p Twitch output”. Source baseline:
`85f678e36daa8b28e104cfb5330d717d4d22688d`.

## Alternatives discussed

| Alternative | Benefit | Limitation or cost |
| --- | --- | --- |
| Harden encoded passthrough | Low compute cost, no additional encoding generation, stronger validation and recovery | Still switches among independently encoded sources |
| Add a bounded input buffer | Absorbs late arrivals and provides time to prepare fallback | Adds latency; does not repair corrupt media or produce missing content |
| Decode, select and continuously encode | Stable output format, frame cadence, encoder state and audio clock | Sustained compute demand, another lossy encode, worker supervision |
| Hybrid rollout | Evaluate processed YouTube output while Twitch remains on passthrough | Different delay between paths; not yet continuity protection for both |

A delay buffer cannot compensate for an encoder that continuously runs below
real time. A persistent encoder alone is also insufficient: its upstream frame
and audio supply must continue when an input decoder stalls.

## Accepted direction and remaining technical choices

Keep passthrough available and add an optional continuous-output path. Decode
sources, select frames and audio on a continuous clock, and keep the output
encoders running across source changes. Supply explicit fallback video and audio
when an input is unavailable. Share encoding among destinations using an
identical output profile, while retaining independent destination connections and
recovery. Additional profiles would require separately budgeted encoding work.

The accepted first-release scope is one shared 1080p25 output, selectable per
destination alongside passthrough. The program's video and audio timelines must
continue across source changes. A two-second keyframe interval (50 frames) is a
candidate for the benchmark. A YouTube-first comparison
is accepted after the local benchmark. No encoder library, preset, bitrate or
internal buffer sizing has been selected. See ADR 0009 for the accepted scope and
benchmark gate, and the second-round decisions below for failure/rollout policy.

Preserve the existing music-driven presentation behavior. Broader mixing across
arbitrary broadcast sources could be considered separately, but runtime music
mixing and visual reuse already exist and must not be presented as benefits that
require full video transcoding. See `docs/design/music-presentations.md` and
`internal/relay/presentation_playback.go`.

## First-round decisions

1. Accepted: optional continuous output alongside passthrough, with one shared
   1080p25 profile in the first release and per-destination path selection.
   Multiple output profiles are later work.
2. Accepted preference: allow more delay for stability. The original proposed
   four-second ceiling was not accepted; Q7 subsequently settled the finite bound.
3. Accepted: preserve chosen quality. If the existing VPS has insufficient
   headroom, stop rollout for explicit hardware/profile choices; no automatic
   quality reduction or purchase.
4. Accepted: preserve current music-driven presentations and playlists; do not
   expand arbitrary-source live mixing in this release.
5. Accepted: gate production implementation on a real-media benchmark on the
   intended server and a four-hour endurance run with switches, input/audio
   outages, and generation contention. Agree measured thresholds first.
6. Accepted: investigate the YouTube incident separately. Comparable evidence is
   required before claiming it fixed; reproducing it is not a prerequisite for
   planning the feature.

## Second-round decisions

7. Accepted: target about four seconds of additional server-side relay delay,
   with an eight-second ceiling during healthy operation. Buffers remain bounded;
   this budget excludes platform playback latency.
8. Accepted: after buffered input runs out, hold the last good picture with
   silence for at most one second, then use the configured fallback BRB. Use the
   existing emergency screen if fallback media is unavailable. Continue producing
   video/audio; retain deliberate-BRB intent and existing automatic LIVE recovery.
9. Accepted: an encoder failure is explicit, with automatic supervised restart
   and affected-destination reconnect. Never silently switch to passthrough.
   Passthrough destinations remain independent. Engine/host/network/platform
   failures are outside the source-continuity guarantee.
10. Accepted: output-mode changes require broadcast OFF in the first release
    with no pending start, consistent with the existing profile-change guard;
    profile changes already require OFF. Normal destination enable/disable
    controls remain available during a broadcast.
11. Accepted: live encoding has priority. Allow heavy asset generation during a
    broadcast only if the benchmark establishes sufficient headroom; otherwise
    visibly queue new jobs for broadcast capacity. Editing and selecting ready
    videos remain available. Q16 below settles treatment of an already-running job.
12. Accepted: after local benchmarks, arrange an explicit platform test with
    YouTube on continuous output and Twitch on passthrough. Both may later share
    continuous output. Preserve OFF-only rollback to passthrough; never change
    production selections automatically.

## Third-round decisions

13. Accepted: ready manual source changes take effect promptly at output rather
    than waiting behind buffered OBS footage. Buffer live input before source
    selection and preserve the running encoder. Unready requests remain Pending;
    do not claim a source is on air merely because the command was accepted.
14. Accepted: Stop now discards queued content and stops delivery immediately.
    Fixed-length endings send their final video/audio before disconnecting their
    destinations. Music-driven endings keep looping until explicitly stopped.
    Recovery must never restart a broadcast after Stop now.
15. Accepted: put output mode in existing destination settings and configure the
    shared profile once. Live gets compact mode/health indicators and a selector
    within the existing monitor when both paths are used. Identify the monitored
    path and estimated delay, and distinguish requested versus actual output.
    Do not create a new top-level page.
16. Accepted: when concurrent preparation is not qualified, starting a broadcast
    stops the current heavy preparation attempt and retains its job for automatic
    restart after broadcasting ends. Explain that rendering restarts from the
    beginning; preserve captured design/assets and any previous ready video.
17. Accepted: use the measurable gates below. Select encoder/preset/bitrate from
    evidence while retaining 1080p25 and existing audio requirements. Bring any
    quality/capacity trade-off back to the operator rather than degrading silently.

## Output and control contract

```text
OBS -> decode + bounded input buffer ---+
prepared media / fallback ------------+-> source selection on a continuous clock
                                      -> persistent video/audio output
                                      -> independent destination connections

passthrough remains a separate destination-selectable path
```

The buffer protects input timing; it is not a queue that forces ready manual
commands to wait several seconds. Media readiness remains a prerequisite. Keep
existing deliberate-BRB intent, clip return/suspension rules, exact-revision
activation, music behavior and source-readiness validation. A destination's
delivery failure must not stall the shared encoder or another destination.

An encoder worker restart may interrupt every destination sharing that encoder;
report that shared failure honestly. Supervise recovery with bounded retry
backoff, retaining the server's stage intent and stop/cancellation state. A full
application restart retains the existing OFF default. Repeated failures must
remain visible; retrying is not a healthy state and must not replay commands.

Normal ending completion accounts for each path's final queued media; immediate
stop deliberately bypasses draining. Existing ending-failure behavior remains
explicit rather than falsely reporting successful completion.

## Preparation capacity and monitoring

Qualify simultaneous heavy preparation separately from live encoding alone. If
that qualification fails, queue new preparation and stop/requeue a running
attempt when a broadcast needs its capacity. The job keeps its captured inputs;
an explicit user cancellation must prevent its automatic restart. This special
capacity reschedule does not introduce automatic retries for unrelated failures
or change recovery rules after a full application restart.

Show waiting-for-broadcast-capacity and rendering-will-restart states in the
existing queue. Keep editing, uploads subject to existing limits, and selection
of ready media available. Implementation must cover the existing preparation
workloads rather than protect one generator while leaving another unbounded.

Live and API consumers must distinguish output-path mode, current source,
readiness/pending state, encoder health, delivery health and estimated delay.
Do not add a permanent diagnostics panel to the normal workflow. Reuse existing
error/details surfaces for actionable faults. Unknown delay/status is shown as
unknown, never zero or healthy. Existing clients, including Stream Deck, need
compatibility tests so buffered or recovering output cannot appear falsely active.

## Benchmark and delivery gates

| Gate | Accepted requirement |
| --- | --- |
| Duration | At least four hours on the intended host using realistic moving video and audio |
| Output decoding | No decode errors in captured outgoing video/audio, including source changes and input fault recovery |
| Connection continuity | No destination reconnect caused by an ordinary source switch or input outage; deliberately injected encoder/network failures are tested separately |
| Cadence | Stable 25 fps output; missing input is filled according to the accepted fallback policy |
| Added relay delay | Target about 4 s; no more than 8 s during healthy operation; no growing delay or unbounded queue |
| Audio synchronization | Audio/video alignment within 80 ms, without accumulating drift over the run |
| CPU headroom | Average total host CPU below 75% of the four-vCPU capacity over each one-minute window under the admitted workload |
| Memory and queues | Bounded configured queues and no continuing memory/backlog growth after warm-up |
| Controls | Ready-source changes appear at relay output within 1 s once readiness and command acceptance are established; verify pending readiness, immediate Stop now, final-ending drain, and cancelled recovery |
| Isolation and capacity | Exercise individual destination failure and both admitted/rejected rendering contention |

The decode-error gate applies to outgoing media. Intentionally damaged input can
produce a diagnosed input error; it must not poison outgoing decoding. Measure
relay delay at ingress/egress for matched test frames, separately from camera,
internet and platform-player delay. Use visible frame/time markers and audio
events to check actual media, not just connection status. Thresholds are accepted
engineering targets, not claims of current performance. The one-second response
target makes “prompt” measurable and was accepted in final confirmation;
it excludes time spent awaiting source readiness and external platform playback.

Build a runnable media proof before production integration. Use the existing
authenticated stage/status and local RTMP/captured-media seams where practical.
The proof supplies capacity and continuity evidence; production integration must
then verify the complete queue, UI, API, security and recovery contracts. An
intentional overload experiment determines admission policy; it is not counted
as passing the sustained qualifying workload.

Record the machine, encoder build, settings, source material, measurements,
fault schedule and results so the benchmark is reproducible. Encoder/library,
preset, bitrate and internal queue sizing are delegated technical choices within
the accepted requirements. Do not commit to a particular implementation or
quality compromise before that evidence exists.

After qualification, arrange the explicit YouTube-first platform trial with
Twitch on passthrough. Compare platform behavior and visual/audio quality before
opting other destinations in. Default existing destinations to passthrough;
do not migrate their saved selections automatically. Switching paths and rollback
require OFF. Production delivery later requires the existing review, local checks
and exact-SHA GitHub CI gate; release claims additionally require main image
publication.

## Out of scope and confirmation

Multiple encoded output profiles, new live music-mixing controls, changing OBS
itself, automatic infrastructure purchases, silent quality reductions, seamless
host/network failover, and diagnosing the YouTube incident by inference are out of
scope. Continuous output is not a guarantee of platform-side public playback.

The operator confirmed the complete plan and the one-second ready-source
response target on 2026-10-30. The next step is the runnable media benchmark;
production implementation remains gated on its results. A standalone
[capacity-screen harness](../../experiments/continuous-output/README.md) is the
first experimental step. It does not yet implement the source-switching proof
or qualify any production rollout; intended-host qualification remains pending.

## Evidence to collect

Accepted gate: a real-media benchmark on the intended host, followed
by at least four hours of playback with repeated source switches, OBS
disconnects/reconnects, and audio interruptions. Measure decoding errors, frame
cadence, audio drift, additional delay, queue growth, CPU and memory. Test asset
generation contention. Real-platform verification needs a separately agreed test
session; ordinary automated tests use local sinks and dummy credentials.

Keep the unexplained YouTube incident separate from architectural acceptance.
Useful incident evidence includes the affected time, source/transition history,
destination reconnect/drop history, outgoing media capture and YouTube ingest
health messages. None of that evidence has yet been collected for this incident.

## Primary references

- [FFmpeg streamcopy and transcoding](https://www.ffmpeg.org/ffmpeg.html): copying
  avoids decoding/encoding; transcoding enables filtering at computational and
  potentially lossy quality cost.
- [YouTube encoder guidance](https://support.google.com/youtube/answer/2853702?hl=en):
  recommends CBR and two-second keyframes, with a maximum four-second interval.
- [YouTube ingest health diagnostics](https://developers.google.com/youtube/data/live/reference/rest/v3/liveStreams/health_status_messages):
  platform-reported configuration issues complement relay-side delivery status.

References checked during the discussion on 2026-10-29. These recommendations
are not measurements of the user's VPS or a diagnosis of the reported incident.
