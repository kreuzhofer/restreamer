---
status: superseded by ADR-0012
---

# Use S3 for the live origin and event archives

For [public streaming page targets (#84)](https://github.com/kreuzhofer/restreamer/issues/84), the externally hosted mode uploads completed HLS segments to S3 during the event and uses S3 as both the live origin and durable archive storage, alongside the direct local mode retained by ADR 0009. Publish playlist references only after their media uploads succeed; when recording is enabled, retain the event's complete media history so a finalized replay playlist can reuse the uploaded objects after the event, with replay publication remaining explicit under ADR 0010. This avoids routing external viewer media traffic through Restreamer or waiting for a whole-recording upload after the event, accepting that S3 upload failures affect public live delivery; provider compatibility, CDN integration, bounded upload recovery, retention, and storage configuration remain to be specified.
