# Neon Night · revision 1

Shared prestream, ending and BRB artwork: a midnight skyline, rooftop cat,
sunset and river. Go renders editable aqua pixel lettering with magenta edges
and purple shadows; no text is baked into the background.

The 16-second Vivid loop animates four independent masks: river reflections,
emissive window pixels, cyan/magenta corner trims and stars. Buildings, the cat,
sun and central content area remain still. Window masks affect only luminous
pixels; water masks follow the foreground roof silhouettes. The full-color
palette-style animation does not move the camera or warp geometry.

The server embeds the prepared H.264 master and PNG poster. FFmpeg composes
transparent content over the master at the captured profile, preserving aspect
ratio. Quick previews decode that same master. Runtime needs no browser, GPU,
Node, external fonts or image-generation service. Existing themes stay pinned.

BRB preserves the full 16-second cycle. Generator scenes restart it independently;
use multiples of 16 seconds or crossfades for a continuous prestream. Existing
scene transitions, video insets, logos and ending fades continue to apply.

## Rebuild

Development tools: Node, Playwright with Chromium and FFmpeg with libx264.
Set `PLAYWRIGHT_MODULE` or `FFMPEG` when these tools are outside the default path.

```sh
node artwork/neon-night/build.mjs
node artwork/neon-night/check-masks.mjs
node artwork/neon-night/render.mjs
cp artwork/neon-night/neon-night-vivid-1080p.mp4 internal/relay/artwork/neon-night/loop.mp4
cp artwork/neon-night/poster.png internal/relay/artwork/neon-night/poster.png
node artwork/neon-night/verify.mjs --record
make check-neon-artwork
```

The ignored `index.html` offers Vivid/Subtle comparison and individual effects.
Only Vivid ships. Export checks identical loop endpoints, a stationary center,
stronger effect contrast and desktop/mobile layouts. Mask checks protect the cat,
sun and foreground roofs. The output is silent 1920×1080, 30 fps, 480 frames.
CI verifies SHA-256 source/output hashes without regenerating artwork.

## Artwork provenance

Created with the built-in image-generation tool from the earlier Neon Night
concept board. Production prompt:

> Use case: precise-object-edit. Input image is the Neon Night concept board, an edit/reference target. Produce ONE full-bleed 16:9 production background using the TOP LARGE scene only, not a board or collage. Remove ALL text including STARTING SOON and subtitle, reconstructing dark midnight blue starry sky behind it. Preserve the rich crisp pixel-art city at night, rooftop water tower and small cat at lower left, bridge and cyan/magenta skyline, orange-pink sunset at right, water reflections, detailed foreground rooftops, and cyan/magenta pixel corner trims. Preserve composition: architecture concentrated in lower 40% and extreme sides, broad quiet dark sky in upper center for editable text added later. Full-bleed 1920x1080 landscape composition, no external frame, no gray margins, no panels. High fidelity 16-bit pixel art with deliberate square pixels, purple clouds, glowing individual windows, deep blue shadows. No typography, no letters, no numbers, no logos, no watermark. Keep geometry crisp and unmoving, ready for local light and reflection animation.

Animation player and exporter derive from the repository's Arcade After Hours
authoring tools, with masks and neon light colors specific to this illustration.
The pixel font is the existing embedded `internal/mediaauthor/pixel-font.json`.
