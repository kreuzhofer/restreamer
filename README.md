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
- Each output defaults to enabled, but an empty/missing target stream key disables
  it. Set its optional `enabled` switch to `false` to disable it while keeping the key.
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
openssl rand -hex 24
```

Edit `.env`: set `INGEST_STREAM_KEY` to the generated value and set the stream key
for each platform you want to use. Leave a target key empty to disable that output,
or set `TWITCH_ENABLED=false` / `YOUTUBE_ENABLED=false` to disable it while retaining
its key. Both switches default to `true` when omitted. Twitch and YouTube server
URLs have defaults; override them with your platform's ingest URL if needed.
Server URLs contain the application path,
**without** the stream key; keys are sent separately and literally.

```sh
docker compose pull
docker compose run --rm restreamer -config /etc/restreamer/config.json -check-config
docker compose up -d
docker compose logs -f restreamer
```

The default stack pulls `ghcr.io/kreuzhofer/restreamer:main`, published by GitHub
Actions after tests pass, with both Linux amd64 and arm64 variants. It does not
build on the deployment host or require a `config.json` file. The image includes
`config.example.json`; its environment placeholders are resolved at startup.
Set `RESTREAMER_IMAGE` to a published `sha-<full-commit-SHA>` tag or digest to pin a
particular build.

### Portainer (standalone Docker)

Create/update a stack from this Git repository, branch `main`, Compose path
`docker-compose.yml`. Alternatively, paste that file into the stack editor.
Set these in the stack's **Environment variables** section:

- `INGEST_STREAM_KEY`: your own random key, at least 16 characters.
- `TWITCH_SERVER`: optional Twitch server URL, without the key; defaults to `rtmp://live.twitch.tv/app`.
- `TWITCH_STREAM_KEY`: your Twitch key; missing or empty disables Twitch.
- `TWITCH_ENABLED`: optional `true`/`false`, defaults to `true`.
- `YOUTUBE_STREAM_KEY`: your YouTube key; missing or empty disables YouTube.
- `YOUTUBE_ENABLED`: optional `true`/`false`, defaults to `true`.
- `YOUTUBE_SERVER`: optional; defaults to `rtmps://a.rtmps.youtube.com/live2`.

Deploy after the repository's **CI / image** job has succeeded. When updating an
existing stack, fetch the latest repository content (or replace the editor content)
and pull the image again. The default stack has no `build:` section or host bind
mounts, so it also works when Portainer manages Docker through an agent.

If GHCR returns `unauthorized` or `denied`, add a `ghcr.io` registry in Portainer
with your GitHub username and a personal access token (classic) with
`read:packages`, then make it available to the Docker environment. GitHub initially
creates container packages as private, even for public repositories. If you want
anonymous pulls instead, make the `restreamer` package public in its GitHub package
settings; repository visibility alone does not do that.
See [GitHub's registry authentication documentation](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).

The error `listing workers ... http2: frame too large ... HTTP/1.1 header` indicates
that the build client reached an incompatible HTTP endpoint. It has been reported
with [Portainer stack builds](https://github.com/portainer/portainer/issues/10562).
Using the prebuilt image bypasses that build connection. Do not include
`docker-compose.build.yml` in the Portainer stack.

### Build locally instead

From a local checkout with `.env` configured:

```sh
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

This explicit override adds `build: .` and uses `restreamer:local`. It is intended
for the Docker CLI on a host with a working local builder.

## OBS setup

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
count, effective `enabled` flag, and media bytes sent (cumulative since process start).
Disabled outputs report `enabled: false`, state `disabled`, and never connect or
retry. If every output is disabled, the service still accepts OBS but discards
the stream; nothing is broadcast. States for active targets are `idle`,
`connecting`, `waiting_for_keyframe`, `streaming`, and `retrying`; `streaming` means
media is being written, not that a platform has made the broadcast public.

## Configuration

The bundled configuration is loaded once at startup. When environment variables
change, recreate the container (`docker compose up -d --force-recreate`, or update
the stack in Portainer).

For custom destinations or listener settings, copy `config.example.json` to
`config.json`, edit it, and mount it read-only over `/etc/restreamer/config.json`.
For a Portainer stack, use an absolute path to an existing file on the **Docker
host**, which may be a different machine from Portainer itself. The default
two-destination configuration requires no mount.

| Field | Default / behavior |
| --- | --- |
| `listen` | `:1935` |
| `health_listen` | `:8080`; if changed, also update the port mapping and container healthcheck |
| `application` | `live`; letters, digits, `_`, `-` |
| `stream_key` | Required input key, 16–256 letters/digits/`_`/`-` |
| `queue_bytes` | 16 MiB per output; configurable from 1–256 MiB |
| `targets` | 1–16 configured outputs, each with a unique `name` |
| `targets[].enabled` | Optional boolean, defaults to `true`; `false` disables the output even with a key |
| `targets[].stream_key` | Missing, empty, or whitespace-only disables the output regardless of `enabled` |
| `targets[].url` | RTMP/RTMPS server URL, required only for active outputs |

String values support `$VARIABLE` and `${VARIABLE}` references. Unset or empty
references fail startup for required input settings and active target URLs.
A missing/empty environment variable referenced in a target key disables that
target. `enabled` accepts a JSON boolean (`true`/`false`) or a string referencing an
environment variable, such as `"${TWITCH_ENABLED}"`; an unset/empty switch defaults
to `true`, and any other non-boolean value fails validation. Expansion happens after JSON decoding, so special
characters in environment secrets cannot change the JSON structure. Literal dollar
signs in keys are best supplied through environment values (expansion is not recursive).

Each output queue is also capped at 512 packets. Memory additionally includes
cached headers, one in-flight packet per output, and protocol buffers. The RTMP
library limits individual incoming messages to 10 MiB. Pending ingest connections
are capped at 16. Handshakes and output writes time out after 10 seconds; an input
without messages times out after 15 seconds. A second publisher is rejected while
the first session, including its output cleanup, is active.

Add destinations in your custom configuration by adding entries to `targets` and
corresponding environment variables to Compose. To run only one platform, leave
the other platform's key empty or set its enable switch to `false`; no entries need
to be removed. Only the input key is mandatory in the supplied Compose stack.
No platform keys are built into the
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

For local binary use, first copy `config.example.json` to `config.json`.

Build a multi-platform OCI image archive without publishing:

```sh
docker buildx build --platform linux/amd64,linux/arm64 \
  -t restreamer:local --output type=oci,dest=restreamer.oci.tar .
```

The explicit local build override builds for the host's architecture. The image
uses a statically compiled Go executable, a CA bundle, an unprivileged user, and a
read-only filesystem. CI runs tests, cross-compiles both architectures, validates
both Compose configurations, smoke-tests the bundled configuration and container
health, and builds the container for both architectures. On `main`, the image job
publishes `main` and `sha-<full-commit-SHA>` tags to GHCR using the repository's
`GITHUB_TOKEN`; PRs build without publishing.

Tests cover authentication, target enable switches and missing keys, disabled
targets never connecting, single-publisher enforcement, two-destination fanout,
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
