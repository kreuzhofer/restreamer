# Maker Workshop · revision 1

Three built-in shared themes: **Maker Workshop**, **Maker Workshop · Halloween**,
and **Maker Workshop · Christmas**. Each has distinct BRB, prestream and ending
masters. All use the approved flat illustrated pixel-art workshop and articulated
sprite animation, authored in Blender. No Blender, GPU, browser, Node or image
service is required on the server.

BRB builds, assembles and greets with a robot in a repeating 12-second cycle.
Prestream opens on the welcoming mascot and cycles through the production line.
Ending waves goodbye, parks the printer, stops the conveyor and settles the CAD
screen. It holds its final pose for longer scenes; the existing design ending
fade still finishes the output in black. Each generator scene starts its own
stage master. Use 12-second multiples for uninterrupted prestream motion; the
included prestream starter uses three 12-second scenes. The ending starter uses
two 12-second scenes and a final two-second fade.

Halloween includes witch hats, a floating ghost and haunted workshop decor.
Christmas includes Santa hats, a scarf, moving parcels and festive workshop decor.
The upper sign stays blank in the artwork: all headlines, lists and links are
rendered by the app using the embedded pixel font. The default sign region is
58% wide and 16% high, offset upward by 33%, with a smaller list font. Overflow
is validated; text is never silently truncated or shrunk. Long agendas should be
split into more scenes. Existing themes and retained revisions remain unchanged.

## Use in the app

- Studio → BRB → Exact theme revision: select a Maker Workshop theme, prepare,
  preview the exact candidate, then activate it.
- Studio → Designs: choose Prestream or Ending and the corresponding **Maker
  workshop** content starter, then choose the regular, Halloween or Christmas
  theme. Edit the content, generate, inspect and select the exact ready revision.
- Duplicating a theme preserves its artwork and supported customization. Choosing
  `none` for its decorative effect freezes the stage-specific artwork. Text,
  colors, scene overrides, uploaded media, logos and soundtrack behavior continue
  to use the existing generator contracts.

## Authoring and verification

```sh
python3 artwork/maker-workshop/render.py --blender /Applications/Blender.app/Contents/MacOS/Blender
node artwork/maker-workshop/verify.mjs --record
make check-workshop-artwork
```

The renderer accepts `--season workshop|halloween|christmas` and
`--stage brb|prestream|ending` for a partial rebuild, plus `--ffmpeg` and
`--ffprobe` paths. Blender 5.2.2 was used for these assets. `build.py --frames`
can render selected source frames for inspection. Each master has 288 frames at
24 fps / 12 seconds, rendered at 1024×576 and encoded at an integer 2× enlargement
(2048×1152 H.264). The server uses nearest-neighbor scaling for this pixel-art set.

The authoring script checks exact source loop endpoints for BRB and prestream,
stationary source frames from six seconds through the ending tail, video metadata,
and complete error-free decoding. CI checks the source/output hash manifest and
runs public HTTP generator/BRB integration tests. The ending uses FFmpeg's
[`tpad` clone mode](https://ffmpeg.org/ffmpeg-filters.html#tpad) to hold its last
frame, bounded by the existing scene duration and output frame limit.

The local `preview.html` is a gallery of app-generated examples. Serve it only
with the nine rendered example videos named `workshop-brb.mp4`, etc.; these are
not the blank background masters. Do not expose source or application state on
an unauthenticated LAN server. Verification screenshots are in `verification/`.

## Provenance

The built-in imagegen tool created the three stationary workshop plates and the
transparent component atlases. Exact prompts are retained in `assets/prompts.json`
and `assets/original-robot-prompts.json`. `sprite-bounds.json` and
`seasonal-bounds.json` capture the component regions; the original alpha is
preserved. Blender clips atlas alpha and uses nearest filtering. The original
illustrated prototype and user-approved motion are captured on the throwaway
`prototype/maker-brb` branch, culminating in `73dc5e7`.
