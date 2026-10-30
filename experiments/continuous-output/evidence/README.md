# Preliminary VPS measurements — 2026-10-30

These are 90-second **single-encoder capacity screens**, not the four-hour
continuity qualification. The operator confirmed the intended Hostinger VPS
and authorized testing alongside its existing applications. No Restreamer
production process/container was running on this host. The operator selected
synthetic video and artwork first; representative OBS media remains pending.

Host: four virtual CPUs (AMD EPYC 9354P), 16 GB RAM. Existing applications
remained running. The isolated benchmark container had no network, no published
ports, a read-only root filesystem, and limits of three CPU cores and 2 GiB RAM.
The recorded CPU gate uses `/proc/stat` for the entire four-vCPU host.

Base media image:
`ghcr.io/kreuzhofer/restreamer@sha256:5c7770d34424950515c9abce6fb1e843d07a1615f77f77823243ff114f17b059`.
Python was added only to the experimental container image. No host packages,
application settings or credentials were changed.

| Input | Whole-host CPU, rolling minute windows | Frames decoded | Timestamp discontinuities | Progress lag growth |
| --- | --- | --- | --- | --- |
| Maker Workshop artwork + test tone | 28.30–31.36% | 2,250 | 0 | 0.679 s |
| 1080p25 moving test pattern + noise + test tone | 35.42–37.59% | 2,250 | 0 | 0.628 s |

Both used libx264 `veryfast`, three encoder threads, candidate 8 Mbit/s CBR,
two-second keyframes, and stereo AAC/48 kHz/160 kbit/s. All output decoded cleanly.
Both measured CPU and wall-clock gates passed. The JSON reports retain the input
SHA-256, full media build/settings, samples and unmeasured gates.

The artwork input was made from the repository's Maker Workshop BRB, scaled to
1080p25 with a synthetic 880 Hz tone. The busier fixture was generated with:

```sh
ffmpeg -f lavfi -i testsrc2=size=1920x1080:rate=25 \
  -f lavfi -i sine=frequency=880:sample_rate=48000 -t 12 \
  -filter_threads 1 -vf noise=alls=8:allf=t+u \
  -c:v libx264 -threads 2 -preset veryfast -b:v 12M -pix_fmt yuv420p -g 50 \
  -c:a aac -ac 2 moving-noise.mp4
```

Interpretation: these results support building and testing the independently
clocked switching proof. They do not yet establish its additional decoding,
selection, buffering or delivery costs, long-run behavior, A/V content sync,
source-outage continuity, rendering contention, or the production bitrate's
visual quality. Raw captures remain on the benchmark host. Neither report
qualifies rollout or explains the YouTube incident.


## Clocked switching proof: 90 seconds

The updated proof adds both source decoders, bounded queues, selection, one
persistent encoder and the actual native relay feeding two local RTMP sinks.
[Environment and source hashes](clocked-environment.json) identify the tested
image and code; [the complete report](clocked-90s.json) retains all samples.
The three-core quota, 2 GiB memory cap and network isolation remain in place.
The same VPS's other applications stayed running.

| Measurement | Result |
| --- | --- |
| Measured frames, each destination | 2,250 / 2,250 |
| Destination connections | 1 each; no ordinary reconnects |
| Decode errors / timestamp discontinuities | 0 / 0 |
| Ready-command arrival, maximum | 0.140 s (16 destination observations) |
| Selection-to-sink arrival, all frames, maximum | 0.937 s |
| Decoded-live-frame arrival to sink, maximum | 6.040 s |
| Input A/V marker error, maximum | 18.813 ms (166 observations) |
| Whole-host CPU, rolling minute windows | 54.24–58.38% |

Both destinations decoded the correct live, manual/fallback BRB, held picture
and emergency-black source markers throughout the scripted pauses and missing
audio. Fifty warm-up frames reached each sink before measurement; native outputs
joined at a keyframe, so the initial warm-up loss is deliberately visible rather
than counted as measured continuity. Twelve stale live-buffer frames were dropped
by the bounded queue during input recovery. The proof's Python process peaked
at about 531 MiB RSS; this short run does not establish a long-run memory bound.

An earlier probe reported a 1.26-second maximum across all selected frames and
was rejected by an overly broad one-second screen. The accepted contract applies
the one-second limit to ready manual commands. This version records those commands
explicitly, includes switches during busy live encoding, and reduces writer and
encoder input queues from eight frames to two. The earlier rejected result has
not been relabelled as passing; its raw evidence remains on the benchmark host.

These are synthetic/artwork fixtures with pre-encoded flash/chirp markers. Delay
starts after input decoding and excludes compressed-input decoder latency. This
is **not** representative-media endurance, full latency qualification, rendering
contention or production acceptance. The operator is uploading a real stream for
the next phase; the original recording will remain unchanged.


## Independent destination failure: 90 seconds

[The separate failure report](target-fault-90s.json) uses the same image and
fixtures. The first sink deliberately closes once; only that destination
reconnects. Its decoded prefix and recovered suffix contain no stale/replayed
frames, and it reaches measured frame 2,249 after a 2.845-second delivery gap.
The healthy sink retains its original connection and receives all 2,250 measured
frames. Its maximum ready-command response is 0.136 seconds, decoded-live arrival
delay is 5.846 seconds, and A/V marker error is 18.813 ms across 82 observations.
Whole-host rolling-minute CPU is 58.31–60.05%. Both affected connection captures
and the healthy output decode cleanly.

This demonstrates isolation of one forced local destination disconnect. It does
not exercise platform recovery, source-network failure, or encoder restart, and
does not change the remaining qualification gates.
