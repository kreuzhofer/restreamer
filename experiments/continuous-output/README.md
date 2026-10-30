# Continuous-output experiments

These isolated measurement tools support the [confirmed design](../../docs/design/continuous-output.md).
They include a capacity screen and a clocked source-switching proof. Neither
qualifies production rollout or selects the production implementation library.
The normal Restreamer executable and saved configuration are unchanged.

## Capacity screen

The screen runs one persistent software H.264/AAC encoder over a local recording,
loops its input, captures the result, and independently decodes every captured
frame. It checks 1920×1080 at 25 fps, stereo AAC at 48 kHz, frame count, and
40 ms video timestamp spacing. Linux measurements include whole-host CPU and
available memory, plus encoder RSS. CPU is evaluated in approximately 60-second
rolling windows sampled once per second; every measured window must stay below
75%. The default 90-second screen includes complete CPU windows; runs shorter
than 61 seconds may leave that gate unmeasured. Progress lag must not grow by
more than one second after warm-up, and finishing may take at most three extra
seconds. These are early rejection checks, not end-to-end latency measurements.
Unknown readings are never treated as passes.

## Run on the intended host

Use Python 3.9+ and an installed FFmpeg/FFprobe pair with `libx264` and AAC. No
Python packages, Go installation, credentials, or exposed ports are needed.
Run directly on the intended Linux host so `/proc` describes that host. Container
CPU quotas can invalidate host percentages; the report records the cgroup v2
quota when available, but does not infer VPS capacity from a container or Mac.

First arrange an idle window with no live broadcast. The screen itself cannot
know whether a separate production instance is broadcasting. It does not stop,
restart, or modify that instance. Supply a representative **local recording with
both video and audio**, ideally the OBS material from the affected stream. The
recording stays on the machine. Network input, streaming credentials, synthetic
silence substitution, and remote output URLs are deliberately unsupported.

```sh
python3 experiments/continuous-output/benchmark.py \
  --input /path/to/representative-obs-recording.mp4 \
  --output /path/to/new-capacity-run \
  --seconds 90 --preset veryfast --bitrate-kbps 8000 --threads 3
```

The bitrate and preset are benchmark candidates, **not approved production
quality settings**. Compare visual quality and host results before choosing them.
A short screen checks whether further engineering is worth testing on this
machine. Repeat with a new output directory for any different preset. Keep
other host workloads recorded with the results rather than assuming an idle host.

The output directory must not exist. Capture space is checked before starting:
at 8 Mbit/s, budget roughly 1.1 MB per second, plus muxing margin and a 512 MiB
reserve. A 14,400-second capture needs roughly 15 GB, with more free space required
by the preflight margin. Do not start a four-hour capacity-only run thinking it
qualifies the complete endurance contract.

Ctrl-C or SIGTERM stops the encoder and records an interrupted result. Errors,
timeouts, invalid media, or failing measured gates produce a nonzero exit code.
Successful exit means **this screen completed**, not that the host or feature is
qualified. `rollout_qualified` is always `false`.

## Evidence

Each new run directory contains:

- `report.json`: source SHA-256, FFmpeg build/settings, machine information,
  periodic samples, progress, measured gates and explicitly unmeasured gates.
- `capture.flv`: captured H.264/AAC output for review.
- `frame-timestamps.txt`: compact decoded video timestamp evidence.
- `encoder.log`, `decode.log`, `probe.log`: failures and warnings for diagnosis.

The report captures the exact encoder command, including local file paths. Do
not put credentials in input filenames. Review paths and media before sharing
artifacts; the tool uploads nothing.

## Clocked switching proof

`playout.py` decodes two local files into bounded queues and selects video/audio
on an independent 25 fps clock. One H.264/AAC encoder remains connected to an
isolated instance of the real Restreamer executable, which fans out to two
loopback RTMP capture listeners. The experiment uses fresh dummy credentials,
its own config and authenticated stage commands. It never opens a platform URL.

```text
local live file → decoder → bounded four-second buffer ┐
local BRB file  → decoder → bounded prepared queue      ├→ clock → encoder
hold picture / silence / emergency black               ┘             ↓
                                          isolated native relay → two local sinks
```

Build the helper and relay with `make check-continuous-benchmark`, or use the
container below. `--relay` and `--sinks` must be supplied together. Without them,
the proof writes a local capture and leaves delivery gates unmeasured.

```sh
python3 experiments/continuous-output/playout.py \
  --live /path/to/live.mp4 --brb /path/to/brb.mp4 \
  --output /path/to/new-clock-run --seconds 90 \
  --relay bin/continuous-bench-relay --sinks bin/continuous-bench-sinks
```

A repeating 60-second script selects manual BRB at 8–12 and 13.5–14.5 seconds,
pauses the live decoder at 20–28 and 50–58 seconds, marks incoming live audio
unavailable at 40–44 seconds, and makes fallback BRB unavailable at 54–58 seconds.
Buffered live drains before hold-last-picture with silence (up to one second),
BRB fallback and emergency black. The decoder resumes and live re-primes.
These are controlled decoder pauses/audio-availability faults; they do not test
real network-input EOF or supervised decoder/encoder replacement.

The first four seconds warm up the native output connections. Warm-up delivery
is reported separately; every subsequent measured frame must reach both sinks
with the correct source marker, frame ID and 40 ms timestamp cadence. Ordinary
switches and source outages must not reconnect either destination. Ready manual
commands must reach the sink within one second. All-frame selection-to-arrival
latency is also recorded, but is not the ready-command gate. The eight-second
live delay check currently starts at **decoded-frame arrival**; compressed-input
decoding time is excluded, so this does not establish full server-ingress delay.

For input-to-output A/V content checks, `prepare_markers.py` creates a separate
fixture with a one-frame flash and matching right-channel chirp before encoding:

```sh
python3 experiments/continuous-output/prepare_markers.py \
  --output /path/to/noise-markers.mp4
python3 experiments/continuous-output/prepare_markers.py \
  --artwork /path/to/workshop.mp4 --output /path/to/workshop-markers.mp4
```

Pass those fixtures to `playout.py --sync-markers`. Each observed flash must have
a matching decoded chirp within 80 ms. Fixtures use synthetic audio; this is not
an operation for replacing the audio of an original recording. Without the flag,
the proof preserves the left source channel and adds right-channel output-clock
chirps, leaving input A/V synchronization explicitly unmeasured.

`--target-fault` is a separate 20–180-second probe: the first sink deliberately
closes its first connection after 15 seconds. The healthy sink must receive all
measured frames without reconnecting; the affected sink must reconnect exactly
once, resume at a keyframe without replay, decode cleanly and reach the final
frame. A ten-second recovery-gap screen is an experimental sanity check, not a
production SLA. Run this probe separately from ordinary zero-reconnect evidence.

Clocked runs retain `selected-frames.jsonl`, per-connection FLV captures and packet
arrival logs, source hashes, encoder settings, CPU/RSS/queue samples, decoder
logs and a `report.json`. Evidence processing decodes the captures after playback;
allow additional time for it to finish. Two four-hour captures need roughly
30 GB plus preflight margin, source files and other workload storage.

## Remaining qualification work

A successful short proof is partial evidence. Production integration remains
gated on representative-media endurance and the complete confirmed contract:

1. Four-hour real-recording run on the intended VPS, with bounded memory/queues
   and whole-host CPU headroom; visual-quality review of the candidate profile.
2. True compressed-input ingress-to-egress delay, network input loss/reconnect,
   supervised encoder failure/recovery and explicit failure state.
3. Admitted and rejected asset-generation contention; immediate Stop with a
   buffered tail, normal ending drain and existing music-loop behavior.
4. Separate approval and evidence for the YouTube-continuous/Twitch-passthrough
   comparison. No claim that the unexplained green-screen incident is fixed.

Every report keeps `rollout_qualified: false` and names unmeasured gates. A short
run can complete with CPU unmeasured; it cannot qualify host capacity. The Mac
validates the harness, not the 4-vCPU VPS. A successful subprocess exit alone is
not approval to implement or deploy the production feature.

## Tests

```sh
REQUIRE_FFMPEG_TESTS=1 make check-continuous-benchmark
```

Tests exercise the CLI with real moving video/audio, decode its capture, verify
failure and interruption reporting, and ensure prior results cannot be replaced.
Real-media integration tests exercise both RTMP outputs, source switches, A/V
markers and independent destination recovery. The capture helper runs with the
Go race detector. CI also enables the 65-second Linux CPU-window integration.
Loopback socket access is required; no platform credentials are used.

## Isolated container option

The included Dockerfile adds Python and builds the loopback capture helper on
the released Restreamer media image. Capacity-only mode does not start Restreamer;
clocked RTMP mode starts a separate instance inside the isolated container. It pins the initial release under test; an explicit
`RESTREAMER_IMAGE` build argument can select a different release for a recorded
comparison. Record the resolved base-image digest with the result.

```sh
docker build -t restreamer-continuous-benchmark \
  -f experiments/continuous-output/Dockerfile .
# /path/to/benchmark-data must be writable by UID 65532 and contain recording.mp4.
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --cpus 3 --memory 2g --pids-limit 128 \
  --tmpfs /tmp:rw,noexec,nosuid,size=128m \
  -v /path/to/benchmark-data:/bench restreamer-continuous-benchmark \
  --input /bench/recording.mp4 --output /bench/run-90s --seconds 90
```

This initial screen budgets three CPU cores out of the intended host's four,
leaving one core available to its other services. The 75% acceptance limit still
applies to **total host CPU**, not just the container. A quota-limited or short
successful run alone cannot qualify the architecture; resource limits and other
host activity must accompany the evidence. The capture is retained in the bind
mount when the disposable container exits.

Run the clocked proof in the same isolation by overriding the entrypoint:

```sh
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --cpus 3 --memory 2g --pids-limit 128 \
  --tmpfs /tmp:rw,noexec,nosuid,size=128m --entrypoint python3 \
  -v /path/to/benchmark-data:/bench restreamer-continuous-benchmark \
  /opt/continuous-benchmark/playout.py \
  --live /bench/noise-markers.mp4 --brb /bench/workshop-markers.mp4 \
  --output /bench/clock-90s --seconds 90 --sync-markers \
  --relay /restreamer --sinks /opt/continuous-benchmark/rtmp-sinks
```

The original recording must remain untouched. Mount it read-only when supplied
separately; place any instrumented copies and results in benchmark storage.
