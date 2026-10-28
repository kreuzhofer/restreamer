# Studio UX prototype — throwaway

Question: can the approved four-destination model make preparing and operating a show understandable without coaching?

Run: `python3 -m http.server 18881 --bind 0.0.0.0 --directory internal/relay/web/ux-prototype`

Open http://127.0.0.1:18881/#setup. Phone: use this Mac's LAN address with port 18881.

All data is synthetic and in memory. No application API calls or real broadcasts. Reload resets state. The Prototype · scenarios control exposes on-air selection changes, source failure, emergency fallback, generation/save failure, empty library and partial cleanup. It is review tooling, not proposed production chrome.

Try: Show setup → edit BRB → Music → create/select playlist → Generate video → Video ready → Use for BRB. Compare current versus next-use in the Already on air scenario. Use the Library for history and cleanup.

The approved interaction layout is intentionally one coherent direction, rather than reopening the twenty settled product decisions with unrelated visual variants. This expands the prototype skill's state-flow exercise into a full app-screen prototype. It is not production-ready code and must not be promoted directly to main.

Media provenance: artwork copied unchanged from output/ux-review/assets (prior user-approved project previews); workshop border from output/theme-frames/maker-workshop.png; illustrative arcade video from output/prototypes/arcade-after-hours/arcade-after-hours-vivid-1080p.mp4. Thumbnails are illustrative; simulated generation does not create media. No external font or asset calls.
