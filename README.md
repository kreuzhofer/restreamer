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
- Optional [Stream Deck plugin](integrations/streamdeck/README.md) with broadcast
  controls, independent clip keys, protected Stop now, and relay delivery status.

There is no live transcoding, recording, public playback endpoint, audio-only mode,
Enhanced RTMP, HEVC/AV1, or Twitch Enhanced Broadcasting support in v1. A publisher
using an unsupported codec is disconnected with an explanatory log entry.

**Passthrough does not make a stream compatible with a destination.** Every enabled
destination must accept the OBS resolution, bitrate, codec, frame rate, and keyframe
interval. For simultaneous Twitch/YouTube streaming, start with a compatible
1080p H.264/AAC OBS profile. A 4K source remains 4K on every output in v1.
See [YouTube's encoder settings](https://support.google.com/youtube/answer/2853702?hl=en)
and [Twitch's broadcasting guidelines](https://help.twitch.tv/s/article/broadcasting-guidelines?language=en_US).

## Whole animation loops in scenes

In the design editor, select a scene and choose **Scene length → Animation loops**,
then enter a whole number of loops. The editor shows the theme cycle and calculated
render length: Arcade After Hours and Neon Night use 16 seconds, Maker Workshop
Starting soon uses 12 seconds, and Pac-Man Maze uses 40 seconds. Custom variants
follow their selected artwork and effect. Static themes and Maker Workshop's
one-shot Ending animation use **Seconds**. BRB already uses its complete theme cycle.

Crossfade overlap is added to loop-based scene lengths so it does not shorten the
requested cycles. To align an entire repeating sequence, use Animation loops for
all scenes. Existing seconds-based designs keep their durations; newly generated
videos now continue background motion across scene boundaries. Existing generated
videos stay unchanged and must be regenerated to receive this fix. Music-driven
playback continues to loop its visual sequence independently of the music duration.

## Run with Docker Compose

Requires Docker with the Compose plugin. Existing installations using environment-based
stream keys or destinations should follow the [managed-credential migration](#managing-destinations-in-desktop-settings)
before replacing their configuration.

```sh
cp .env.example .env
```

Edit `.env` to set dashboard credentials. The ingestion key is generated
and persisted automatically; copy it from desktop **Settings → OBS ingestion**.
Configure platform server URLs and keys in desktop **Settings → Destinations**;
enable delivery in Live. Server URLs contain the application path without the key.
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

Ingestion keys are generated automatically, and destination credentials are
configured in desktop Settings. Configure these environment settings:

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
| Stream key | Copy from desktop Settings → OBS ingestion |
| Video encoder | An H.264 encoder |
| Audio encoder | AAC |
| Keyframe interval | 2 seconds is a practical starting point; follow destination requirements |

Include both the port and `/live` in OBS's **Server** field. The path must match
the configured `application`; copy the current key from desktop **Settings → OBS ingestion**
into the **Stream key** field.

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

Use **Live** (`/live`) to operate the broadcast, **Show setup** (`/setup`) to
choose upcoming videos, **Library** (`/library`) to manage reusable resources,
and **Settings** (`/settings`) for ingestion, destinations and the streaming
profile. Existing `/studio` and `/generator?id=…` bookmarks still open their
corresponding content. Navigation never starts or stops a broadcast.

Show setup presents **Starting soon**, **BRB** and **Ending** together. Select
**Change**, preview the exact ready video, then use it for that stage. Each
choice shows its name, date, music and playback behavior; older videos are
available on demand. Updating a selection affects next use. Current and
suspended playback change only through a separately reviewed replacement in Live.

Open a design by its name in Show setup or Library. The dedicated editor keeps
content, theme, music, draft preview and generated videos together for all three
stage types. Generate captures saved inputs; it never selects a result or changes
on-air media. Save status, background progress, errors and retry remain visible.
Name a video to make variants recognizable; technical revision IDs are under
Technical details. Older videos and maintenance actions open on demand.

Library contains **Designs**, **Music**, **Uploads** and **Themes**. Tracks and
playlists appear as lists; search filters designs, tracks and uploads. Create a
playlist or upload a missing asset from the editor, then return to the same draft.
Templates are available from Designs and as starting points for a new design.
Theme and playlist updates require explicit adoption; existing generated videos
remain unchanged. Select multiple designs or generated videos for cleanup;
partial failures identify protected items for retry. Deleting a design removes
its generated files, while uploaded originals remain separately managed.

On phones, Live is a broadcast remote: **Operate** keeps the outgoing preview,
stages, Return/Stop and pinned clips together. **Signals & destinations** opens
readiness, delivery switches, OBS preview and diagnostics on demand. Show setup
and Library support browsing, exact previews and selecting ready videos. Editing,
uploads, generation, theme changes and technical configuration require desktop.
Preview pause controls affect browser traffic only, never the broadcast.

Desktop uses a persistent sidebar, local underlined tabs and a prominent main
action per task. The Layout controls retain resizable panels and reset options;
preferences apply only to this browser. Narrow windows use panel views. Focus
outlines, keyboard controls and explicit save/departure messages remain available.

Twitch destination creation and editing fetch available servers from Twitch's
unauthenticated ingest API. Choose the global/default or a regional server;
manual URL entry remains available when discovery fails. Discovery never sends
the stream key. An existing destination's address changes only after selecting a
server and saving it.

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
requires fresh review; ordinary playback progress does not. A running **finite** ENDING
rejects ordinary stage changes with **Ending in progress**. Confirmed Stop now
is its early exit. Explicit ending-media replacement restarts that ending and
postpones shutdown; finite ENDING cannot be paused or scrubbed. A **Follow music**
ending loops independently and permits explicit stage changes; it never drains or
stops at EOF. Use confirmed Stop now to finish.

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
  invalid URL prevents enabling that target; correct it in desktop **Settings → Destinations**.
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
On compatible iPhone browsers (iOS 17.1 or later), the player uses Apple's
ManagedMediaSource API when standard MediaSource is unavailable. AirPlay is
disabled for these streamed monitors because they have no separate remote-playback
source. If automatic playback is blocked, tap Play in the player. Browsers without
either media-source API show an explicit unsupported-browser message.
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

Compose mounts the named `restreamer-state` volume at `/data`. Managed destinations
and enabled preferences are saved atomically in the private credentials file.
Legacy switches use `/data/targets.json`, which contains only names and booleans.
A write failure leaves the running settings unchanged. Keep the same volume when
redeploying; a missing key always disables delivery.

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

For custom listener settings, copy `config.example.json` to
`config.json`, edit it, and mount it read-only over `/etc/restreamer/config.json`.
For a Portainer stack, use an absolute path to an existing file on the **Docker
host**, which may be a different machine from Portainer itself. The default
configuration requires no mount; manage destinations in Settings.

| Field | Default / behavior |
| --- | --- |
| `listen` | `:1935` |
| `health_listen` | `:8080`; if changed, also update the port mapping and container healthcheck |
| `application` | `live`; letters, digits, `_`, `-` |
| `ingestion_key_file` | Private managed ingestion credential; bundled path `/data/ingestion-key.json`. First startup imports a legacy key or generates one; later starts retain the stored key |
| `stream_key` | Optional one-time import for managed ingestion; required only without `ingestion_key_file`. Legacy keys use 16–256 letters/digits/`_`/`-` |
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
| `destinations_file` | Private persistent managed destinations; bundled path `/data/destinations.json` |
| `targets` | 0–16 legacy outputs available for explicit import; empty for new installations |
| `targets[].enabled` | Optional startup default, `true` if omitted; a saved/dashboard switch overrides this |
| `targets[].stream_key` | Missing, empty, or whitespace-only disables the output regardless of `enabled` |
| `targets[].url` | RTMP/RTMPS server URL, required only for active outputs |

String values support `$VARIABLE` and `${VARIABLE}` references. Unset or empty
references fail startup for required legacy input settings and active target URLs.
With managed ingestion, an absent legacy `stream_key` generates a key only when
the managed credential file does not yet exist.
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

Add destinations and replace platform keys in desktop Settings. No destination
credentials are required in `.env`. Legacy target JSON remains supported for
explicit migration; see Managing destinations below.
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

Open **Show setup → BRB** or a BRB design in Library. Edit the message, choose
its theme and music, then **Generate video**. Draft preview shows appearance;
the Videos view plays the exact animation and soundtrack. Preview and use the
result explicitly. BRB designs and their generated history persist independently.

The selected BRB video can also serve as automatic fallback. Under **When a
source is unavailable**, choose **Use selected BRB** or an explicit different
video. An existing separately configured legacy fallback is retained until you
change this choice; opening the redesigned app does not replace it. Selecting
next-use media never replaces an active or suspended fallback.

If managed fallback fails, an independently prepared silent emergency screen
keeps a visible source when available. The error stays visible until explicit
recovery; the failed presentation is not restarted in a loop. If emergency media
is also unavailable, the dashboard reports output failure. Current and suspended
fallback media, as well as selected media, are protected from cleanup.

**Pac-Man Maze** is a selectable 40-second theme with ghost regeneration and a
continuous chase. Other themes and named variants live under **Library → Themes**.
Shared changes do not alter an existing video's appearance. BRB supports a message
of up to 40 characters; unsupported text and overflow are rejected explicitly.
Music supports none, one uploaded track, or a playlist. Follow music downloads
one music pass; broadcast BRB loops until the operator leaves it.

The legacy `/api/brb/assets` prepare-and-save and candidate APIs remain available
for compatibility. Their immediate-activation behavior is not the ordinary
editor workflow. Existing custom images and music remain in retained legacy
fallback storage. See [UX migration and rollback](docs/design/ux-migration.md)
before deploying or downgrading persisted state.

Shared-theme BRB uses the same embedded typography, images/logos, palette,
spacing, borders, and decorative effect renderer as authored scenes. It supports
up to 1920 × 1080 at 30 fps. Plain styles use a four-second visual loop; illustrated
themes retain their full motion cycle (16 seconds for Arcade After Hours/Neon Night,
12 seconds for Workshop, and 40 seconds for Pac-Man Maze). The complete uploaded
music track retains its own independent loop and selected volume. The exact preview
shows one full visual cycle. Existing
legacy styling keeps its original profile support and 32-second arcade or
two-second custom-image loop. Restore legacy styling before choosing a profile
outside the measured shared-theme limits.

Preparation shares the existing media workload gate and has a two-minute timeout
including waiting. Retention is bounded to one active generation and one prepared
candidate, plus one temporary in-flight preparation. Encoded video and audio are
each bounded to 64 MiB; the remuxed candidate preview is bounded to 128 MiB. Existing
10 MiB image/20-megapixel and 32 MiB/10-minute music upload limits still apply.
Copies of normalized stored images/music are bounded to 192 MiB, allowing a
20-megapixel RGBA64 PNG (160 MB of pixels plus framing). Oversized, growing, or
shrinking sources fail explicitly and partial copies are removed; they are never
silently truncated.
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
uses visible in the asset catalog, including in-flight preparation references. Missing or invalid retained media is reported
explicitly. Authenticated same-origin routes are `/api/brb/theme/prepare`,
`/api/brb/theme/candidate` (read/discard), `/api/brb/theme/candidate/preview`, and
`/api/brb/theme/activate`. Preparation status is available at
`GET /api/brb/theme/preparation`; cancel with
`POST /api/brb/theme/preparation/cancel` and its exact `id`. Prepare keeps its
synchronous `201` candidate response; optional `async: true` returns a `202`
preparation record. Only one preparation and one latest status record are retained.

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
**Studio → Media** accepts MP4 uploads on desktop and discovers regular `.mp4`
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

In **Live → Browse prepared media**, preview an exact ready revision and save it
as starting-soon media, ending media, or a named pinned clip shortcut. Closing
the drawer keeps selection edits; save or discard them explicitly. There can be multiple shortcuts. Selections survive restart, but
active playback and pending transitions do not. Changed content under the same
filename cannot silently replace a selected revision.

Start Prestream or Go live, then trigger a pinned clip or choose **Play once**
in the prepared-media drawer. A shortcut plays once and returns to its recorded
stage. The contextual **Clip playback** panel provides pause, resume,
seek, explicit looping, and Stop clip. Pause holds the position with fallback
BRB on air; seeking resumes from a preceding keyframe within about one second,
and seeking while paused keeps it paused. PRESTREAM owns its loop; fixed-duration ENDING is
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

In **Studio → Designs**, choose a blank design, the editable welcome/topics/links
prestream starter, the thanks/follow-up-links ending starter, or a saved content
template. Select the style theme separately. Every starter scene can be edited,
reordered, duplicated, or removed using the normal scene editor.

In **Studio → Templates**, browse built-in starters and custom templates, then
use **Use template** to choose a name and separate theme for an independent design.
The collection also shows the open design as the source for explicit save/update.
**Save as new template** captures the current draft content explicitly.
**Update selected template** replaces only that reusable template; saving a design
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

The generator's **Style theme library** offers **Arcade After Hours · revision 2** (the illustrated arcade city,
vivid stars, chasing lights, cabinet highlights and animated canal reflections),
**Pac-Man Maze · revision 1** (the detailed maze and continuous 40-second chase),
and **Neon Night · revision 2** (a neon skyline, rooftop cat, shimmering river,
changing window lights, cyan/magenta trims and vivid stars).
Revision 2 of both illustrated themes keeps the 120-point title lettering and
uses a separate 64-point default for list scenes, so the agenda and follow-up
starters fit without manual changes. For existing revision-1 designs, click
**Apply updated theme**, then regenerate. Revision 1 remains resolvable for saved designs but is retired from new selections; already
prepared media is unchanged. Explicit scene font sizes still override the theme.
The theme editor exposes **List font size**; leaving it blank inherits the
normal default. Whitespace-only titles render empty, and validation identifies
the affected scene and list item rather than leaving stale errors while editing.
Duplicate any built-in into a named theme. Supported settings include Go Sans,
Go Mono or the embedded uppercase Arcade Pixel alphabet, 24–120 point type at 1080p, three palette colors, an optional pixel text edge color, exact background
and logo image revisions, logo corner and size, content dimensions and vertical
offset, line/list
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
height). Retro animation uses a small sprite; Arcade After Hours and Neon Night decode
embedded background masters. Neither needs a frame cache or browser/Node runtime. Generation remains sequential with bounded scene files.

Both illustrated themes keep all text editable. Their default large headline uses a
quiet upper-center region; use explicit line breaks for a two-line title and a
smaller scene font size for lists or longer copy. The pixel alphabet intentionally
renders lowercase as uppercase and supports A–Z, digits, German umlauts, ß and
basic punctuation. Other glyphs produce a field error; choose a Go font for
broader language coverage. Text color, accent/shadow color, font, logo and spacing
remain editable in a duplicated theme. The illustration has its own fixed colors;
background color does not recolor it. Clear built-in artwork before choosing an
uploaded background, and select **none** to freeze decorative animation.

The palette-style background repeats every **16 seconds** and restarts per scene.
For a seamless prestream use scene durations in multiples of 16 seconds, or scene
and end-to-start crossfades. BRB prepares the full 16-second loop and retains its
independent music duration. Select either illustrated theme in **BRB → Shared theme**,
prepare, preview, then explicitly activate it. Existing designs, prepared media
and Retro revisions do not change automatically. The source, production edit
prompts and rebuild instructions are in [Arcade After Hours](artwork/arcade-after-hours/README.md)
and [Neon Night](artwork/neon-night/README.md).
Neon Night uses pale aqua lettering with magenta edges and purple shadows;
its artwork and animation remain independent of Arcade After Hours.

### Theme previews and OBS frames

In **Studio → Themes**, choose a current theme and switch between **OBS frame**, **Starting soon**, **Agenda**, **BRB**, and **Ending**. Download PNG exports at 1920×1080 on desktop or phone. Scene samples use example text and the current editor settings without publishing a revision. Each built-in artwork has a dedicated thin OBS frame with a transparent center; add it as an Image source in OBS. Frame artwork has fixed colors and excludes custom logos. Custom styles without built-in artwork do not have a frame.

The catalog offers Pac-Man Maze, Arcade After Hours revision 2, Neon Night revision 2, and Maker Workshop normal, Christmas and Halloween. Retro and superseded Arcade/Neon revision 1 are retired from selectors. Saved designs and retained media keep their exact old revisions; no generated or uploaded files are deleted by this change.

### Maker Workshop and seasonal sets

**Maker Workshop**, **Maker Workshop · Halloween**, and **Maker Workshop · Christmas**
are shared revision-1 themes for BRB, prestream, and ending. Each includes its own
stage-specific animated pixel-art master: BRB prints and assembles a waving robot;
prestream opens on the welcoming mascot; ending waves goodbye and settles the
machinery before the configured final fade. Halloween adds witch hats and a
floating ghost; Christmas adds Santa hats, scarves and moving parcels.

Choose a theme in **Studio → BRB** and prepare, preview and activate it. For a
show design, choose the **Maker workshop · starting soon** or **Maker workshop ·
signing off** content starter, then select any of the three themes separately.
Headlines, agenda items, links, colors and supported overrides remain editable.
The sign has a compact content region and a smaller list font; divide longer
agendas into additional scenes. Content height can now be 12–90%, width 30–90%,
and vertical offset −40–40%, provided the region stays inside the frame.

BRB and prestream use complete **12-second** cycles. The supplied prestream
starter uses 12-second scenes; other durations or crossfades remain available.
Ending holds its settled final frame if a scene exceeds the master duration, then
the existing ending fade applies. An inset video can keep playing over that held
background. Ending motion continues across scene changes on one timeline; BRB
and prestream scenes start their own cycles. The editable sign follows the
contained artwork on non-widescreen profiles.

The nine masters are embedded and require only the existing FFmpeg runtime.
Authoring uses Blender; production does not. Pixel artwork uses nearest-neighbor
scaling in motion and frozen previews. See [source, prompts and rebuild checks](artwork/maker-workshop/README.md).

### Deleting authored media

Each saved show design has a **Delete** button. After confirmation, deletion
removes the draft, its generation history (including queued work), and generated
video files that are not shared with another saved result. **Delete result** in
generation history removes one result without deleting its editable design.
Wait for running generation to finish or cancel it before deleting.

Uploaded images, videos and music are **never automatically deleted with a
design**. Manage them separately under **Studio → Media → Scene assets**: expand an asset to see
its references, then use **Delete unused image/video/audio** to remove the upload
and all its revisions from disk. Remove its uses in designs, templates, themes,
generation history or BRB before deleting it.

**Manage saved templates** deletes custom content templates independently of
designs made from them. The style library's **Delete custom theme and all
revisions** removes an unused custom theme; built-in starters/themes remain.
Current or prepared BRB, saved designs and generation history protect their
theme revisions until those uses are removed.

Generated media selected for PRESTREAM/ENDING, a clip shortcut, current playback
or a suspended return stage cannot be deleted. Clear or replace those uses in
live control first. Deletion never changes the broadcast or silently clears a
stage selection. Files are staged together before removal; startup restores
interrupted uncommitted operations or finishes committed file cleanup. Storage
errors are reported rather than acknowledging incomplete cleanup.

**Studio → Media → Library videos** manages uploaded MP4 originals separately
from scene assets and generated outputs. Upload and preparation progress, errors,
exact ready revisions, previews and explicit links to Live selection are shown
there. **Delete uploaded video** removes the original, prepared cache copies and
owned revisions only when they are not selected, playing, retained for return,
shared with another upload, or referenced by generation history. Wait for active
preparation to finish first. Deletion uses the same recoverable file transaction
as generated-result cleanup. Uploads, preparation and cleanup require desktop;
phones can browse, preview and select ready media.

Authenticated same-origin JSON deletion endpoints:

- `DELETE /api/generator/designs/{id}` with `{"version":<current draft version>}`.
- `DELETE /api/generator/jobs/{id}` with `{}`.
- `DELETE /api/generator/templates/{id}` with `{"version":<current template version>}`.
- `DELETE /api/generator/themes/{id}` with `{"revision":<latest theme revision>}`.
- `DELETE /api/generator/assets/{id}` with `{}` (separate upload deletion).
- `DELETE /api/library/files/{id}` with `{"version":<file version from GET /api/library>}` (uploaded original and eligible prepared files).

Design/result deletion reports deleted job/media counts and retained shared
media IDs. A newer draft, template or theme revision rejects a stale deletion.

### Studio design drafts

Open **Studio → Designs** to create independent prestream or
ending show designs. Add, remove, reorder or independently duplicate title and
list scenes. Each defaults to 10 seconds and selects the built-in Retro theme
revision independently of its
text and timing. Click **Save changes** to persist a design beneath
`<library_directory>/generator/designs`
(or the default BRB library directory). Wait for **Saved** before closing the tab.
A failed save retains local edits and offers Retry; a concurrent-tab conflict
requires Reload saved draft or Save local work as a copy. Neither draft saves nor
quick previews change broadcast media.

Quick previews use embedded Go Sans/Go Mono scalable fonts or Arcade Pixel
bitmap glyphs, with no system font downloads. Animated artwork previews use the
already-required FFmpeg executable. The same Go raster renderer and glyph metrics
are available to prepared-media generation. Font size is specified at 1080p and
scales with the active streaming profile. Latin text including European accents,
Greek and Cyrillic are supported; missing glyphs (including unsupported emoji)
and text overflow produce field errors. Text wraps at spaces and explicit line
breaks, preserves mixed case with Go fonts, and is never automatically shrunk or truncated.
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
range from 24 to 120 at 1080p. Quick scene previews remain available for longer drafts, while validation reports
the separate 600-second generation cap (the scene sum before overlaps). These are
bounded authoring limits, not rendering performance promises. Drafts retain invalid durations/unsupported glyphs for
correction; preview validation uses the current streaming profile (up to 4K).
Only one quick preview or validation runs at a time per server. Preview requires
the existing dashboard authentication and same-origin control header, and uses
untrusted text strictly as raster data.

### Generate and review exact media

Choose **Generate video** after saving. New edits remain local and block generation
until you click **Save changes** again. Save is disabled when unchanged; Generate
is hidden when the current saved design already has a ready video. The server captures that
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

Ready PRESTREAM results offer **Select for next PRESTREAM**, **Review Replace
PRESTREAM now**, and **Review Replace PRESTREAM on return** when applicable.
These open live control in another tab, preserving the editor. Opening a result
only loads its exact candidate; **Use for next PRESTREAM** stages a settings edit
that still requires **Save selections**. Replacement requires preview and a
confirmation against the observed broadcast. A stale request retains its
candidate for a fresh explicit choice and cannot become an action on another
stage. Generated results are also listed under **All prepared revisions** in
the video library. The generator shows saved selections, actual on-air/rehearsal
media, and suspended return revisions separately from captured jobs and drafts.

ENDING results use the same **Select for next ENDING** and **Review Replace
ENDING now** path. A running ending continues throughout authoring, generation
and next-selection saves. Confirmed replacement starts the reviewed revision
from the beginning and postpones shutdown. Once final destination writes start
or the ending completes, replacement is unavailable; the prepared result stays
available for a later explicit selection. Generation never restarts a completed
broadcast or changes rehearsal/delivery mode.

Job snapshots and outcomes persist beneath `<library_directory>/generator/jobs`;
a restart marks unfinished jobs interrupted and removes incomplete workspace
files. **Retry captured revision** explicitly creates a new job at the back of
the queue using the interrupted, failed or cancelled job's immutable inputs,
profile, renderer and duration. It does not use a newer editable draft or alter
the previous outcome. To use newer edits, save the design and choose Generate video instead.
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
its separate 512 MiB cap. Generated video timing comes from exact frame counts;
AAC packets retain the codec granularity described below.

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
Invalid overlap edits can be explicitly saved with field errors, while generation preserves
previous ready output. Source audio is combined on cumulative sample boundaries
before continuous music and the single final AAC encode.

A native FFmpeg 4.4 measurement at 1080p30 with two-second title scenes and
0.4-second overlaps took 2.40 seconds for 3 scenes and 16.58 seconds for 20 scenes.
The 20-scene run produced 32.4 seconds of output, with one preparation process,
at most two FFmpeg inputs, sampled peak encoder RSS 178 MiB and workspace 7.9 MiB.
These synthetic still-scene measurements are not deployment performance guarantees;
video, themes and longer scenes can use more memory, storage and time.

### Complete prestream cycles

The generator's **End-to-start transition** selects a cut or crossfade for the
complete PRESTREAM sequence. `loop_transition` is saved with designs, templates,
and captured jobs; omission means cut. A crossfade must fit the first and last
scenes together with their adjacent overlaps. A single scene needs room for both
ends. ENDING retains this setting without applying it.

Cycle duration includes all internal overlaps and subtracts the loop overlap.
For example, a 2-second sequence with a 0.4-second loop crossfade produces a
1.6-second cycle. It starts after the overlapped opening portion and blends the
true tail into the true head, including source audio and the complete music mix.
Music End/Repeat and fades apply within the complete sequence before this circular
composition. The displayed cycle length is not a countdown to OBS going live.

**Preview exact revision** plays two passes of newly prepared PRESTREAM output,
without selecting or activating it. The authenticated revision preview endpoint
keeps its single-pass default; `?passes=2` explicitly repeats an exact generated
revision. Older jobs retain single-pass review. The retained metadata records the
validated video frame count and captured FPS; restart, preview and the existing
broadcast controller use that same duration. Loop starts advance from one epoch,
so delayed scheduler ticks do not accumulate a gap on every cycle.

Audio is encoded once after the full mix with bounded cyclic pre/postroll to
avoid repeating encoder startup silence. AAC carries whole 1024-sample packets,
so arbitrary frame durations have up to one packet (21.33 ms at 48 kHz or
23.22 ms at 44.1 kHz), plus FLV's 1 ms timestamp rounding, of boundary granularity.
Video and scheduling remain frame exact; this is not a sample-exact audio-loop
promise. Players that ignore packet timestamps may accumulate audio overlap.

Ordinary imported revisions retain their existing timing behavior. Identical
legacy generated bytes can acquire validated frame metadata without changing
already-open readers. If those bytes instead belong to a legacy ordinary import,
generation fails with an actionable collision message; changing the design or
using the existing revision preserves that import's contract. Later ordinary
imports of identical generated bytes preserve their validated timing.
### Ending finish

ENDING designs default to fading the final second of the complete sequence to
black and silence. Change **Final fade** to another duration, or set it to zero
to preserve the final image and audio. The fade covers the assembled picture,
source-video audio and background music, including any internal crossfades.
It occupies existing media time: a 2.2-second sequence remains 2.2 seconds.

The fade rounds to the nearest video frame and must contain at least two frames
when enabled. It may span multiple scenes, but must fit the complete sequence
after transition overlaps. Shorten or disable the fade, or lengthen a short
sequence; generation never silently clips it. Drafts retain invalid edits with
an actionable field error. The setting survives templates, theme changes and
immutable job snapshots/retries. It is inactive on PRESTREAM designs.

Generate and play **Preview exact revision** to inspect the finite finish. Quick
scene previews do not show the temporal fade. The final video frame is black;
the final frame's source audio is silent before the single AAC encode (which can
have small lossy residuals). No hold frame or second is appended. Destination
draining remains a later broadcast-controller operation, separate from the media
duration. Editing, generating and previewing never start or end a broadcast.

### Managing destinations in desktop Settings

New installations start with no destinations. Add the RTMP/RTMPS server URL and
stream key in desktop **Settings → Destinations**, then enable the prepared
destination from **Live**. Names use 1–64 letters A–Z, digits, spaces, underscores, or hyphens,
with no leading or trailing spaces.
For a new destination, choose **Twitch**, **YouTube**, **Kick**, or **Custom** in
the Platform dropdown. Twitch loads its current secure servers from the
[public Twitch ingest API](https://dev.twitch.tv/docs/video-broadcast/reference/).
Choose a regional server in **Twitch server**; the initial address is the first
server returned by Twitch, not a latency measurement. Discovery runs from the
relay and never sends your stream key. If it fails, retry or enter the server URL
manually. Editing an existing Twitch destination offers **Choose Twitch server**;
loading that list keeps the saved URL until you select a server and save.
YouTube prefills its secure server URL and name; Kick prefills its name and asks
for the server URL from your Kick Creator Dashboard. Names and URLs remain
editable. Changing to a platform preset clears the draft stream key so a key
from another platform is not reused.
Up to 16 destinations are supported. A destination without a key cannot send.
Saving an edit with an empty key field preserves the stored key; enter a new key
to replace it. Keys are masked and never returned by the management API or status.
Mobile provides delivery switches and health; configuration requires desktop.

`destinations_file` identifies the private persistent credentials file (the
container example uses `/data/destinations.json`). Native deployments must set
this to a writable private path. When omitted, a configured `state_file` supplies
its directory and `destinations.json` is used. The file is atomically replaced
with mode 0600. Back it up as a secret. The older switch-state file remains
secret-free. Failed validation or writes leave the running configuration intact.

For an existing deployment, migrate deliberately before replacing its old
configuration with the new empty-target example:

1. Save a copy of the existing target JSON configuration and keep the existing
   destination environment mappings. Do not paste keys into tickets or logs.
2. In that retained configuration, add `ingestion_key_file` set to
   `/data/ingestion-key.json` and `destinations_file` set to
   `/data/destinations.json`. Keep the old `stream_key` and target entries for
   this first new-binary start, so the OBS credential imports unchanged.
3. Upgrade the image while mounting the retained configuration read-only at
   `/etc/restreamer/config.json`, preserving its environment mappings and the
   same persistent `/data` volume. For Compose/Portainer, the host config path
   must exist on the Docker host. The new bundled Compose stops passing platform
   variables, so retain them explicitly for this migration start.
4. While OFF, open desktop **Settings → Destinations** and choose **Import existing
   destinations**. Check names, readiness, and enabled preferences. Import
   preserves the current preferences, including saved legacy switches.
5. After successful import, remove the legacy targets and destination environment
   mappings, and use the new bundled configuration. Restart and verify Settings:
   the managed credentials file remains authoritative, and the broadcast stays
   OFF. Keep the persistent volume. Old configuration values never silently
   overwrite managed destinations.

Unsaved form changes stay in memory when moving between Live and Settings. Moving
to Studio or reloading prompts before discarding a draft; credentials are never
stored in browser layout preferences. Failed saves retain the draft for correction.

Editing a selected destination during a real broadcast requires confirmation
against the current broadcast state and reconnects only that destination.
Removing one also requires confirmation; removing the last leaves the show and
preview running with no delivery. New destinations start disabled and join only
through Live's confirmed delivery controls. Rehearsal never opens a destination
session. Management is locked for the entire ENDING stage, including final drain;
wait for completion or confirm Stop now. Later edits/removals preserve the
historical ending completion results and their original destination names.

### Managed ingestion keys and migration

The standard image stores its ingestion credential in `/data/ingestion-key.json`
(`ingestion_key_file`), separate from secret-free target switch state. Keep the
persistent volume when updating. The file is written atomically with permissions
0600. For native installations, set `ingestion_key_file` to a writable private
location before starting. Back up this credential securely alongside persistent
media; do not commit it or include it in diagnostic reports.

On the first managed startup only, an existing legacy `stream_key` value
(including `INGEST_STREAM_KEY` in the bundled configuration) is imported without
changing the OBS credential. If none exists, the server generates a random key.
Once the managed file exists it is authoritative: old configuration values never
replace it. After verifying OBS reconnects, remove the old environment value and
optionally the `stream_key` configuration field. Configurations without
`ingestion_key_file` retain legacy authentication for compatibility; management
controls explain that migration is required. Invalid or unreadable stored keys
fail startup rather than silently rotating credentials.

Use desktop **Settings → OBS ingestion** to reveal or copy the key. It is masked
initially and whenever you leave Settings, and clipboard failures offer manual
copying. Enter your public RTMP hostname and published port in the server-address
field; the dashboard hostname/HTTP port can differ, so the app does not infer
those values. This field is a copying aid, not server configuration. Keep the
application path shown in its placeholder and enter the key separately in OBS. Regeneration requires confirmation and
persists the replacement before success. A connected publisher **stays connected
until it disconnects**; new connections must use the new key immediately. Update
OBS before reconnecting. Concurrent or stale regeneration requests are rejected
so another browser cannot unknowingly overwrite a newer key. Restarting preserves
the key and leaves the broadcast OFF. The plaintext key is returned only by the
explicit authenticated, same-origin reveal/copy request; it is absent from status,
metrics, health responses and logs.

### Music playlists and looping presentations

In **Studio → Music**, create named playlists from uploaded tracks and arrange
playback order. Choose a track, playlist, or no music in a design's music controls.
**Follow music** prepares a download covering one music pass while its visual
sequence repeats. On air, video and music loop independently until you change stage
or press Stop—even for Ending. **Fixed** retains the existing duration and finite
Ending behavior. Playlist edits create new versions and do not change prepared or
on-air results. Music-only changes reuse the encoded visual.

BRB has the same preparation choices in its editor. Preview a prepared result and
assign it through **Show setup → BRB**; automatic fallback remains independently
configured. See [music presentation behavior and limits](docs/design/music-presentations.md).
