---
status: accepted
---

# Serve live events and replays from mounted local storage

For the MVP of [public streaming page targets (#84)](https://github.com/kreuzhofer/restreamer/issues/84), serve live HLS and retained recordings from the streaming server, using a Docker-mounted storage directory instead of S3; S3 and CDN integrations are outside the MVP. Each target's viewer URL belongs to one event, cannot be reused for a later live event after that event is finished, and also serves its replay; replay publication can be authorized when creating the target or enabled afterward. This retains event identity and operator-managed archives while reducing deployment dependencies, accepting local storage and bandwidth limits; it supersedes ADR 0011's S3 delivery decision and ADR 0010's recording/publication scope.

Record every public-page event independently of replay publication, and preserve the incoming H.264/AAC quality through HLS packaging without generating additional quality levels. Stopping the overall broadcast, including completion of Ending, closes its active events to further live content and finalizes their recordings before replay can be published; OBS disconnection and temporary target disablement do not finish an event.

The initial archive provides an event list, recording status and size, operator preview, copy-link, replay enable/disable, and Delete event. Retain recordings until explicit deletion; deleting an event removes its recording and public page and invalidates the viewer URL, which must never be reused for another event.

Check storage before starting. If storage becomes unwritable or fills up, fail the affected public target visibly while other streaming destinations continue; clearly mark incomplete recordings and never publish them automatically.

Record the programme delivered to the public target, including Starting soon, LIVE, BRB, clips, and Ending, excluding rehearsal and periods of disabled delivery. Live viewers watch near the live edge with an approximate 10–20-second latency target and no live rewind controls; published replays support seeking.

Keep event pages unlisted, with no public directory and instructions against search-engine indexing. One optional event password protects live viewing and replay, including all media endpoints; changing the password invalidates existing viewer sessions, although downloaded media cannot be recalled.

After a server restart, an event that was active is interrupted and closed to further live content. Recover playable media as an incomplete recording for operator preview and allow explicit replay publication only after the operator acknowledges the interruption; a new broadcast requires a new event and URL.
