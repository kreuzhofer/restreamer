# Restreamer

A native Go application that accepts one OBS stream over RTMP and forwards it to
Twitch, YouTube, and other RTMP/RTMPS destinations. Runs as a single executable in
a Docker container on Linux **amd64** and **arm64**. No Node, Python, FFmpeg, or GPU
is required for v1.

```text
                              ┌── RTMP/RTMPS ── Twitch
OBS ── RTMP :1935 ── Restreamer ┤
                              └── RTMP/RTMPS ── YouTube
```

## V1 behavior

- One active publisher, authenticated with a required input stream key.
- Standard RTMP **H.264 video and AAC audio**, including 1080p, 1440p, and 4K.
  The relay does not decode frames or impose a resolution/bitrate limit.
- Encoded audio/video payloads and metadata are preserved. RTMP connections are
  independently negotiated; stream IDs and connection timestamps are rewritten.
- Each output connects independently, waits for the next keyframe, and receives
  cached metadata and codec headers before media. Audio preceding that starting
  keyframe is skipped to keep the new connection aligned.
- Failed outputs retry with exponential backoff (1–30 seconds, plus jitter).
  A slow output gets a bounded queue; overflow disconnects just that output and
  starts a fresh connection at a future keyframe. No disk spooling or stale backlog.
- JSON logs, HTTP liveness/status endpoints, and graceful SIGTERM shutdown.

There is no transcoding, recording, playback endpoint, web UI, audio-only mode,
Enhanced RTMP, HEVC/AV1, or Twitch Enhanced Broadcasting support in v1. A publisher
using an unsupported codec is disconnected with an explanatory log entry.

**Passthrough does not make a stream compatible with a destination.** Every enabled
destination must accept the OBS resolution, bitrate, codec, frame rate, and keyframe
interval. For simultaneous Twitch/YouTube streaming, start with a compatible
1080p H.264/AAC OBS profile. A 4K source remains 4K on every output in v1.
See [YouTube's encoder settings](https://support.google.com/youtube/answer/2853702?hl=en)
and [Twitch's broadcasting guidelines](https://help.twitch.tv/s/article/broadcasting-guidelines?language=en_US).

## Run with Docker Compose

Requires Docker with the Compose plugin.

```sh
cp .env.example .env
cp config.example.json config.json
openssl rand -hex 24
```

Edit `.env`: set `INGEST_STREAM_KEY` to the generated value, set each platform's
stream key, and copy the Twitch server URL from OBS/Twitch into `TWITCH_SERVER`.
The YouTube RTMPS URL has a default. Server URLs contain the application path,
**without** the stream key; keys are sent separately and literally.

```sh
docker compose build
docker compose run --rm restreamer -config /etc/restreamer/config.json -check-config
docker compose up -d
docker compose logs -f restreamer
```

In OBS, choose **Settings → Stream → Service: Custom**:

| Setting | Value |
| --- | --- |
| Server | `rtmp://YOUR_SERVER_IP:1935/live` |
| Stream key | Your `INGEST_STREAM_KEY` |
| Video encoder | An H.264 encoder |
| Audio encoder | AAC |
| Keyframe interval | 2 seconds is a practical starting point; follow destination requirements |

Open TCP port 1935 on the host/firewall for the OBS machine. `localhost` works when
OBS and Docker run on the same computer. Input RTMP is unencrypted, including its
key; use a trusted network or VPN for remote ingest. RTMPS outputs verify the
destination's certificate using the image's CA bundle.

Status is bound to the Docker host's loopback interface by Compose:

```sh
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/status
docker compose down
```

`/healthz` means the process is running, even when OBS is offline or a target is
unavailable. `/status` reports `publishing`, each target's state, connection attempt
count, and media bytes sent (cumulative since process start). States are `idle`,
`connecting`, `waiting_for_keyframe`, `streaming`, and `retrying`; `streaming` means
media is being written, not that a platform has made the broadcast public.

## Configuration

`config.json` is loaded once at startup. Restart/recreate the container after edits;
when `.env` changes, use `docker compose up -d --force-recreate`.

| Field | Default / behavior |
| --- | --- |
| `listen` | `:1935` |
| `health_listen` | `:8080`; if changed, also update the port mapping and container healthcheck |
| `application` | `live`; letters, digits, `_`, `-` |
| `stream_key` | Required input key, 16–256 letters/digits/`_`/`-` |
| `queue_bytes` | 16 MiB per output; configurable from 1–256 MiB |
| `targets` | 1–16 outputs, each with unique `name`, server `url`, and `stream_key` |

String values support `$VARIABLE` and `${VARIABLE}` references. Unset or empty
references fail startup. Expansion happens after JSON decoding, so special
characters in environment secrets cannot change the JSON structure. Literal dollar
signs in keys are best supplied through environment values (expansion is not recursive).

Each output queue is also capped at 512 packets. Memory additionally includes
cached headers, one in-flight packet per output, and protocol buffers. The RTMP
library limits individual incoming messages to 10 MiB. Pending ingest connections
are capped at 16. Handshakes and output writes time out after 10 seconds; an input
without messages times out after 15 seconds. A second publisher is rejected while
the first session, including its output cleanup, is active.

Add destinations by adding entries to `targets` and corresponding environment
variables to Compose. To run only one platform, remove the other entry **and** its
required environment entries from Compose. The supplied Compose file intentionally
requires keys for both configured platforms. No platform keys are built into the
image, logged, or returned by `/status`. `.env` and `config.json` are Git-ignored.
Docker administrators can still inspect container environment variables.

The host needs outbound bandwidth for the sum of all forwarded streams. For
example, a 20 Mb/s OBS stream sent to two destinations uses roughly 40 Mb/s outbound,
plus protocol overhead. In v1, CPU handles transport/TLS, not video encoding.

## Build and test

Requires Go 1.27 or later. The protocol dependency is pinned in `go.mod`/`go.sum`.

```sh
make check              # race-enabled tests and go vet
make build              # bin/restreamer for your local machine
make cross-build        # static Linux amd64 and arm64 executables in dist/
```

Run locally with the configured environment variables exported:

```sh
./bin/restreamer -config config.json
```

Build a multi-platform OCI image archive without publishing:

```sh
docker buildx build --platform linux/amd64,linux/arm64 \
  -t restreamer:local --output type=oci,dest=restreamer.oci.tar .
```

Normal `docker compose up --build` builds for the host's architecture. The image
uses a statically compiled Go executable, a CA bundle, an unprivileged user, and a
read-only filesystem. CI runs tests, cross-compiles both architectures, validates
Compose, and builds the container for both architectures.

Tests cover authentication, single-publisher enforcement, two-destination fanout,
payload preservation, independent reconnection, keyframe/header recovery, queue
overflow, shutdown, secret-safe status/config errors, RTMP interoperability with
the reference library, ping handling, and rejection of untrusted TLS certificates.
They use local endpoints and do not broadcast to real platform accounts.

## Later: a separate 1080p Twitch output

Proposed v2 architecture:

```text
                              ┌── original encoded stream ── YouTube
OBS 1440p/4K ── Go relay ──────┤
                              └── decoder → scale → H.264 encoder ── Twitch
```

Go would supervise one native FFmpeg worker for the Twitch branch. The relay can
mux that branch into an FLV pipe, keeping the worker's input local and independent
of the YouTube connection. FFmpeg decodes the source video, scales it to fit a
1920×1080 canvas while preserving aspect ratio, and encodes a Twitch-compatible
H.264 stream. Copy AAC audio when it already meets the target requirements;
otherwise encode that branch's audio too. FFmpeg is native compiled software and
does not need Node or Python.

Start with a portable `libx264` CPU implementation on amd64 and arm64, then add
hardware profiles such as NVIDIA NVENC or Intel Quick Sync/VAAPI where the host
supports them. GPU support depends on the exact hardware, Linux drivers, FFmpeg
build, and container device access; an arm64 CPU alone does not guarantee an
available hardware encoder. Benchmark the actual machine with the intended
resolution and frame rate before choosing a preset or promising real-time 4K60.

The worker would get its own queue, process supervision, and reconnect policy.
Encoder overload would restart/resynchronize the Twitch branch without backing up
the YouTube branch. This adds CPU/GPU cost and latency only to the transcoded
branch. The next design inputs are the deployment CPU/GPU, 30 vs 60 fps, and whether
the source is SDR or HDR (HDR needs deliberate SDR tone mapping for an SDR output).
No transcoder is shipped or started in v1.

References: [FFmpeg's streamcopy and transcoding model](https://ffmpeg.org/ffmpeg.html),
[scaling filters](https://ffmpeg.org/ffmpeg-filters.html),
[Twitch publish URLs](https://dev.twitch.tv/docs/video-broadcast/).
