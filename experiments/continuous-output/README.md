# Continuous-output capacity screen

This is the first measurement tool for the [confirmed design](../../docs/design/continuous-output.md).
It does **not** implement continuous source selection or qualify production rollout.
The normal Restreamer executable and saved configuration are unchanged.

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

## Remaining benchmark work

A successful screen is necessary evidence, not the complete design gate. The
next proof must maintain an independent video/audio clock and test:

1. Bounded live-input buffering before selection; input/video/audio outages;
   hold-last-picture, fallback BRB, emergency media, and automatic recovery.
2. Ready source switches within one second, with output timing measured from
   decoded frame/audio markers; A/V content synchronization within 80 ms and
   added relay delay targeted at four seconds, bounded at eight.
3. Local RTMP destinations, independently failing sinks, connection counts,
   supervised encoder failure/recovery, and immediate Stop versus ending drain.
4. Admitted and rejected rendering contention, bounded queues and memory growth.
5. A complete four-hour run on the intended VPS, then the separately arranged
   platform comparison. Steady re-encoding for four hours alone is insufficient.

Media timestamps show cadence, not end-to-end latency or lip synchronization.
The Mac can validate the harness; it cannot establish the 4-vCPU VPS capacity.
No claim about fixing the unexplained YouTube green screen follows from this
screen. Results must be reconciled with the design before production integration.

## Tests

```sh
REQUIRE_FFMPEG_TESTS=1 python3 -m unittest discover \
  -s experiments/continuous-output -v
```

Tests exercise the CLI with real moving video/audio, decode its capture, verify
failure and interruption reporting, and ensure prior results cannot be replaced.

## Isolated container option

The included Dockerfile adds Python to the released Restreamer media image,
without starting Restreamer. It pins the initial release under test; an explicit
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
