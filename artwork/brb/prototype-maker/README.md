# THROWAWAY — maker BRB motion prototype

## Follow-up: distinguish authoring tool from visual style

The user's clarification is that Blender can be the authoring tool for every option:

1. Flat illustrated pixel sprites and backgrounds, animated as 2D layers in Blender.
2. Fully modeled 3D miniature scenery and actors (the initial proof).
3. Fully modeled 3D with pixel textures, stepped shading and a low-resolution render.

The primary `index.html?variant=2|3` now compares **2 versus 3** at identical camera position and inspection time. This replaces the earlier proposed illustrated-background/rendered-actor hybrid as the current experiment. Option 1 remains a valid fully animated approach, but is not implemented in this comparison. The original A/B/C camera proof remains at `camera-study.html?variant=A|B|C`.

`P.mp4` is the pixel-styled version: the same geometry and 12-second animation as A, with deterministic 32×32 pixel texture tiles, nearest filtering, four discrete shading levels and a 512×288 source render enlarged to 1536×864. The `.blend` file packs those generated texture images. This is a stylization experiment, not a claim that pixel textures automatically reproduce hand-drawn sprite art. The approved illustration is not used as a background in either video.

Rebuild the pixel variant:

```sh
/Applications/Blender.app/Contents/MacOS/Blender -b --python artwork/brb/prototype-maker/build.py -- --variant A --style pixel --save
ffmpeg -y -framerate 24 -i artwork/brb/prototype-maker/renders/A-pixel/%04d.png -vf scale=1536:864:flags=neighbor -c:v libx264 -crf 18 -pix_fmt yuv420p -movflags +faststart artwork/brb/prototype-maker/P.mp4
```

No aesthetic verdict has been accepted yet. Continue comparing motion readability and art treatment before production work.

The follow-up P video also contains exactly 288 frames at 24 fps and 1536×864. Chrome decoded it at readyState 4 and advanced playback. Switching 3 → 2 → 3 retained the 5.8-second assembly inspection point. At 390px the page width remained 390px with working decoded media. Desktop and phone comparison screenshots are retained. The first prototype capture (`813ecae`) passed repository CI; the follow-up capture requires its own branch CI result, recorded on issue #52 rather than modifying the captured commit after verification.

## Initial camera experiment

Question: can a short printer → conveyor → robot assembly sequence read clearly, and is a Blender-rendered miniature set a promising direction for the maker BRB?

This is the visual branch of the prototype skill, adapted to an animation rather than an application UI. Three structurally different camera compositions share one route (`?variant=A`, `B`, `C`) and the same 12-second choreography. A is a fixed arcade elevation, B is an isometric workshop diorama, C follows the build as an inspection shot. The floating switcher is isolated in this standalone prototype; the embedded production dashboard does not serve this directory.

Run from the repository root:

```sh
python3 -m http.server 18861 --bind 127.0.0.1 --directory artwork/brb/prototype-maker
```

Open http://127.0.0.1:18861/. Videos are ordinary local MP4 files; no installation, server state, real stream or credentials are needed. Play, pause, scrub, jump to a beat, slow playback, or switch compositions. The status below the video exposes phase, time and camera choice.

## Method

All animation geometry is created by `build.py` in Blender 5.2.2 LTS. The AI-generated concept is included only as a reference image in the page. It is not used as a backdrop or as moving cutouts. Materials, machinery, pixel lettering and robot components are modeled in the script. Motion is deterministic from time: print reveal, nozzle travel, belt slats, robot transfer, two-link arm positioning, gripper release and first steps. The scene uses a fixed orthographic camera for A and B; C follows the body.

`maker-prototype.blend` retains the editable set at the beginning of the sequence. The Python script is the animation source; motion is not baked into Blender timeline keyframes in this throwaway.

Regenerate a variant (requires local Blender and FFmpeg):

```sh
/Applications/Blender.app/Contents/MacOS/Blender -b --python artwork/brb/prototype-maker/build.py -- --variant A --save
ffmpeg -y -framerate 24 -i artwork/brb/prototype-maker/renders/A/%04d.png -vf scale=1536:864:flags=neighbor -c:v libx264 -crf 18 -pix_fmt yuv420p -movflags +faststart artwork/brb/prototype-maker/A.mp4
```

Repeat with B and C. Rendering uses 768×432 source frames at 24 fps, enlarged to 1536×864. No audio is baked in. The saved message is a prototype constant rather than the production editable overlay.

## Provisional finding

Blender makes the mechanical action controllable and gives real object depth. The result reads as a low-resolution miniature 3D set, not the rich hand-drawn pixel illustration. Matching that concept would need more asset detailing and a deliberate sprite/pixel treatment. This is a motion and camera proof, not a final theme or a fidelity claim. The user's aesthetic verdict is still pending. No decision is promoted to main.

Limitations: robot walking is a simple pose cycle; printing reveals solid component geometry rather than simulating extrusion paths; the next head is restaged between cycles; C crops the BRB sign; there is no editable BRB text or Restreamer activation integration here.

## Manual verification

All three videos rendered successfully. A contains 288 frames at 24 fps, 1536×864 and exactly 12 seconds. In Chrome, playback decoded at `readyState=4` and advanced; the assembly button sought to exactly 5.8 seconds; switching A → B → C retained that inspection time and decoded each video. The local static server does not provide seekable byte ranges, so the page loads each small video as an in-memory Blob before playback. At 390×844, page width was 390px and the video, controls and readout remained available. Temporary viewport override was reset. Desktop and mobile screenshots are included. No automated tests were added for this throwaway visual experiment.
