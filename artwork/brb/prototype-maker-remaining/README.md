# THROWAWAY — remaining maker motion studies (#52)

Two motion prototypes on `feature/maker-prototypes-52`, captured in draft PR #66.
User visual review remains open. Nothing here changes the theme catalog,
prepared masters, editable designs, selected revisions or broadcast settings.
Do not merge this exploration into main as a production feature.

## Reconciled scope and feedback

Issue #52's October 27 reconciliation leaves **Midnight Workshop** and
**Debug Quest** outstanding. The factory / Idea to Object concept and its
Halloween and Christmas variants shipped in PR #53; their production source
remains untouched in `artwork/maker-workshop/`. The original Blender style
comparison remains on `prototype/maker-brb` at `73dc5e7`.

This pass follows the selected **high-fidelity flat pixel-art** direction,
with independently animated components in Blender. After the first two
16-second studies, the user requested two concrete refinements:

- Debug Quest characters must enter and leave through the illustrated doors.
- Midnight Workshop should be a longer, cute robot-and-drone story with
  meaningful community and friendship, rather than a short repair cycle.

## Preview

From the repository root, with no packages to install:

```sh
python3 artwork/brb/prototype-maker-remaining/serve.py
```

Open <http://127.0.0.1:18861/?variant=midnight> or
<http://127.0.0.1:18861/?variant=debug>. The arrows switch concepts and retain
inspection time, clamped to the destination video's duration. Play, scrub,
choose a story beat, slow to half speed, inspect the loop seam or download the
MP4. Native video controls include fullscreen. Keyboard arrows switch concepts
and Space toggles playback when focus is outside a control.

For a phone on the same LAN:

```sh
python3 artwork/brb/prototype-maker-remaining/serve.py --bind 0.0.0.0
```

Open `http://<this-computer-LAN-address>:18861/`. Only the six public preview
files are served, including timing metadata. Video byte ranges are supported;
repository source, uploaded media, application state and credentials are not
exposed. Stop it with Ctrl-C.

## Midnight Workshop — 60 seconds

A wordless delivery-day story, with time for anticipation and reactions:

| Time | Story |
| --- | --- |
| 0–8s | The maker works quietly; a friendly drone struggles in with a heavy supply crate. |
| 8–16s | The maker puts their tool aside, walks over, steadies the load and helps park it on the bench. The drone gets a rest. |
| 16–26s | The maker cannot reach a high shelf. The drone returns the favor by fetching its energy cell. |
| 26–34s | A smaller robot arrives with an unlit lantern. The friends welcome the visitor. |
| 34–44s | The maker holds the lantern while the drone lowers the cell into it. The lantern lights. |
| 44–48s | The visitor bounces with delight; the maker and visitor wave. |
| 48–54s | The drone lifts a replacement cell from inside the crate and restocks the shelf. The visitor takes the lit lantern and happily leaves along the front of the bench. |
| 54–60s | The maker helps the drone launch its delivery onward, the visitor carries the lantern onward, and the maker returns to quiet work. |

The drone is never broken and repaired again at the loop seam. The repeatable
activity is a cooperative delivery stop: the same friends can help the next
visitor. Both visitors are offstage at the boundary, the maker is settled, and
the shelf is visibly replenished. The used cell's relocation occurs while
concealed inside the lantern/crate, rather than teleporting across the bench.

The drone body, separate rotors, crate, cell and both lantern states now use a
new detailed illustrated atlas. The rotor geometry, cargo tether, winch, hands,
walking, reactions and prop trajectories move independently. No explanatory
captions are burned into the story; only the BRB headline and theme name remain.

Review question: does the mutual help read clearly, with enough time for the
characters' reactions? Does the calm minute-long rhythm suit a BRB loop?

## Debug Quest — 16 seconds

0–2.5s: the left shutter opens and both actors step out through the doorway.
2.5–5s: chase; 5–7.5s: scan and repair; 7.5–8.5s: success; 8.5–12.5s: escort to
the right doorway; 12.5–14.5s: step inside; 14.5–16s: the right shutter closes.

Actors fit the actual illustrated apertures, scale with depth as they step onto
the platform, and pass behind the original illustrated door frames when entering.
The foreground masks sample the existing plate rather than replacing its art.
Opaque shutters conceal the reset: there is no fading or teleporting on the
open platform, and neither actor simply walks through the outer canvas edge.

Review question: do both doorway crossings feel physically connected to the set,
while the chase → repair → escort story stays readable?

## Rebuild and verify

Blender **5.2.2 LTS** and FFmpeg **7.1.5** were used. Python uses only its standard
library. The renderer and preview read each concept's duration, frame rate and
story beats from `public/concepts.json`.

```sh
python3 artwork/brb/prototype-maker-remaining/render.py
```

Use `--variant midnight` or `--variant debug` for a partial rebuild, and
`--blender`, `--ffmpeg`, `--ffprobe` to override executable paths. Each video is
24 fps at 2048×1152, enlarged with nearest-neighbor sampling from 1024×576.
Midnight has 1440 frames; Debug has 384. Rendered frames and packed editable
`.blend` sets stay ignored locally. Python computes the animation directly;
the saved Blender set is **not a baked timeline**.

For source-frame inspection, including the 60-second endpoint:

```sh
/Applications/Blender.app/Contents/MacOS/Blender -b \
  --python artwork/brb/prototype-maker-remaining/build.py -- \
  --variant midnight --frames 0 240 480 912 1248 1438 1439 1440
```

After all frames exist, `render.py --encode-only` re-encodes and verifies. It
checks frame availability, exact source endpoint equality, encoded dimensions,
frame count/rate/duration and complete error-free decoding. `verification.json`
records last→first versus previous→last pixel differences and video hashes.
Endpoint equality alone does not prove a natural loop: also inspect the visible
seam with **Watch loop seam**.

The refined preview passed native Chrome checks at desktop and a 390px phone
viewport: playback advances, story beats seek and pause, half-speed works, the
Midnight seam wraps from 58s to the quiet opening, and switching to Debug clamps
a late Midnight seek to Debug's 16-second duration. The doorway entry and exit
were inspected in motion. Controls wrap within the phone viewport. Screenshots
are `verification/desktop-story.png` and `verification/phone-story.png`; the
older desktop/phone images belong to the first studies. Device emulation was
turned off after verification. No real-iPhone check is claimed.

The prototype skill calls for exploratory code without new test suites;
verification uses the established artwork/render-output seam. Exact-SHA CI and
local-check limitations are recorded on the PR and issue.

## Provenance

The built-in `image_gen` tool created the stationary plates in `assets/`;
`prompts.json` retains their exact prompts. It also created the new detailed
friendship atlas: `friendship-prompt.txt` records generation and alpha correction,
and `friendship-bounds.json` records its component bounds. Blender clips alpha
at 0.5 and uses nearest texture filtering. There is no runtime image generation.
The existing robot atlas remains `artwork/maker-workshop/assets/robot-parts.png`.
Drone rigging, tools, bug, shutters, CAD, weather and winch are authored in
`build.py`. No existing production artwork or configuration was modified.
