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
