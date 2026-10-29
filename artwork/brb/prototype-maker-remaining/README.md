# THROWAWAY — remaining maker motion studies (#52)

Two **unapproved motion prototypes**, on `feature/maker-prototypes-52` only.
Nothing here changes the theme catalog, prepared masters, editable designs,
selected revisions, or broadcast settings. Do not merge this exploration into
main as a production feature.

## Reconciled scope

Issue #52's October 27 reconciliation leaves **Midnight Workshop** and
**Debug Quest** outstanding. The factory / Idea to Object concept and its
Halloween and Christmas variants shipped in PR #53; their production source
remains untouched in `artwork/maker-workshop/`. The original three-way Blender
style comparison remains on `prototype/maker-brb` at `73dc5e7`.

This pass follows the user's final selected direction: **high-fidelity flat
pixel art with individually animated components in Blender**. It does not
revive the rejected/earlier miniature or pixel-styled 3D direction.

## Preview

From the repository root, with no packages to install:

```sh
python3 artwork/brb/prototype-maker-remaining/serve.py
```

Open <http://127.0.0.1:18861/?variant=midnight> or
<http://127.0.0.1:18861/?variant=debug>. The floating arrows switch concepts and
retain the inspection time. Play, pause, scrub to a frame, choose a story beat,
slow to half speed, inspect the loop seam, or download the MP4. The native video
controls remain available, including fullscreen. Keyboard arrows switch concepts
and Space toggles playback when focus is outside a control.

For a phone on the same LAN:

```sh
python3 artwork/brb/prototype-maker-remaining/serve.py --bind 0.0.0.0
```

Open `http://<this-computer-LAN-address>:18861/`. This serves **only** the five
public preview files, supports video byte ranges, and does not expose repository
source, uploaded media, application state, or credentials. Stop it with Ctrl-C.

## Choreography and review questions

Both clips are silent **16-second loops**, 384 frames at 24 fps. Each uses a
1024×576 source grid, enlarged with nearest-neighbor sampling to 2048×1152 H.264.
The environment plates are stationary; the following motion is authored
separately as sprite parts and pixel geometry.

| Concept | Sequence | Visual question still to answer |
| --- | --- | --- |
| Midnight Workshop | 0–3s: maker approaches; 3–7s: articulated soldering tool repairs the drone; 7–12s: rotors spin, drone rises, tests flight and lands; 12–16s: maker returns. Independent rotating CAD wireframe, rain and steam support the scene. | Does a quiet repair bench feel distinct enough from the shipped factory? Does the repair-and-test story read, and do the procedural drone/tool details need a richer illustrated pass? |
| Debug Quest | 0–4s: robot catches a circuit bug; 4–7s: scanner diagnoses its chip; 7–8s: repair succeeds; 8–14s: the friendly bug is escorted out; 14–16s: clear stage before the next pass. Independent leg motion, antennae, scan beam and data motes. | Does chase → scan → repair → escort read without explanation? Should the bug be more expressive, playful or mechanical? |

The Midnight drone is the **same device** throughout: it returns to its parked
pose. Debug Quest's actors leave completely off canvas before their positions
reset; the return journey is implied offstage. It is not yet a visible walking
return circuit. No visible actor is teleported to restart the loop.

The headline is deliberately baked into these preview videos. There is no
editable-text or retained-revision integration in this prototype. The bug,
drone and tool use procedural pixel geometry; their final illustrated treatment
is part of the review question, not an approved design.

## Rebuild and inspect

Blender **5.2.2 LTS** and FFmpeg **7.1.5** were used. Python uses only the standard
library. Geometry/material helpers and the articulated robot atlas reuse the
approved factory's visual language, but no existing production file is edited.

```sh
python3 artwork/brb/prototype-maker-remaining/render.py
```

Use `--variant midnight` or `--variant debug` for a partial rebuild, and
`--blender`, `--ffmpeg`, `--ffprobe` to override executable paths. Rendered frames
and packed editable `.blend` sets stay ignored locally. The Python source
computes the animation directly; the saved Blender set is **not a baked timeline**.

For selected inspection frames:

```sh
/Applications/Blender.app/Contents/MacOS/Blender -b \
  --python artwork/brb/prototype-maker-remaining/build.py -- \
  --variant midnight --frames 0 120 192 288 382 383 384
```

After all frames exist, `render.py --encode-only` re-encodes and verifies without
rendering. It checks frame availability, exact source endpoint equality,
encoded dimensions/frame count/rate/duration, and complete error-free decoding.
It records source last→first versus previous→last pixel differences and video
hashes in `verification.json`. Endpoint equality alone does not prove a natural
loop; compare the visible seam with the **Watch loop seam** control too.

## Evidence and remaining gates

- Both exact source endpoints match frame zero. Midnight's last→first change
  affects 0.0621% of pixels; Debug Quest's affects 0.0298%. These are small
  environmental changes while the actors are settled/offstage, not a full-scene
  cut. See `verification.json` for the raw measurements.
- Both MP4s fully decode: 384 frames, 24 fps, 16 seconds, 2048×1152.
- Inspected source frames show the repair tool at the drone, the drone airborne,
  the bug under the scanner, and the repaired bug walking with the robot.
- Python and preview JavaScript syntax checks pass. HTTP serves the page, returns
  a correct 206 byte-range response for video, and rejects source-file requests.
- Root-agent Chrome verification passed: playback advances, beat selection seeks
  and pauses, half speed works, 14→0 loop continuation works, and concept arrows
  preserve inspection time after loading. At a 390px viewport, page scrollWidth
  is 390px and controls wrap without overflow. Screenshots are in `verification/`.
  This is a phone-sized browser check, not a real-iPhone playback claim.
- Final standards/spec review, exact-SHA GitHub CI, and the **user's visual
  verdict** remain required. This issue must not be closed as shipped themes.

The prototype skill calls for exploratory code without new test suites. Checks
here use the established artwork/render-output seam; production tests remain
unchanged. Local full-suite results and exact-SHA CI belong in the delivery
handoff/issue comment, not a claim that this branch is a release.

## Asset provenance

The built-in `image_gen` tool created the two new stationary plates in `assets/`.
Exact prompts are retained in `assets/prompts.json`; no external model/API or
runtime image generation is used. The robot's existing source atlas is
`artwork/maker-workshop/assets/robot-parts.png`, with its original provenance in
that directory. Drone, articulated tool, circuit bug, chip, CAD, rain, steam,
scanner and data motes are authored geometry in `build.py`.
