# Pac-Man Maze production artwork

The approved detailed maze and continuous 40-second chase from [the fidelity prototype](https://github.com/kreuzhofer/restreamer/tree/b1fc8b13af27408683d6fbe5ac01a5c65e5945d1/artwork/brb/prototype-pacman). The prototype comparison page and alternate cabinet stay on their throwaway branch.

The illustrated plate was created using the built-in image generator. Prompts are retained in prompts.json. Native deterministic Canvas drawing supplies the actors, pellets, returning eyes, regenerated bodies, fans, light packets and score popups. No game ROM or external runtime service is used.

```sh
make pacman-artwork
make check-pacman-artwork
```

Rebuilding requires Node and FFmpeg/FFprobe (`FFMPEG` and `FFPROBE` may override paths). Production only needs the checked-in media and FFmpeg. Sources render at 960×540, upscaled with nearest-neighbor sampling into 1920×1080/30 fps masters. Both masters are 1200 frames. The BRB master has the small STREAM PAUSED caption; the authored-scene master omits it. Headlines are always rendered separately by the production theme, never baked into either master.

The build checks endpoint pixels, exact video dimensions/rate/frame count and complete decode, then records source/asset hashes. CI checks those hashes and samples actor positions at 120 Hz through all transitions and the seam. At 30 seconds Pac-Man passes bottom-center; the ghosts emerge one by one, turn left along the bottom corridor at normal chase speed, and approach him at top-center. Pac-Man turns away there and the opening chase resumes without position resets.

Use Studio → BRB → Shared style theme → Pac-Man Maze · revision 1 to prepare, preview and explicitly activate it. Runtime retains exact generated tracks across restart. Existing legacy default artwork is independently preserved.
