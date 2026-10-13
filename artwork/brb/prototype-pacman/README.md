# Throwaway Pac-Man fidelity study

Selected direction: B, the detailed overhead maze. Compare A (current reference), B (maze machine), and C (cabinet) on `preview.html?variant=B`.

```sh
python3 -m http.server 18865 --bind 127.0.0.1 --directory artwork/brb/prototype-pacman
```

The illustrated machinery plates were created with the built-in imagegen tool; exact prompts are retained in assets/prompts.json. Actors, pellets, eyes, fans, light packets and text use deterministic Canvas drawing. No game ROM, external font or runtime service is needed. The message remains editable. This is a throwaway visual branch, not integrated or production-ready artwork. Select and refine the direction before implementing the retained theme and preparation contracts.


`node artwork/brb/prototype-pacman/render.mjs` exports both 1080p/30 fps previews with FFmpeg/FFprobe. The renderer checks 1200 decoded frames, exact 0/40-second source endpoints, and error-free full decoding. B.png/C.png are stills from the rendered output, not mocked screenshots. FFMPEG and FFPROBE can override the command paths.

Only the preview HTML, its runtime modules/font, the two PNG plates and the rendered videos belong in a shared preview directory. Do not serve the repository or app configuration.

The first purely geometric code study proved the choreography, but lacked the visual density of the approved workshop. The illustrated plates supply texture; the animated characters, pellets, returning eyes, light packets and fan/speaker details remain controlled by code. A is the unchanged legacy reference. B is selected. C remains an alternate study.

Validation: desktop browser comparison and a 390×844 phone viewport; no horizontal overflow; pause, timeline, scene jump and custom text exercised. Captures are in verification/. All source/video hashes in manifest.json were checked after rendering. The selected maze now uses a 40-second continuous loop: a top-center chase, bottom-center power reversal, full lap, visible ghost regeneration, staggered exits and a top-center reunion. Production integration and retained-revision tests remain pending.

`node --test artwork/brb/prototype-pacman/motion.test.mjs` checks actor continuity at 120 samples/second across every transition and the seam, plus regeneration, clockwise exits at normal speed, and the closing encounter. The runtime and offline renderer use that same motion state.
