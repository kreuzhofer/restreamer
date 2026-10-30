# Restreamer

Restreamer selects a broadcast source and forwards its media to destinations. This vocabulary covers broadcast stages and authored media for prestream and ending.

## Language

**Broadcast stage**:
The operator's selected phase of a show: OFF, PRESTREAM, LIVE, BRB, CLIP, or ENDING. The stage describes intent and can differ from the source currently on air.

**On-air source**:
The source of media currently emitted on an output path, such as OBS, a prepared video, or BRB media. Paths may differ during transitions; an on-air source does not confirm platform-side public playback.

**Clip**:
A prepared library video selected as a temporary interruption of a broadcast stage. It normally plays once, with explicit looping available in the detailed playback controls.

**Return stage**:
The stage to restore when a temporary clip finishes or is stopped. Replacing one clip with another preserves the original return stage rather than making the replaced clip the return destination.

**Deliberate BRB**:
An operator-selected break that remains in effect until explicitly left, even if OBS reconnects. It can suspend a clip or serve as a clip's return stage.
_Avoid_: Manual fallback

**Fallback BRB**:
BRB media used to maintain a visible broadcast when the intended source is unavailable. Its presence does not itself select the deliberate BRB stage.

**Fallback override**:
A separately selected BRB video used when a source is unavailable instead of the video chosen for deliberate breaks.

**Emergency BRB**:
An independent silent break screen used when the selected fallback media cannot play. It indicates degraded operation rather than a deliberate break.

**Prestream**:
The on-air starting-soon stage, using a prepared video that loops until the operator changes stage.

**Starting soon**:
The user-facing name for the Prestream broadcast stage.

**Ending**:
The on-air closing stage. Fixed-duration media finishes before forwarding stops; music-driven media loops until the operator explicitly stops or leaves the stage. Replacing its media is a separate operation within the same stage.

**Pending transition**:
A requested stage change that has not yet taken effect because its source is not ready. The existing on-air source remains selected until the transition succeeds or is cancelled.

**Preview-only session**:
A rehearsal that runs the normal stage sequence and broadcast preview while destination delivery is blocked. Starting a real broadcast requires ending the rehearsal first.

**Media revision**:
A particular version of the content selected for a stage or clip. Recreating content produces a new revision rather than silently changing the selected version.

**Generated video**:
A ready result made from a design and its chosen content, appearance and music. It can be previewed or selected for broadcast independently of later design edits.

**Next-use selection**:
The particular ready video chosen for the next use of a broadcast stage. Changing it does not replace content that is already playing.

**Stage media replacement**:
An explicitly confirmed change to the content of the current stage without selecting a different stage. It allows corrected or regenerated content to replace material already playing.

**Generator scene**:
A timed section of authored prestream or ending media, with a chosen layout, content, and styling. It belongs to the media generator, not to OBS.

**Scene layout**:
The arrangement of content within a generator scene, such as a title, text with an image, a list, or full-screen media.

**Content template**:
A saved reusable sequence of generator scenes with their layouts, content, and timing. It is the starting point for an independent show design, with appearance supplied by a separate style theme and scene overrides.

**Show design**:
An editable media design for a particular stream, optionally copied from a content template and styled with a style theme. Changes to it do not change the template or an already-generated media revision.

**Style theme**:
A named, reusable visual style for prestream, ending, and BRB, independent of scene content and timing. It defines fonts, palette, backgrounds, borders, decorative animation, logo placement, and spacing, with supported customization and per-scene overrides.

**Theme revision**:
A particular saved version of a style theme, explicitly selected by a design. Editing the shared theme does not change designs that retain an earlier revision.

**Design revision**:
A fixed snapshot of a show design used to generate media. Later draft edits do not alter that snapshot or the media revision produced from it.

**Output profile**:
The selected video and audio characteristics of a broadcast output delivered to destinations. Destinations with identical requirements can share one output profile.

**Output path**:
The media route selected for one or more destinations: passthrough or continuous output. Destinations sharing a path still have independent delivery states.

**Continuous output**:
An output mode that maintains a stable program timeline while broadcast sources change or become unavailable, supplying replacement content when needed. It does not imply protection against failure of the output engine, host, network, or destination platform.
