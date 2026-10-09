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
  Docker volume; changing them does not disconnect OBS or other destinations.
- Server-owned OFF, PRESTREAM, LIVE, BRB, CLIP, and ENDING stages, with separate
  OBS-input and broadcast previews. Every application launch starts OFF.
- Exact prepared-media selections, predictable clip returns, bounded ending
  delivery, and isolated preview-only rehearsals.

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
Both credentials are needed to operate broadcast stages. Without them,
RTMP input and health checks still run, but the broadcast remains OFF.

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
retry. With every output disabled, OBS input and the broadcast preview can
continue, but no destination receives media. Target states include `idle`,
`connecting`, `waiting_for_keyframe`, `streaming`, `retrying`, `draining`, and
`drained`; `streaming` means
media is being written, not that a platform has made the broadcast public.

## Dashboard and live controls

Open `http://127.0.0.1:8080/` on the Docker host (or your reverse proxy's HTTPS
URL) and sign in with `DASHBOARD_USERNAME` and `DASHBOARD_PASSWORD`. Authentication
uses HTTP Basic; all dashboard assets, `/status`, and `/api/*` require it.
`/healthz` remains public for Docker healthchecks. If both credentials are absent,
the dashboard and status API return 503 while RTMP input continues operating.
A broadcast starts only through the authenticated dashboard/API.

The application serves HTTP only. Configure your HTTPS reverse proxy to forward
to port 8080 and preserve the original `Host` and `Authorization` headers. Use a
dedicated hostname at `/` (subpath hosting is not supported). A proxy on the same
Docker network can use `http://restreamer:8080`; a host proxy can use
`http://127.0.0.1:8080`. If the proxy is on another machine, bind
`STATUS_BIND_ADDRESS` to a reachable private interface and allow that proxy through
your firewall. You do not need WebSocket support.

The stage controls express the intended phase of the show. The **on-air source**
shows the actual selected media; neither confirms public playback on a platform.
The server owns playback, pending transitions, and ending completion, so closing
all dashboard clients does not stop a sequence.

| Stage/action | Behavior |
| --- | --- |
| **OFF** | No destination delivery. OBS may remain connected and its input preview continues. Every process restart returns here. |
| **Prestream** | Validate the saved exact starting-soon revision and prepared BRB fallback, then loop it until an explicit stage change. OBS cannot interrupt it. |
| **Go live** | Keep the current source while waiting up to 10 seconds for a fresh compatible OBS keyframe. Show the pending request and allow cancellation. Timeout leaves the previous sequence intact. Ordinary LIVE works with BRB disabled. |
| **BRB** | Select a deliberate break. Reconnecting OBS does not leave it. A playing clip is suspended with its position, explicit pause state, and original return stage. |
| **Play clip** | Play the chosen exact ready revision once, then return to the recorded stage. Replacing clip A with B preserves A's original return destination. A clip interrupting PRESTREAM resumes its original revision near its saved position. |
| **Return / Stop clip** | Restore the recorded stage; Return from BRB resumes a suspended clip when one exists. An unavailable return source holds fallback BRB with an explanation. |
| **End stream** | Validate and play the saved ending once, then independently drain each connected destination for at most 10 seconds before selecting OFF. Show incomplete destinations, including those unavailable when draining begins. |
| **Stop now** | Confirm an immediate stop of the sequence, pending transitions, destination connections, and retries. OBS input and saved target/media selections remain available. |

Only **Prestream** and **Go live** can start from OFF. BRB, clips, and ending
require an active session; actions that need fallback reject missing/unprepared
BRB before changing anything. Re-selecting the current stage or current clip is a
no-op: it does not restart playback or reset a pending deadline. Use **Retry** to
recover failed prepared-media playback. A runtime media failure holds fallback
BRB with a persistent error and explicit recovery actions; it never exposes OBS
automatically. Established LIVE can recover OBS automatically after an outage.

Stage changes, selecting a different clip, Return, Retry, Stop clip, Stop now,
and media replacement require confirmation of their actual effect. Confirmation
is bound to the reviewed stage and media selection. Another client's change
requires fresh review; ordinary playback progress does not. A running ENDING
rejects ordinary stage changes with **Ending in progress**. Confirmed Stop now
is its early exit. Explicit ending-media replacement restarts that ending and
postpones shutdown; ENDING cannot be paused or scrubbed.

Choose **Preview only** when starting a rehearsal. It uses the same sequence and
broadcast preview but opens no destination connections, even if target switches
are enabled. **PREVIEW ONLY — NOT BROADCASTING** remains visible. Stop rehearsal
before starting a real session. For a real session, disabling the last destination
requires confirmation and retains the stage/preview with **NO DESTINATIONS — NOT
SENDING**. Re-enabling a destination requires confirmation that it joins the
current source. During ending playback, targets may be disabled but no additional
targets may be enabled; existing enabled targets can reconnect until final drain.
Final drain cancels pending connects and allows no new destination sessions.

- Switch a target off to close its connection and cancel its retries independently.
  Saved target preferences do not start a session by themselves. A missing key or
  invalid URL prevents enabling that target; update the configuration and redeploy.
- A healthy destination closes as soon as its queued and in-flight writes finish;
  it does not wait for a slow destination's deadline. Ending completion confirms
  **relay writes**, not platform playback or public availability.
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
The player starts muted and previews the original input even while the stage is
OFF, during rehearsal, or with all destinations off. Use its audio/fullscreen controls or **Pause preview** to
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
  disabled or destination delivery is off; these do not increase relay drops.
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
`GET /status` omits history. Both include `stage` and the effective `forwarding`
gate; `forwarding` is status, not an independent operator control.

### Shared stage API and migration

Use authenticated `GET /api/stage` for the stage, actual source, delivery mode,
server identity, confirmation context, pending transition, and command outcomes.
Submit JSON commands to `POST /api/stage/commands` with
`X-Restreamer-Control: 1`. Browser mutations must be same-origin. A command has:

```json
{
  "id": "a-new-unique-command-id",
  "server_id": "server_id-from-reviewed-stage-status",
  "context": "context-from-reviewed-stage-status",
  "action": "prestream",
  "confirmed": true,
  "mode": "preview_only",
  "revision": "exact-ready-revision-reviewed-by-the-operator"
}
```

Use a fresh command ID for a new deliberate action. Retrying the **identical**
request with the same ID reconciles a lost response without repeating its effect;
`GET /api/stage/commands/{id}` retrieves its correlated outcome. A reused ID with
different content is rejected. The server retains up to 256 command outcomes,
including an outstanding transition; older identities expire with their review
context and cannot be replayed. A missing outcome requires a fresh state review
and user action. After a server restart, refresh state and require
a fresh user action instead of replaying old commands. Stale confirmation contexts
are rejected. Completed requests return 200, pending transitions return 202, and
state/confirmation conflicts return 409 with an explanation. Do not turn
`confirmed:true` into an automatic response to a conflict.

The shared actions are `prestream`, `go_live`, `brb`, `return`, `play_clip`,
`end_stream`, `stop_now`, `retry`, `stop_clip`, `pause`, `resume`, `seek`,
`set_loop`, `cancel_pending`, `replace_now`, `replace_on_return`, and `set_target`.
Clip selection/replacement identifies `revision`; `seek` uses seconds in
`position`; `set_loop` uses `loop`; `set_target` uses `target` and `enabled`.
Starts select `mode: "real"` or `"preview_only"`. Direct clip pause, resume, and
seek do not create a different stage; protected actions still require review.

**API migration:** the former `PUT /api/forwarding`, `PUT /api/brb`,
`PUT /api/playback`, and `PUT /api/targets/{name}` mutation routes are removed.
Update automation to the shared command contract; none provides a bypass around
stage validation, confirmation, ending protection, or rehearsal isolation.
Media-management routes remain separate: use `PUT /api/library/prepare` with
`{"id":"library-entry-id"}` to prepare/retry an original, and the revision and
selection APIs documented below. `/healthz` remains public.

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
| `library_directory` | Persistent MP4 library root; Compose uses `/data/videos`, native default is `<brb.directory>/library` |
| `library_upload_mib` | Maximum original MP4 size in MiB; 0 or omitted means 4096; maximum 32768 |
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

Requires Go 1.27 or later and Node.js 24 for dashboard tests. Install FFmpeg/FFprobe with `libx264` and AAC support to run
BRB or its media integration tests. CI sets `REQUIRE_FFMPEG_TESTS=1` to ensure
these tests cannot silently skip. The protocol dependency is pinned in `go.mod`/`go.sum`.

```sh
make check              # race-enabled tests, dashboard tests, and go vet
make build              # bin/restreamer for your local machine
make cross-build        # static Linux amd64 and arm64 executables in dist/
```

Implementation is complete after the final commit is pushed and its GitHub CI
run succeeds. Releases on `main` also require successful image publication.
Follow the [delivery completion procedure](docs/agents/delivery.md) and include
the exact commit SHA and successful CI run link when reporting completion.

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
profile (see `config.example.json`). Enabling BRB prepares fallback media; it does
not start delivery. Start an explicitly confirmed Prestream or Go live session.
The broadcast still starts OFF after every application restart.

- Established LIVE falls back to BRB when OBS disconnects or its video/audio
  stalls for three seconds. Fallback has no automatic timeout.
- LIVE recovers only after compatible current headers, audio, and a fresh video
  keyframe arrive. Destination sessions stay connected and timestamps continue
  across OBS reconnects, including OBS resetting its timestamps.
- Deliberate BRB remains selected until an explicit action leaves it. Use Return
  for the recorded stage or suspended clip; reconnecting OBS does not leave BRB.
- Confirmed Stop now ends all outputs including BRB/music. Closing the dashboard,
  cancelling a dialog, or pressing Escape never stops a broadcast. Application
  shutdown or loss of the relay's own connectivity cannot be protected by BRB.

Expand **BRB screen & music** in the dashboard:

- The default is a **32-second pixel-art arcade loop**: ghosts chase Pac-Man,
  a power pellet reverses the chase, frightened ghosts scatter and returning
  eyes cross the screen, then another chase leads back into the opening.
  “BE RIGHT BACK” stays readable in the centre. There are no game sound effects.
  The dashboard shows a labelled still preview of this animation.
- Upload a PNG/JPEG image up to 10 MiB and 20 megapixels for a static screen,
  or select **Remove the custom image and use the BRB message** and
  **Prepare & save BRB** to restore the message screen. Select **Restore legacy
  arcade/custom-image styling** as well to return from a shared theme to the
  original animation. Images fit inside the output without cropping.
  Existing default-screen installations receive the animation on restart;
  custom images and music are preserved.
  To change sprites, colours or choreography, use the editable Canvas source and
  browser preview described in [the artwork guide](artwork/brb/README.md).
- Edit **BRB message**, then click **Prepare & save BRB** to change the text
  and its still preview. With legacy styling, the message stays on one centred line and automatically
  shrinks in whole-pixel steps to fit clear of the animation. Up to 40 characters
  are supported: A–Z, 0–9, ÄÖÜß, spaces and `. , ! ? : ' - / ( ) + &`.
  Lowercase letters are displayed in uppercase. Unsupported or empty text is
  rejected without replacing the active BRB. The message persists across
  restarts and can be changed while forwarding. It appears in legacy animation
  or the selected shared theme when no custom image is selected. Shared themes
  validate typography and overflow without shrinking the text.
- Upload optional MP3/WAV audio up to 32 MiB and 10 minutes, choose volume, or
  remove it. Music loops during BRB; without music, the relay sends silent AAC.
  Preparing and saving audio restarts the BRB loop if already active.
- **Prepare & save BRB** validates and encodes assets before atomically activating
  them. Failed uploads preserve the previous working assets. The saved dashboard
  profile overrides the initial JSON profile on restart. Assets/profile persist;
  deliberate BRB selection does not persist across application restarts.

Expand **Shared style theme** to select an exact built-in or named theme revision
from the same library used by prestream and ending. Customize named variants in
**Media generator**, then explicitly refresh the BRB choices. A theme edit never
changes the current BRB or an already-prepared candidate. Save any message,
custom-image, or music edits first; theme preparation captures those saved inputs.

**Prepare theme preview** creates a separate candidate. **Preview exact BRB**
plays its encoded video and audio; **Activate prepared BRB** asks for confirmation
before replacing the current fallback. If BRB is on air, activation restarts its
video and music. Candidate activation checks the captured base generation and
profile. Later BRB settings/profile changes make it stale: preview remains
available, but a new preparation is required before activation. Cancelling a
preparation or discarding a candidate leaves the current BRB unchanged. The
existing `/api/brb/assets` prepare-and-save contract remains available.

Shared-theme BRB uses the same embedded typography, images/logos, palette,
spacing, borders, and decorative effect renderer as authored scenes. It supports
up to 1920 × 1080 at 30 fps with a four-second visual loop. The complete uploaded
music track retains its own independent loop and selected volume; it is never
trimmed to four seconds. The exact preview shows the first four seconds. Existing
legacy styling keeps its original profile support and 32-second arcade or
two-second custom-image loop. Restore legacy styling before choosing a profile
outside the measured shared-theme limits.

Preparation shares the existing media workload gate and has a two-minute timeout
including waiting. Retention is bounded to one active generation and one prepared
candidate, plus one temporary in-flight preparation. Encoded video and audio are
each bounded to 64 MiB; the remuxed candidate preview is bounded to 128 MiB. Existing
10 MiB image/20-megapixel and 32 MiB/10-minute music upload limits still apply.
A successful new candidate replaces the previous candidate; cancellation,
discard, and validated startup remove unused preparation directories. Preview
requests open their own files, so replacing a candidate does not truncate an
already-open preview. Generation directories and candidate metadata are owned by
the server beneath the BRB directory.

Named-theme BRB restarts from its exact retained tracks and captured style before
the authoring catalog loads; it does not silently re-render or adopt a newer
theme. Prepared candidates also persist. Legacy startup still refreshes its
arcade assets, so candidates based on a prior legacy generation become stale.
Theme image revisions remain protected from deletion, with active/prepared BRB
uses visible in the asset catalog. Missing or invalid retained media is reported
explicitly. Authenticated same-origin routes are `/api/brb/theme/prepare`,
`/api/brb/theme/candidate` (read/discard), `/api/brb/theme/candidate/preview`, and
`/api/brb/theme/activate`.

The **Shared streaming profile** in the **OBS input** card controls resolution,
frame rate (including **25 and 30 fps**), and audio sample rate for OBS, BRB,
and prepared videos. Select **OFF** before changing it, including ending rehearsal.
The form shows the active saved profile and keeps edits as an unsaved draft.
**Save & rebuild** warns that saving rebuilds BRB and re-prepares the video
library from retained originals. **Cancel changes** restores every saved value.
Returning all fields to their saved values also clears the warning and disables
saving; no media is rebuilt. Saving BRB artwork/music never applies a profile draft.

The new profile becomes active only after BRB preparation and persistence succeed;
failures preserve the previous working profile and library. Videos are then
prepared in the background, with progress in the library and playback available
individually when ready. Configure/reconnect OBS to match the saved values—these
controls do not configure OBS. Video must be 8-bit 4:2:0 H.264; audio must be
AAC-LC stereo. Incompatible resolution, video timing, or audio headers reject
that publisher with a visible error while the prepared fallback stays available.

FFmpeg runs only at preparation/startup. Go then loops bounded encoded tracks,
with no transcoding of OBS and no dependence on an FFmpeg process during a break.
Preparation may take up to two minutes and adds temporary CPU/memory use. Allow
43 MiB uploads and a three-minute request timeout in your reverse proxy. BRB's
prepared video/audio tracks are limited to 64 MiB each; upload processing is
serialized and bounded. Output queues remain independently bounded.

Input preview always shows OBS, including during deliberate BRB. The BRB image preview
shows the saved artwork. Destination bitrate/FPS and sent counters include BRB
media. Original OBS frames withheld during deliberate BRB count as skipped (or paused
when a target or destination delivery is off), not dropped. These counters do not acknowledge
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

Deliberate BRB is selected through the shared stage command API.
`POST /api/brb/assets` takes multipart fields `image`, `music`, `volume`, `text`,
`reset_image=true`, `remove_music=true`, and optional `width`, `height`, `fps`,
`sample_rate`. It requires `X-Restreamer-Control: 1` and rejects cross-origin
requests. Profile changes require OFF and return 409 during an active session. `GET /api/brb/image`
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

## Prepared video library

Enable BRB to use its saved output profile and persistent broadcast controller.
The **Video library** section accepts MP4 uploads and discovers regular `.mp4`
files in `<library_directory>/originals` every five seconds. In Compose this is
`/data/videos/originals` inside the existing persistent `/data` volume. Mount a
host folder there if you want to add files directly. Copy external files under a
temporary extension and rename them to `.mp4` when complete; unchanged size and
modification time over successive scans are used to detect finished copies.
Hidden files, subdirectories and symlinks are ignored. Filenames are unique;
uploading the same name never overwrites an original.

The library shows discovery, queued, preparation progress, ready and failed
states. One background worker converts each original to the **active saved**
resolution, frame rate, H.264 video and AAC-LC stereo profile. Silent audio is
added to files without audio. Aspect ratio is preserved with letterboxing;
rotation is handled by FFmpeg. **Prepare again** retries a failed conversion or
rebuilds a prepared copy. Originals are retained, and successful conversions are
reused after restart. Playback reads bounded packets from disk instead of loading
entire videos into memory. Node/browser runtimes are not needed on the server;
FFmpeg and FFprobe are required (both are in the container image).

Save an exact ready revision as starting-soon media, ending media, or a named
clip shortcut. There can be multiple shortcuts. Selections survive restart, but
active playback and pending transitions do not. Changed content under the same
filename cannot silently replace a selected revision.

Start Prestream or Go live, then choose **Play clip**. A shortcut plays once and
returns to its recorded stage. Detailed CLIP controls provide pause, resume,
seek, explicit looping, and Stop clip. Pause holds the position with fallback
BRB on air; seeking resumes from a preceding keyframe within about one second,
and seeking while paused keeps it paused. PRESTREAM owns its loop; ENDING is
play-once and cannot be paused or scrubbed.

Preparation is separate from activation. A ready candidate can be previewed
without changing the show. **Replace now** requires confirmation of the exact
revision, starts it at the beginning, and preserves the current stage, return
stage, loop setting, and destination sessions. A paused clip remains paused at
the beginning. Replacing an ending explicitly restarts it and postpones shutdown.
**Replace on return** changes suspended media without interrupting the current
source; that revision starts from the beginning on return. Without explicit
replacement, active/suspended playback retains its original revision and position.
Saving different configured selections applies on subsequent use and does not
replace media already on air. Failed preparation leaves playback unchanged;
completed preparation never activates itself, even if every client closed or
the operator has since left that stage.

**Broadcast preview** uses the same encoded feed sent to destinations, including
OBS, files and BRB. Its position display follows the visible file when the browser
is playing the current preview, otherwise it shows the server position. Seeks
and source changes reconnect the preview to discard stale buffered frames.
Browser preview controls affect only that browser, not the broadcast. Use a
browser supporting Media Source Extensions with H.264/AAC; platform buffering
can add a separate delay for remote viewers. The original OBS preview is retained.

Profile changes require OFF, including ending a preview-only rehearsal. Saving a different resolution, FPS or
sample rate cancels obsolete preparation, marks the library queued, and prepares
from the originals. Older incompatible conversions cannot be selected for
playback. BRB artwork, message or music changes do not reconvert library files.
Immutable revisions retain the content needed by saved selections and active or
suspended playback; newly prepared content does not retarget those references.

Uploads stream to temporary disk files and publish atomically; interrupted or
oversized uploads do not enter the library. Defaults/limits: one simultaneous
upload, 4 GiB per original (configurable), 1000 files, source duration 0.1 seconds
to 12 hours, source dimensions at most 8192 × 8192, and 32 GiB per prepared copy.
Preparation has a 12-hour deadline and is cancelled on shutdown. Allow disk space
for originals plus conversions and temporary files. Configure your reverse proxy
for your chosen upload size, up to a two-hour upload timeout, and unbuffered
long-lived `/api/broadcast-preview` responses. Conversion happens asynchronously
after upload, so the upload request does not wait for transcoding.

Authenticated APIs (mutations require `X-Restreamer-Control: 1` and same origin):

- `GET /api/library`: originals, preparation progress, exact revisions, readiness,
  duration, profile, and saved selections.
- `POST /api/library/upload`: multipart with exactly one `file` MP4; returns 202.
- `PUT /api/library/prepare`: JSON `{"id":"library-entry-id"}`; returns 202 and
  performs preparation on the server, independently of client connections.
- `GET /api/library/revisions/{revision}/preview`: finite authenticated MP4
  preview of that exact ready candidate, without selecting it on air.
- `GET /api/stage-media` and `PUT /api/stage-media`: persist exact selections as
  `{"prestream":"revision-id","ending":"revision-id","shortcuts":[{"name":"Intro","revision":"revision-id"}]}`.
  Use an empty prestream/ending string to clear it. Up to 32 uniquely named clip
  shortcuts can be saved. Validation or persistence failures retain old settings.
- `POST /api/stage/commands`: stage selection and CLIP playback/replacement controls
  through the shared confirmed command contract above.
- `GET /api/broadcast-preview`: streaming fragmented MP4, using the same bounded
  viewer limit as the input preview. `/status` and `/api/dashboard` include
  `library_enabled` and `playback` state, source, position and preview timeline.

### Reusable content templates

In **Media generator**, choose a blank design, the editable welcome/topics/links
prestream starter, the thanks/follow-up-links ending starter, or a saved content
template. Select the style theme separately. Every starter scene can be edited,
reordered, duplicated, or removed using the normal scene editor.

**Save as new template** captures the current draft content explicitly.
**Update selected template** replaces only that reusable template; draft autosave
never does this. Copies are independent: changes to one show or the template do
not change another show, a captured generation job, or prepared media. Templates
preserve all supported scene fields and overrides, but do not select a theme.
Concurrent updates fail visibly and retain local work; reload the latest template
as a new design, or save local work as a new template.

Up to 200 user templates persist beneath `<library_directory>/generator/templates`.
Template requests allow 65 KiB, including a 1 KiB envelope around the existing
64 KiB design limit. The authenticated JSON API is
`/api/generator/templates` (list/create), `/api/generator/templates/{id}`
(read/versioned update), and `/api/generator/templates/{id}/designs`
(create an independent design from an exact template version and selected theme).
Built-in starters are immutable sources; their copied scenes are fully editable.

### Versioned style themes

The generator's **Style theme library** offers Retro revision 1 (the original
static appearance) and Retro revision 2 (pixel corners and an animated pixel
trail). Duplicate either into a named theme. Supported settings include Go Sans
or Go Mono, 24–120 point type at 1080p, three palette colors, exact background
and logo image revisions, logo corner and size, content dimensions, line/list
spacing, borders, and a deterministic four-square decorative trail. Logo boxes
must not overlap text regions; validation reports collisions and typography
that no longer fits. Images keep their proportions and transparency.

**Publish new revision** retains every previous revision. Designs select an
exact theme revision separately from content templates. **Apply updated theme**
changes only that reference: scene order, text, timing, soundtrack and supported
scene overrides remain intact. Review layout validation and generate again to
create new output. Editing a theme never changes another draft, a captured job,
or selected/on-air media. Quick preview's effect time shows the same integer
step positions used by generated video, restarting per scene.

Theme revisions are stored under `<library_directory>/generator/themes`, with a
limit of 200 custom revisions. Referenced image revisions remain protected from
deletion and appear in asset uses. Jobs capture the full resolved theme alongside
the pinned design and streaming profile. Theme images are verified and decoded
sequentially, then bounded to the output dimensions (logos to 20% width and 10%
height); animation uses one small sprite input, never a frame cache or a separate
browser/Node runtime. Generation remains sequential with bounded scene files.

### Media generator drafts

Open **Media generator** from the dashboard to create independent prestream or
ending show designs. Add, remove, reorder or independently duplicate title and
list scenes. Each defaults to 10 seconds and selects the built-in Retro theme
revision independently of its
text and timing. Drafts autosave beneath `<library_directory>/generator/designs`
(or the default BRB library directory). Wait for **Saved** before closing the tab.
A failed save retains local edits and offers Retry; a concurrent-tab conflict
requires Reload saved draft or Save local work as a copy. Neither draft saves nor
quick previews change broadcast media.

Quick previews use embedded Go Sans and Go Mono scalable fonts, with no system
fonts or new runtime executables. The same Go raster renderer and glyph metrics
are available to prepared-media generation. Font size is specified at 1080p and
scales with the active streaming profile. Latin text including European accents,
Greek and Cyrillic are supported; missing glyphs (including unsupported emoji)
and text overflow produce field errors. Text wraps at spaces and explicit line
breaks, preserves mixed case, and is never automatically shrunk or truncated.
Clear the font-size field to inherit the selected theme's default.

Draft input is limited to 64 KiB per request, 4096 UTF-8 bytes of combined title
and item text per scene, 180 bytes per design name, and 200 saved designs. A
composition has 1–20 scenes; list scenes have 1–20 separately editable items,
each preserving explicit line breaks. Empty compositions can be saved for
correction but cannot be previewed or generated. Each scene supports font and
size overrides, left/center/right alignment, and a centered content region
with width and height from 30–90% of the frame (blank inherits 80%). Layouts
never permit unrestricted pixel positioning. Validation identifies the scene
and list item; choose its error to open it. A valid scene can still be previewed
while a different scene needs correction.
Durations must be finite, greater than zero, and at most 3600 seconds; font sizes
range from 24 to 120 at 1080p. These are bounded authoring limits, not rendering
performance promises. Drafts retain invalid durations/unsupported glyphs for
correction; preview validation uses the current streaming profile (up to 4K).
Only one quick preview or validation runs at a time per server. Preview requires
the existing dashboard authentication and same-origin control header, and uses
untrusted text strictly as raster data.

### Generate and review exact media

Choose **Generate saved draft** after autosave finishes. The server captures that
acknowledged draft version, its exact theme revision and active streaming profile
as an immutable design revision. Closing the browser does not stop generation;
editing the draft creates newer work without changing the captured input. The
editor shows queued/running progress, cancellation, failures and retained ready
results separately from the editable draft, saved stage selections and on-air
media. **Preview exact revision** plays the actual prepared H.264/AAC content.
Generation and preview never select a stage or replace media already playing.

The renderer reuses the quick-preview Go PNG raster and bundled Go fonts, then
runs the existing FFmpeg dependency with libx264 and AAC. No browser runtime,
Node, CGO or system fonts are needed. Native builds require `ffmpeg` and `ffprobe`
on PATH; Alpine containers already install them. Both Linux architectures use
the same static Go renderer and packaged FFmpeg. Third-party font and module
licenses are included in `THIRD_PARTY_NOTICES`.

Generation accepts up to 20 scenes and at most eight outstanding jobs
across prestream and ending, including the running/cancelling job. Jobs run one at
a time in persisted admission order. A full queue rejects new work explicitly;
cancel queued work or wait for completion to free a slot. The shared queue shows
every design's state, queue position, progress and cancellation controls. At most
200 job records are retained, including retries; reaching that history limit
rejects further work without removing existing records or media. Output is
limited to 512 MiB per job, with a 15-minute preparation-wait/render/validation
deadline. A scene raster uses at most 1920 × 1080 pixels; the temporary workspace
holds at most 512 MiB of normalized scene segments and PCM audio plus a raster during
encoding, then at most 512 MiB of final output (under 1 GiB combined). Only one
scene is normalized at a time; a concat demuxer joins independent bodies and
transition pieces. A crossfade decodes at most two normalized scenes; composition
never repeatedly encodes a growing prefix. All pieces and source/final PCM share
the same workspace cap. Source audio and silence are assembled on cumulative frame boundaries, and
one continuous AAC track is encoded for the complete sequence, avoiding per-scene audio priming gaps. The workspace is removed
on success, cancellation or failure. Prepared revisions remain retained in the
existing library. Keep enough persistent disk space for retained revisions;
200 maximum-size generated outputs can occupy 100 GiB before library media.

Supported generation profiles are the active even-sized 320 × 180 through
1920 × 1080 profile at 24, 25 or 30 fps, with stereo 44.1 or 48 kHz audio. Higher
profiles remain available for other media and quick previews, but generation
rejects them explicitly instead of silently downscaling. Generation supports up
to 600 seconds of scene durations before overlaps; each scene duration rounds independently to the nearest
complete video frame and must contain at least one frame. With cuts, total
duration is the sum of those rounded scene durations. Both the editor and the
generated result display that total (for example, two 1.02-second scenes at
25 fps become two 26-frame scenes, totaling 2.08 seconds). Complete frame counts and audiovisual timing are checked before a
revision becomes ready, including detection of output-limit truncation.

These bounds are admission limits, not throughput or resource guarantees. Native
FFmpeg 4.4 probes at 1080p30 measured a 600-second still in 27.6 seconds with about
436 MiB peak memory, and 20 seconds of moving test media in 1.4 seconds with about
390 MiB, using the ultrafast preset. Production uses veryfast; timings differ by
hardware, version and workload. A 20-input simultaneous scene probe used about
848 MiB even for a short still sequence; the 20-scene admission cap therefore
uses sequential normalization rather than simultaneous inputs. Encoding uses two threads and filter processing
one thread. This bounds concurrency inside the process, not system-wide CPU or
memory use. Generator rendering, ordinary library preparation and BRB encoding
share one cancellable preparation slot. A waiting generator remains running with
an explicit waiting explanation; the library also displays its wait. Cancelling
a waiting generator removes its request without interrupting another conversion.
BRB's existing two-minute preparation deadline and library's twelve-hour deadline
include waiting. Live delivery never acquires the preparation slot. This limits
expensive preparation concurrency, not aggregate retained storage or the CPU and
memory used by live sessions and previews. Native 320 × 180/25 fps regression
measurements with real generation, concurrent library conversion and a local RTMP
broadcast observed one FFmpeg process (about 50 MiB peak sampled RSS). A healthy
destination continued delivery with no new drops or reconnects while another
destination disconnected. These fixture measurements do not promise production
throughput or memory limits; deployment and larger-profile loads still vary.

Job snapshots and outcomes persist beneath `<library_directory>/generator/jobs`;
a restart marks unfinished jobs interrupted and removes incomplete workspace
files. **Retry captured revision** explicitly creates a new job at the back of
the queue using the interrupted, failed or cancelled job's immutable inputs,
profile, renderer and duration. It does not use a newer editable draft or alter
the previous outcome. To use newer edits, choose Generate saved draft instead.
If a server update removes the captured renderer version, its retry is rejected
explicitly; generating the current saved draft is a new job and may include newer
edits, not an equivalent replay of the old result.
Missing dependencies,
storage failures, cancellation and profile changes leave prior ready and on-air
revisions intact. A profile change during generation fails that job rather than
substituting a different profile. Authenticated APIs expose jobs at
`/api/generator/jobs`, individual outcomes at `/api/generator/jobs/{id}`, and
cancellation at `/api/generator/jobs/{id}/cancel`, and explicit captured-input
retry at `/api/generator/jobs/{id}/retry` (POST with an empty JSON object).

### Reusable images and logos

The generator's **Reusable media** library accepts PNG and JPEG files up to
10 MiB and 20 megapixels, matching the existing BRB image input bounds. Uploads
are decoded only after checking their format and dimensions, then normalized to
PNG with transparency preserved. Normalized images are limited to 32 MiB, with
200 total retained asset revisions per server. Image-only storage uses at most
6.25 GiB; video revisions can increase this to 100 GiB.
One upload runs at a time and shares the media preparation slot; waiting has a
one-minute deadline. Exact image viewing shares the eight preview-reader slots.
These are storage and admission limits, not throughput guarantees.

Use **Text with image** for text and an image in two columns inside the centered
content region, or **Full-screen media** for a proportional full-frame image.
Images are contained without cropping or distortion; transparent areas use the
scene background. Setting custom content dimensions insets full-screen media.
Text layouts retain typography and alignment controls; media layouts show only
the applicable image, duration and content-region controls.

Each upload creates an independent asset regardless of filename. Choose an
existing asset as the upload destination to append an immutable revision;
concurrent replacements require reloading its latest version. Existing scenes
keep their exact selected revision. Choose another revision, or **Use latest
image revision**, to adopt an update explicitly. Quick preview and generation
resolve the same pinned bytes and verify their stored digest; changed or missing
files produce errors instead of substitution.

The media library lists uses in saved designs, content templates, captured jobs,
prepared results,
saved stage selections, on-air media and suspended return media. Deletion removes
an entire unused asset and all its revisions; any reference blocks deletion.
Failed and cancelled jobs retain their captured image inputs for explicit retry.
Switching a scene's layout preserves its selected image; choose **No image
selected** to remove that reference. Refresh the media library to see changes
made in another tab. Assets and metadata persist under
`<library_directory>/generator/assets`; temporary uploads are removed on failure
or recovered at restart. Existing BRB uploads remain independent of this library
until shared themes are introduced.

### Reusable video scenes

Choose **Video** in **Reusable media** to upload MP4 files with one H.264 video
track and optional mono/stereo AAC audio at 8–48 kHz. Sources may be up to
512 MiB, 1920 × 1080, 60 fps and 600 seconds, with a minimum duration of 0.04
seconds. Extra tracks and unsupported codecs are rejected. Uploads share the
preparation slot, decode before acceptance, and have a 15-minute deadline.
These bounds limit resource use; conversion time still depends on the source.

In a **Full-screen media** scene choose **Video** and a source revision. Selecting
a new source starts the scene at its source duration with audio muted. Set trim
start/end to select a range (zero end means the source end), then set scene
duration. Shorter scenes stop early; longer scenes require **Repeat selected
range**. Only that range repeats. Enable source audio explicitly and adjust its
independent volume from 0–100%. Enabling audio on a silent source is a field
error. Video is contained within the content region without cropping.

Quick preview shows the first selected source frame in the composition. **Play
selected range** previews source timing, repetition and audio; **Preview exact
revision** plays the generated composition with exact frame timing. Generation
normalizes one source at a time at the captured profile, assembles continuous
PCM audio, and encodes AAC once for the complete sequence.

Video assets follow the same immutable revision, explicit adoption, reuse,
template, retry and deletion protections as images. Switching media kind or
layout retains inactive references and settings. Choose **No video selected** to
remove a reference. Replacing an asset never changes captured jobs or outputs.

### Continuous background soundtrack

Upload reusable **Music** assets as MP3 or PCM WAV: up to 32 MiB, one mono/stereo
track at 8–192 kHz, and 0.04–600 seconds of decoded audio. Files with extra tracks
(including embedded video artwork) are rejected explicitly. Upload preparation
normalizes audio to stereo 48 kHz signed 16-bit PCM WAV, with exact sample counts;
it performs no additional lossy encoding. A revision holds at most 115,200,044
bytes. Audio shares the 200-revision asset limit, immutable replacement/adoption,
usage reporting and deletion protection with images and video. Uploads share the
preparation slot and have a five-minute deadline; staged input plus normalized
PCM/WAV requires at most about 252 MiB before publication.

Select one **Continuous soundtrack** per design. Music starts at the beginning
and never restarts at scene boundaries. **End at track end** stops at the earlier
track/design end, with silence afterwards. **Repeat to design end** repeats the
whole track until the design ends; it does not change scene duration. Repetition
does not promise a seamless musical join between a track's tail and head.
Music volume is independent of each video's source-audio controls; video remains
muted by default. Zero music volume retains the selected revision.

Fade-in starts at the design beginning. Fade-out finishes at the actual audible
track end in End mode, or at the design end in Repeat mode. Fades round to audio
samples and must fit the audible interval without overlapping; zero disables a
fade. Quick scene preview remains visual; generate and play the exact prepared
output to review the complete audio mix.

The renderer assembles continuous scene PCM, resamples/repeats the soundtrack,
and mixes them using bounded streaming buffers. When the combined peak exceeds
full scale, it applies one fixed gain to the entire mix, preserving relative
source levels. The job's `mix_gain` and displayed **Whole-mix gain** disclose this
adjustment; 100% means requested levels were retained. There is no adaptive
normalization or lookahead delay. AAC is encoded once after the complete mix.
PCM intermediates share the existing 512 MiB workspace cap; the final output has
its separate 512 MiB cap. Container/AAC packet padding retains the existing
preview timing tolerance; scene timing comes from video frames.

Soundtrack revision, mode, level and fades are saved in designs and templates,
and captured immutably by jobs and retries. New uploads or draft edits never
change an existing generated revision, stage selection, or on-air result.


### Scene transitions

Choose **Cut** or **Crossfade** to the next scene in the generator. An omitted
transition remains a cut for existing designs. The setting belongs to the
outgoing scene and follows scene reorder/duplicate/template copies. The last
scene retains its outgoing setting for later reordering but does not apply it.
A prestream loop boundary is configured separately when available.

A crossfade overlaps the outgoing tail and incoming head, including enabled
source-video audio. Music continues once over the resulting sequence. Durations
round to the nearest video frame; a crossfade needs at least one frame. Incoming
and outgoing overlaps together must fit their shared scene, preventing three-way
blends. A scene may consist entirely of its two overlaps. A one-frame crossfade
has no intermediate video blend frame; use a longer overlap for a visible fade.

The editor and captured job show the total after subtracting overlaps. For
example, 0.8-, 0.6- and 1-second scenes with two 0.2-second crossfades produce
2 seconds at 25 fps. Quick preview shows the selected individual scene and explains
its transition; generate and play the exact output to inspect motion and audio.
Invalid overlap edits remain saved with field errors, while generation preserves
previous ready output. Source audio is combined on cumulative sample boundaries
before continuous music and the single final AAC encode.

A native FFmpeg 4.4 measurement at 1080p30 with two-second title scenes and
0.4-second overlaps took 2.40 seconds for 3 scenes and 16.58 seconds for 20 scenes.
The 20-scene run produced 32.4 seconds of output, with one preparation process,
at most two FFmpeg inputs, sampled peak encoder RSS 178 MiB and workspace 7.9 MiB.
These synthetic still-scene measurements are not deployment performance guarantees;
video, themes and longer scenes can use more memory, storage and time.
