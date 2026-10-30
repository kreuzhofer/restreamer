---
status: accepted
---

# Add continuous output alongside passthrough

To strengthen continuity across broadcast source changes without requiring every
destination to incur another encoding generation, retain passthrough and add an
optional continuous-output path. The first release has one shared 1080p25 output
profile; each destination can select passthrough or that output, with independent
destination connections. Multiple encoded output profiles and broader live music
mixing are outside this first release.

Production implementation is gated on a real-media benchmark and at least a
four-hour endurance run on the intended host. Preserve the chosen quality: if
capacity is insufficient, stop rollout for an explicit hardware or profile
decision rather than silently reducing resolution or frame rate. This decision
does not assert that continuous encoding fixes the separately investigated
YouTube green-screen incident.

Target approximately four seconds of additional relay delay, bounded at eight
seconds during healthy operation. Output-mode changes require OFF; an encoder
failure is reported and recovered through restart/reconnect, never a silent
change to passthrough. Live output takes priority over preparation workloads.
Buffer live input before source selection so ready manual source changes appear
at relay output within one second after readiness and command acceptance,
excluding external platform playback delay.
Stop now discards queued content, whereas normal fixed-length endings drain their
final media. Use the existing settings, Live monitor and job queue to expose mode,
actual output, delay, recovery and deferred preparation. Detailed acceptance gates
are recorded in [the design plan](../design/continuous-output.md). The operator
confirmed the complete plan on 2026-10-30.
