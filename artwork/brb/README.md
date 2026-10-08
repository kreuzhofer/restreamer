# Editable BRB arcade animation

`animation.mjs` is the source of truth. Edit the palette, bitmap sprites,
`sceneAt()` timings or paths in `drawFrame(ctx, seconds)`. The function draws
integer rectangles through the Canvas 2D API; it has no browser clock, random
state, external assets, fonts or dependencies.

Preview it interactively:

```sh
python3 -m http.server 18081 --bind 127.0.0.1 --directory artwork/brb
# Open http://127.0.0.1:18081/preview.html
```

Use Play/Pause, the scene selector and timeline to inspect your changes. Refresh
the browser after editing. The 320 × 180 canvas is enlarged with crisp pixels.

Then regenerate the bundled artwork (Node 24+ and FFmpeg with libx264rgb):

```sh
make brb-artwork
make check-brb-artwork
make build
```

The offline renderer runs **the same drawing function**, with a small integer
`fillRect` adapter in `raster.mjs`, and pipes RGB frames to FFmpeg. It saves a
lossless 60 fps, 32-second master and a still poster in `internal/relay/artwork/`.
Commit those generated files and their manifest with source changes. CI checks
the source/asset hashes so an edited animation cannot silently ship an old render.
If you change dimensions or duration, update the Go preparation contract and
tests too. The loop's endpoints leave the actors off-screen; the message never
moves. Scene changes and mouth/feet animation are deterministic at any timestamp.

Restreamer embeds the master; startup and **Prepare & save BRB** encode it at the
saved resolution/FPS using nearest-neighbour scaling. Only FFmpeg is needed on
the server, with no JavaScript runtime or browser. Video and optional break music
loop independently. Custom image uploads use the existing static-image path.

The arcade sprites and scenery are drawn in this repository; no game ROM,
screenshots, sound recordings or third-party artwork files are included.
