# THROWAWAY — animated maker BRB styles

## Selected direction: #1, high-fidelity flat pixel art

The user chose **option 1, with higher illustration fidelity**. The current pass combines a detailed illustrated workshop plate with independently animated sprite parts in Blender. Options 2 and 3 remain as references. Nothing in this directory is served by the production dashboard or merged into main.

Run from this worktree's repository root:

```sh
python3 -m http.server 18861 --bind 127.0.0.1 --directory artwork/brb/prototype-maker
```

Open `http://127.0.0.1:18861/?variant=1`. The arrows cycle 1/2/3 while retaining inspection time. Play, seek, jump to a story beat, or choose **Expand view** to inspect the artwork. In the expanded view, Space toggles playback and Escape returns. Pixel views use integer device-pixel scaling; small low-density windows can scroll inside the picture instead of blurring it.

| Option | Method | Source grid | Download |
| --- | --- | --- | --- |
| 1 · Illustrated pixel art | Separate flat illustrated sprites and animated pixel geometry, composed in Blender | 1024×576 | `F.mp4`, 4096×2304 |
| 2 · 3D miniature | Modeled objects with soft lighting | 768×432 | `A.mp4`, 1536×864 |
| 3 · Pixel-styled 3D | Same 3D geometry as 2, pixel textures and stepped shading | 512×288 | `P.mp4`, 2048×1152 |

All are 12 seconds, 288 frames at 24 fps, with no audio. The original illustration is only a reference; option 1 has a newly generated stationary plate and a separate transparent component atlas, not an animation of that original finished illustration.

## Option 1 authoring

`build_flat.py` is the animation source. The built-in imagegen tool created `assets/workshop-plate.png` and `assets/robot-parts.png`; the exact prompts are retained in `assets/prompts.json`. `assets/sprite-bounds.json` records the opaque component bounds within the six-cell atlas. No runtime image generation is involved.

Blender rigs the robot head, torso, arms, legs, extruder and spool as separate nearest-filtered flat quads. The atlas alpha is clipped to avoid blurred halos. The arm linkage, flexible hose, conveyor, gears, printer gantry, CAD drawing, facial expressions, print supports, transfer carriage and delivery shuttle are independent animated geometry. Emission materials and a frontal orthographic camera preserve the flat illustrated treatment; no lit 3D props are used in this option.

The loop shows body printing, transfer to the conveyor, head attachment, eye boot-up and walking. A maintenance robot waves in the foreground. A spare head arrives on a shuttle before the loop boundary, and the printer returns to its starting pose. Geometry and sprite positions are deterministic from time.

```sh
/Applications/Blender.app/Contents/MacOS/Blender -b --python artwork/brb/prototype-maker/build_flat.py -- --save
ffmpeg -y -framerate 24 -i artwork/brb/prototype-maker/renders/flat/%04d.png -frames:v 288 -vf scale=4096:2304:flags=neighbor -c:v libx264 -threads 2 -crf 12 -pix_fmt yuv420p -movflags +faststart artwork/brb/prototype-maker/F.mp4
```

Use `--still 139` for one inspection frame. `maker-flat-prototype.blend` saves the editable sprite set at the start and packs its images. The Python script drives changing sprite meshes and poses; animation is not baked into timeline keyframes in the saved set.

The 4× MP4 enlargement aligns source pixels with the 4:2:0 chroma grid. The preview samples pixel centers back onto the native grid and then enlarges without smoothing. This preserves normal H.264 compatibility.

## Reference options and sharpness correction

`build.py --variant A --style pixel --save` regenerates option 3. Encode its `renders/A-pixel/%04d.png` frames with the same command, using `scale=2048:1152:flags=neighbor` and destination `P.mp4`. Omitting `--style pixel` renders the miniature; encode `renders/A/%04d.png` at 1536×864 into `A.mp4`.

The earlier blur in option 3 was reproduced at fixed frame 139. Blender's inherited 1.5-pixel reconstruction filter softened edges; narrowing it to 0.01 made the render crisp while retaining 64 lighting samples. Solid glyph cells removed subpixel cracks in the lettering. The old browser fit also used a fractional scale (about 2.68 device pixels per source pixel in the measured desktop viewport). Both pixel options now use the unsmoothed integer-scaled canvas. The user confirmed the improvement.

The original A/B/C camera study remains at `camera-study.html?variant=A|B|C`. The original low-detail option 1 is captured in commit `44c024b`.

## Scope and verification

This is an art and motion prototype. There is no configurable text, production BRB generation or Restreamer activation integration yet. Final timing and illustration direction remain open to user feedback; the selected style is option 1.

The prototype skill skips new automated tests. Validation uses Python/JavaScript syntax checks, existing web/artwork checks, decoded frame count/duration, selected rendered-frame inspection and desktop/phone browser checks. Full local `make check` passed during this prototype sequence. Exact final-SHA CI and the design verdict are recorded on GitHub issue #52. Captures `flat-comparison-desktop.png` and `flat-comparison-mobile.png` show the current page.

Detailed-pass verification: the rendered MP4 decodes without errors and contains exactly 288 frames at 24 fps / 12 seconds / 4096×2304. Selected source frames show partial printing and the assembled walking character. Chrome decodes at readyState 4; expanded view renders a 2048×1152 canvas at integer scale, Space advances playback, and Escape restores the page. At 390px the page remains 390px wide and overflow stays inside the preview. Existing web/artwork and source syntax checks passed. The expanded screenshot is `flat-expanded-desktop.png`.
