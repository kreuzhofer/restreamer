---
status: accepted
---

# Expose broadcast setup separately from the media library

The operator cannot reliably rediscover stage assignments among Studio's layered navigation and authoring controls. Use five primary destinations—Live, Show setup, Events, Library and Settings—with a dedicated editor reached from setup or the library, accepting an additional primary destination to make preparation and resource management explicit. This supersedes only ADR 0006's Live/Studio/Settings navigation choice; its desktop-authoring/mobile-operation boundary remains in force.

Events is a primary workspace because upcoming broadcasts and retained recordings have a lifecycle independent of reusable media. Finished public event targets leave Live and Settings → Destinations while their event entries remain manageable in Events.
