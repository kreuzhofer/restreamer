---
status: accepted
---

# Design public HLS delivery for an optional CDN

For [public streaming page targets (#84)](https://github.com/kreuzhofer/restreamer/issues/84), design HLS delivery to support an optional CDN from the start while retaining direct serving for small deployments. Shared media segments must remain cacheable independently of individual viewers, with appropriate live-playlist freshness and access enforcement on cached media for password-protected streams; origin-only password checks are insufficient when a CDN serves cached responses. This accepts additional caching and authorization design work up front so audience growth can move delivery to a CDN without replacing the streaming protocol or player; the CDN provider, access mechanism, encoding ladder, latency budget, and initial CDN integration scope remain undecided.

ADR 0012 narrows the MVP to local serving and storage. CDN compatibility remains a future design consideration; implementing a CDN integration is outside the MVP.
