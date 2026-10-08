# Restreamer

A native Go application that accepts one OBS stream over RTMP and forwards it to
Twitch, YouTube, and other RTMP/RTMPS destinations. Runs as a single executable in
a Docker container on Linux **amd64** and **arm64**. Live media stays native Go
passthrough. Optional BRB protection uses FFmpeg to prepare uploaded assets; the
container includes it. No Node, Python, GPU, or continuous transcoder is required.

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
  Live video is not decoded. BRB-enabled streams must match the selected profile.
- Encoded audio/video payloads and metadata are preserved. RTMP connections are
  independently negotiated; stream IDs and connection timestamps are rewritten.
- Each output connects independently, waits for the next keyframe, and receives
  cached metadata and codec headers before media. Audio preceding that starting
  keyframe is skipped to keep the new connection aligned.
- Failed outputs retry with exponential backoff (1–30 seconds, plus jitter).
  A slow output gets a bounded queue; overflow disconnects just that output and
  starts a fresh connection at a future keyframe. No disk spooling or stale backlog.
- JSON logs, HTTP liveness/status endpoints, and graceful SIGTERM shutdown.
- Password-protected dashboard with independent live target switches and rolling
  15-minute bitrate graphs for the input and every output. Switches persist in a
  Docker volume; changing them does not disconnect OBS or other destinations.\n- Authenticated input video preview and a master forwarding switch that starts\n  off on every application launch, independently of saved target switches.

There is no live transcoding, recording, public playback endpoint, audio-only mode,
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
Set both `DASHBOARD_USERNAME` and `DASHBOARD_PASSWORD` to enable the dashboard.
Both credentials are needed to operate the forwarding switch. Without them,\nRTMP input and health checks still run, but forwarding remains off.

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
- `DASHBOARD_USERNAME` and `DASHBOARD_PASSWORD`: set both for dashboard access.
- `STATUS_BIND_ADDRESS`: defaults to `127.0.0.1`; adjust for your proxy's network.

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

Include both the port and `/live` in OBS's **Server** field. The path must match
the configured `application`; the **Stream key** field must match the container's
`INGEST_STREAM_KEY`.

Input failures log a safe `stage`, `reason`, and `hint`. `application_mismatch`
means the server path is wrong or missing; `stream_key_mismatch` means the OBS key
differs from the running container's input key; `publisher_already_connected`
means another input session is still active. Protocol failures include the RTMP
stage and distinguish timeouts from closed connections. Peer-supplied URLs and
stream keys are never included in these diagnostics.

Open TCP port 1935 on the host/firewall for the OBS machine. `localhost` works when
OBS and Docker run on the same computer. Input RTMP is unencrypted, including its
key; use a trusted network or VPN for remote ingest. RTMPS outputs verify the
destination's certificate using the image's CA bundle.

The HTTP listener is bound to the Docker host's loopback interface by Compose:

```sh
curl http://127.0.0.1:8080/healthz
curl --user "$DASHBOARD_USERNAME" http://127.0.0.1:8080/status
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

## Dashboard and live controls

Open `http://127.0.0.1:8080/` on the Docker host (or your reverse proxy's HTTPS
URL) and sign in with `DASHBOARD_USERNAME` and `DASHBOARD_PASSWORD`. Authentication
uses HTTP Basic; all dashboard assets, `/status`, and `/api/*` require it.
`/healthz` remains public for Docker healthchecks. If both credentials are absent,
the dashboard and status API return 503 while RTMP input continues operating.\nForwarding stays off until enabled through the authenticated dashboard/API.

The application serves HTTP only. Configure your HTTPS reverse proxy to forward
to port 8080 and preserve the original `Host` and `Authorization` headers. Use a
dedicated hostname at `/` (subpath hosting is not supported). A proxy on the same
Docker network can use `http://restreamer:8080`; a host proxy can use
`http://127.0.0.1:8080`. If the proxy is on another machine, bind
`STATUS_BIND_ADDRESS` to a reachable private interface and allow that proxy through
your firewall. You do not need WebSocket support.

- **Forward to destinations** is the master switch. It starts **off on every
  process/container launch**, even when saved target switches are on. Turn it on
  to forward to enabled targets. Turning it off requires confirmation, closes all target connections, and
  cancels retries; OBS input, statistics, and preview continue. It never changes
  or persists over the individual target preferences. Reloading the page or
  reconnecting OBS does not reset it; restarting the application does.
- Switch a target **off**
 to close its connection and cancel retries. OBS and
  other outputs continue. Switch it **on** to connect again, send cached headers,
  and resume at the next live video keyframe. Nothing is buffered for replay.
- With master forwarding on and BRB enabled, offline OBS is replaced by BRB
  indefinitely. Without BRB, enabled targets remain **Ready** until input connects.
  With the master off, they show **Forwarding off**.
  A missing key or invalid server URL prevents enabling a target; update the
  container configuration and redeploy to fix it.
- Each card shows its connection state, attempts, and latest issue with a
  timestamp. A recovered target keeps its last issue visible for diagnosis.
  Errors are sanitized and never contain destination URLs or keys.
- Bitrate is sampled every second on the server and the page refreshes every two
  seconds. The graphs show a rolling 15-minute window, current Mbps, and peak
  bitrate. They count encoded audio/video bytes, excluding metadata and RTMP/TCP/TLS
  overhead. Short bursts and keyframes can produce normal fluctuations.
- History collects even with the page closed. Idle/disabled connections settle to
  zero on the next complete sampling interval. History and byte counters are held
  in memory and reset on process restart; only switches are persisted.

### Input preview

The input monitor and player share a row on desktop and stack on smaller screens.
The player starts muted and previews the original input even with master forwarding
or all destinations off. Use its audio/fullscreen controls or **Pause preview** to
stop downloading video without changing forwarding.

Preview uses authenticated `GET /api/preview`: Go repackages H.264/AAC-LC into
fragmented MP4; the browser decodes it through Media Source Extensions. No GPU,
FFmpeg, transcoding, extra port, or external player dependency is required. The
browser must support the input H.264 profile/resolution and AAC. Unsupported
codecs and playback failures are shown in the panel. Video-only input also works.
Playback starts at the next keyframe and reconnects after input changes; this is a
live monitor, not a recording or a 15-minute playback buffer.

Each open preview downloads the **original input bitrate**, including 1440p/4K;
showing it at half width does not reduce bandwidth. Up to eight simultaneous
viewers have independent bounded queues; slow viewers cannot block destinations.
The player keeps roughly 20–25 seconds of buffered media.

Disable response buffering and compression for `/api/preview` in your reverse
proxy and allow long-lived streaming responses. The server sends
`X-Accel-Buffering: no` for nginx. Keep forwarding `Host` and `Authorization`, and
protect this endpoint with the same HTTPS/authentication setup as the dashboard.

### FPS and frame counters


The **Bitrate / FPS** selector switches all graphs between the same rolling
15-minute histories. Current input FPS and FPS written to each target remain
visible alongside bitrate. FPS counts H.264 media packets per elapsed second
(one packet per video frame for standard OBS output), excluding configuration
headers, end markers, audio, and metadata. It measures arrival/write rate, so
network bursts can produce short spikes even for a constant-frame-rate source.
No decoding or GPU is required. **This observed packet-arrival FPS is not used
for BRB compatibility.** BRB compares the frame rate signalled in H.264 SPS/VUI
codec headers against the active saved profile; it does not infer it from these
graph samples.

Each destination also shows **Relay drops**, with expandable frame counters:

- **Written:** video packets successfully written to the target connection.
- **Relay drops:** video packets rejected by a full queue, abandoned in a failed
  connection's queue, not fully written, or omitted while an enabled destination
  is disconnected after an error. An in-flight failed write is counted once.
- **Omitted while paused:** frames arriving or discarded while that target is
  disabled or master forwarding is off; these do not increase relay drops.
- **Skipped:** initial connection setup, waiting for a keyframe, old-timestamp
  media during resynchronization, and buffered frames discarded on intentional
  cancellation or input-session shutdown. These do not increase relay drops.

Counters are cumulative across OBS sessions until the restreamer process restarts.
Frames still queued or being written have not yet received a final classification.
Input drop counts are unavailable: the relay cannot know which frames OBS never
sent. Successful writes are not acknowledgments of Twitch/YouTube playback.
OBS rendering/encoding statistics and platform-side drops are not measured.

Compose mounts the named `restreamer-state` volume at `/data`; target switches are
atomically saved to `/data/targets.json` before a control request succeeds. A write
failure is shown on the page and leaves the current switch unchanged. Keep the same
stack/Compose project and volume when redeploying. The target state file contains
only names and booleans, never stream keys. BRB assets and its profile also live
in this volume, without credentials. Saved switches override `TWITCH_ENABLED` /
`YOUTUBE_ENABLED` startup defaults for existing names. A missing key always disables
the target, even if the saved switch is on. Targets newly added to the configuration
use their configured defaults. Removing the state file while the container is
stopped resets all switches to configuration defaults on its next start.

For automation, `GET /api/dashboard` returns server time, status, and up to 900
samples (timestamps in Unix milliseconds). Existing `input` and `outputs` values
remain bitrates in bits per second; `input_fps` and `output_fps` add frames per
second. Status includes cumulative `input_frames`; target objects include
`video_frames_sent`, `dropped_frames`, `paused_frames`, and `skipped_frames`.
`GET /status` omits history. Both include `forwarding`, the current master switch.
Set it with `PUT /api/forwarding` and `{"enabled":true}` or
`{"enabled":false,"confirmed":true}`,
using the same authentication, JSON content type, and `X-Restreamer-Control: 1`
header as target controls. **Migration for API clients:** master-off requests
without `confirmed:true` now return 409 without changing the switch. Master
changes affect the running process only.
Set a target switch using authenticated JSON:

```sh
curl --user "$DASHBOARD_USERNAME" \
  -X PUT http://127.0.0.1:8080/api/targets/twitch \
  -H 'Content-Type: application/json' -H 'X-Restreamer-Control: 1' \
  --data '{"enabled":false}'
```

Curl prompts for the password. The control API rejects cross-origin browser
requests and requires the custom header above; successful updates return 204.

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
| `dashboard_username`, `dashboard_password` | Both required to enable dashboard and status API; omitted disables access |
| `state_file` | Optional path for saved switches; bundled config uses `TARGET_STATE_FILE`, set to `/data/targets.json` by Compose |
| `brb.enabled` | Opt-in for custom/native configurations; Compose defaults `BRB_ENABLED=false` |
| `brb.directory` | Required writable persistent directory when enabled; Compose uses `/data/brb` |
| `brb.width`, `brb.height` | Initial profile; bundled config uses 1920×1080. Even dimensions, 320×180 through 3840×2160 |
| `brb.fps` | Initial rate: 30; supports 24, 25, 30, 50, 60 |
| `brb.sample_rate` | Initial AAC-LC stereo rate: 48000; also supports 44100 |
| `targets` | 1–16 configured outputs, each with a unique `name` |
| `targets[].enabled` | Optional startup default, `true` if omitted; a saved/dashboard switch overrides this |
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

Requires Go 1.27 or later. Install FFmpeg with `libx264` and AAC support to run
BRB or its media integration tests. CI sets `REQUIRE_FFMPEG_TESTS=1` to ensure
these tests cannot silently skip. The protocol dependency is pinned in `go.mod`/`go.sum`.

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
uses a statically compiled Go executable, Alpine with FFmpeg, a CA bundle, an unprivileged user, and a
read-only filesystem. CI runs tests, cross-compiles both architectures, validates
both Compose configurations, smoke-tests the bundled configuration and container
health, and builds the container for both architectures. On `main`, the image job
publishes `main` and `sha-<full-commit-SHA>` tags to GHCR using the repository's
`GITHUB_TOKEN`; PRs build without publishing.

Tests cover authentication, target enable switches and missing keys, disabled
targets never connecting, single-publisher enforcement, two-destination fanout,
payload preservation, independent reconnection, keyframe/header recovery, queue
overflow, live stop/resume isolation, rapid toggles, persistent switches, dashboard
authentication and origin checks, bitrate sampling and 15-minute expiry, shutdown,
secret-safe status/config errors, RTMP interoperability with
the reference library, ping handling, and rejection of untrusted TLS certificates.
They use local endpoints and do not broadcast to real platform accounts.

## BRB and OBS disconnect protection

Enable BRB by setting `BRB_ENABLED=true` in `.env` and recreating the Compose
service. It defaults off so existing deployments retain their behavior. Custom
configurations need `brb.enabled=true`, a writable directory, and an initial
profile (see `config.example.json`). With BRB enabled, master forwarding **on starts broadcasting even
before OBS connects**. It sends the prepared BRB screen and audio to enabled
outputs. Master forwarding still starts off after every application restart.

- Lost or stalled OBS video/audio activates automatic BRB. Stalls are detected
  after three seconds; a closed connection activates BRB immediately. There is
  **no BRB timeout**. An OBS disconnect never ends the broadcast.
- Automatic BRB returns to OBS only after valid current codec headers, audio,
  and a fresh video keyframe arrive. Destination sessions stay connected and
  timestamps continue across reconnects, including OBS resetting its timestamps.
- **Manual BRB** overrides connected OBS and never expires. Turn it on before
  restarting your computer. Reconnecting OBS does not disable manual BRB.
  Turn it off to return to OBS at the next keyframe; if OBS is unavailable,
  automatic BRB continues indefinitely.
- **Master off**, with the dashboard confirmation, ends all outputs including
  BRB/music. Individual target switches still control their own destinations.
  Closing the dashboard, cancelling the dialog, or pressing Escape never stops
  a broadcast. Application shutdown or loss of the relay's own connectivity
  cannot be protected by BRB.

Expand **BRB screen, music & video profile** in the dashboard:

- Upload a PNG/JPEG image up to 10 MiB and 20 megapixels, or restore the built-in
  “Be right back” screen. Images fit inside the output without cropping.
- Upload optional MP3/WAV audio up to 32 MiB and 10 minutes, choose volume, or
  remove it. Music loops during BRB; without music, the relay sends silent AAC.
  Preparing and saving audio restarts the BRB loop if already active.
- Choose resolution, frame rate (including **25 and 30 fps**), and audio sample
  rate while master forwarding is **off**. **Selecting a value does not apply
  it: click Prepare & save BRB and wait for success.** The dashboard shows the
  active saved profile separately, flags unsaved changes, and highlights the save
  button. Configure/reconnect OBS to match the saved profile. Video must be 8-bit 4:2:0 H.264; audio must be AAC-LC stereo. Declared
  incompatible resolution, video timing, or audio headers reject that publisher
  with a visible BRB error while the prepared fallback stays available.
- **Prepare & save BRB** validates and encodes assets before atomically activating
  them. Failed uploads preserve the previous working assets. The saved dashboard
  profile overrides the initial JSON profile on restart. Assets/profile persist;
  manual BRB selection does not persist across application restarts.

FFmpeg runs only at preparation/startup. Go then loops bounded encoded tracks,
with no transcoding of OBS and no dependence on an FFmpeg process during a break.
Preparation may take up to two minutes and adds temporary CPU/memory use. Allow
43 MiB uploads and a three-minute request timeout in your reverse proxy. BRB's
prepared video/audio tracks are limited to 64 MiB each; upload processing is
serialized and bounded. Output queues remain independently bounded.

Input preview always shows OBS, including during manual BRB. The BRB image preview
shows the saved artwork. Destination bitrate/FPS and sent counters include BRB
media. Original OBS frames withheld during manual BRB count as skipped (or paused
when a target/master is off), not dropped. These counters do not acknowledge
platform playback. Test switching against your intended destinations before a
production broadcast; local tests exercise H.264 decoder changes and B-frames,
but cannot establish every platform's ingest behavior.

BRB compatibility diagnostics log each accepted video/audio codec header at
INFO and each rejection as `input incompatible with BRB` at WARN. The structured
fields are `media`, `source`, `compatible`, `ingest`, `brb`, `brb_profile`, and
`mismatches`. Each mismatch has a `field`, the decoded `ingest` value, and the
required `brb` value. The dashboard error also lists these differences. Expected
values come from the **active saved profile**, not unsaved dropdown selections or
an initial JSON profile superseded by saved settings.

For example, a 25 fps input against a saved 30 fps profile reports
`{"field":"fps","ingest":25,"brb":30}`. Video diagnostics include width, height,
luma/chroma bit depths, chroma format, NAL length size, SPS/PPS counts, and frame
rate. FPS comes from `h264_sps_vui`: `time_scale / (2 * num_units_in_tick)`.
Both raw timing numbers and `fixed_frame_rate_flag` are logged; the existing
comparison tolerance is 0.1 fps. This describes signalled encoder timing, not a
measurement of rendered frames or what the OBS settings UI currently displays.
Absent VUI timing is explicitly `not_signalled` and skips the FPS comparison;
zero timing values are invalid. It never substitutes the observed graph rate.

Audio diagnostics come from `aac_audio_specific_config`, including sample rate,
channel count, AAC object/extension types, frame-length and core-coder flags.
Malformed headers are identified without inventing unreadable values or logging
raw payloads/parser errors. Video and audio headers are logged separately; if a
publisher is rejected before the other header arrives, its properties cannot be
reported. Stream keys, credentials and arbitrary peer metadata are never logged.

Authenticated APIs: `PUT /api/brb` takes `{"enabled":true}` / `false` for manual
mode, with the same JSON/control-header requirements as other controls.
`POST /api/brb/assets` takes multipart fields `image`, `music`, `volume`,
`reset_image=true`, `remove_music=true`, and optional `width`, `height`, `fps`,
`sample_rate`. It requires `X-Restreamer-Control: 1` and rejects cross-origin
requests. Profile changes while forwarding is on return 409. `GET /api/brb/image`
returns the saved PNG. Status adds `brb`, `brb_assets`, and `brb_profile`.

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
BRB asset preparation already uses FFmpeg; this proposed live transcoder is not implemented.

References: [FFmpeg's streamcopy and transcoding model](https://ffmpeg.org/ffmpeg.html),
[scaling filters](https://ffmpeg.org/ffmpeg-filters.html),
[Twitch publish URLs](https://dev.twitch.tv/docs/video-broadcast/).
