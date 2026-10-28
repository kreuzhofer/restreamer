---
status: accepted
---

# Share BRB selection while retaining an explicit fallback override

Separate appearance activation, music preparation and deliberate-versus-automatic BRB selection make preparing a break difficult to understand. Provide one complete BRB authoring/preparation flow and use its selected ready video for both deliberate breaks and automatic fallback by default, with an explicit option for a different disconnection screen; this trades independently configured defaults for a simpler common workflow while retaining deliberate/fallback stage semantics. Preserve existing distinct fallback choices during migration and exact active-media protection; generation alone never selects or replaces on-air content.

The override covers all automatic source-unavailable situations, including failed media and paused clips, and is labelled accordingly. Selection changes affect next use; current or suspended fallback retains its exact video until a separately reviewed replacement. If selected fallback itself fails, use independently prepared silent emergency media with a persistent error and explicit recovery, avoiding dependence on the failed music process or recursive playback retries.
