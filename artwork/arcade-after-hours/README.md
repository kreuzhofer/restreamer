# Arcade After Hours · revision 1

A shared prestream, ending and BRB theme. This source produces the **background
only**; Go draws editable text, images, borders and logos over it. No headline or
subtitle is baked into the prepared video. Existing Retro revisions are unchanged.

The native server embeds `internal/relay/artwork/after-hours/loop.mp4` and the
poster. There is no browser, GPU, Node or font download at runtime. FFmpeg scales
and composes the master with transparent content at the captured streaming
profile. Quick previews decode the same master and frame-rate conversion.
Downscaling averages source pixels; text uses integer bitmap pixels.

The approved Vivid recipe has four independently masked effects: canal palette
cycling, cabinet screen highlights, chasing warm bulbs and pronounced star
shimmer. Geometry and the central content area remain still. This is a full-color
palette-style effect, not an indexed-color asset. Every phase is periodic over
16 seconds. BRB retains all 16 seconds; scenes restart the cycle independently.
Use scene durations in multiples of 16 seconds, or crossfades, when a continuous
prestream loop is desired. Existing scene transitions and ending fades apply.

The orange ghosts must remain outside the chasing-light mask. The nearby right
strip ends at source x=1156, before the right ghost. The two ships and moon are
excluded from star animation. `check-masks.mjs` samples both orange bodies at
four animation times and requires zero changed pixels.

## Rebuild

Development tools only: Node, Playwright with Chromium, FFmpeg with libx264.
Set `PLAYWRIGHT_MODULE` to an installed Playwright module if it is not locally
resolvable. Set `FFMPEG` to override the encoder executable.

```sh
node artwork/arcade-after-hours/build.mjs
node artwork/arcade-after-hours/check-masks.mjs
node artwork/arcade-after-hours/render.mjs
cp artwork/arcade-after-hours/arcade-after-hours-vivid-1080p.mp4 internal/relay/artwork/after-hours/loop.mp4
cp artwork/arcade-after-hours/poster.png internal/relay/artwork/after-hours/poster.png
node artwork/arcade-after-hours/verify.mjs --record
make check-arcade-artwork
```

`index.html` is an ignored, self-contained source preview with Vivid/Subtle
comparison. Only Vivid ships as revision 1. The renderer checks exact loop
endpoints, unchanged center, motion contrast and mobile overflow. Encoding is
1920×1080, 30 fps, 480 frames, H.264 yuv420p, silent. Committed media is pinned by
SHA-256 so CI verifies it without requiring a browser or image generation.

## Artwork provenance

Background created with the built-in image-generation tool, from the approved
Arcade After Hours concept. Final production edit prompt:

> Remove ONLY the large central words STARTING SOON and small central sentence
> A little setup. Then we're live. Fill their former area with uninterrupted
> very dark midnight-blue sky matching its surroundings, leaving that center
> empty for editable application text. Preserve the entire rest of the image
> EXACTLY: same framing, size, composition, all stars, spaceships, moon, arcade
> cabinets, characters including orange ghosts, skyline, canal reflections,
> colors and pixel-art texture. Do not add text or new objects. This is a
> production clean background plate, not a redesign. Retain 16:9 landscape.

`internal/mediaauthor/pixel-font.json` derives from the repository's hand-drawn
`artwork/brb/font.json`, with a bullet and typographic apostrophe. Lowercase maps
to uppercase intentionally. Unsupported glyphs are reported, never substituted.
