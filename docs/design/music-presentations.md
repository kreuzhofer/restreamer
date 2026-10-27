# Music playlists and presentations

Studio → Music manages ordered playlists of uploaded, normalized music revisions.
Saving an edit creates a new playlist version. A saved design, queued preparation,
and retained presentation keep their exact version until explicitly changed.
Deleting a playlist never deletes uploads; referenced playlists cannot be deleted.
All historical playlist versions protect their referenced tracks.

Prestream and Ending offer **Fixed** and **Follow music** under Design → Music &
duration. BRB offers the same choices under **BRB music presentations**. Prepare,
preview or download, then explicitly assign a ready result in Show setup. BRB
presentations capture the current encoded BRB animation; appearance preparation
and activation still happen in the existing Appearance controls. The deliberate
BRB selection is separate from the existing automatic fallback. Clearing that
selection uses the current fallback for deliberate BRB too.

Fixed preserves the scene-sequence duration and finite Ending completion. Follow
music uses the decoded sample count of one track or the sum of an ordered playlist,
rounded **up** to the next video frame. Its download contains that one music pass;
up to one frame of music wrap can fill the rounding remainder. On air, the complete
visual sequence and the music pass repeat independently at their original speeds.
This applies to ENDING too: music EOF does not drain destinations or stop the show.
Use Stop now or change stage. A new server always starts OFF. Rehearsal never opens
destination sessions. Preparation never changes selections or active playback.

## Audio and video pipeline

The first preparation retains an encoded visual without background music and with
its authored scene audio. A fingerprint captures visual settings, source revisions,
theme and profile. Later music-only preparations reuse this encoded visual. BRB
uses its already encoded current animation and silent source audio, replacing the
legacy BRB music in the selected presentation. Legacy fallback media is unchanged.

Preparation concatenates normalized 48 kHz stereo signed-16-bit music on disk,
applies the requested music gain and fades, extracts the retained scene audio to
a second PCM file and records both digests. One fixed gain of
`1 / max(1, scenePeak + musicPeak)` protects against clipping for every relative
phase while preserving the relative source/music levels. It may be conservative;
the applied percentage is visible on generation results. Source-audio enable and
gain choices are preserved in the visual preparation. Fixed endings retain their
whole-composition final fade. Music-driven visual loops retain the authored visual
ending; music fades apply per complete music pass, not per visual cycle.

The downloadable FLV/MP4 presentation copies encoded video packets and encodes only
the mixed audio. The retained presentation identity covers the export digest,
profile, frame timing and exact independent audio/video inputs. Preview/download
uses this combined one-pass export. Regular imported media and legacy fixed designs
keep their existing byte-digest identities and playback behavior.

On-air Follow music uses the native disk-backed encoded video reader and a single
**audio-only FFmpeg process per retained playback**. Scene PCM and music PCM repeat
independently; AAC encoding continues across boundaries, avoiding recurring encoder
priming. Video uses cumulative frame timing. No video decoding or encoding is needed
on air, and OBS passthrough remains native Go. A paused/suspended presentation keeps
its encoder behind pipe/channel backpressure and resumes its phase on Return.
Stop/replacement/shutdown cancels it. Pause/Resume are available for presentations
played as clips; seek and disabling their continuous loop are explicitly rejected.
Missing files, start failures, encoder exit and five seconds without audio packets
surface as playback failures with fallback and an explicit Retry action.

## Bounds and storage

- 100 playlists, 100 immutable versions each, 50 tracks per version.
- One hour of decoded music per presentation; existing individual upload limits
  remain 10 minutes / 32 MiB source upload.
- Existing visual authoring limit: 10 minutes. Music may extend exports beyond it.
- At most 2 GiB per combined export; the existing 15-minute preparation deadline,
  eight outstanding jobs and 200-job history limits still apply.
- PCM processing uses bounded chunks and disk files. One hour of stereo 48 kHz PCM
  uses about 659 MiB, plus scene PCM (up to 110 MiB), export and preparation scratch.
- On-air audio buffers 32 bounded FLV packets plus OS pipe buffers. Playback ticks
  emit at most 256 packets and never wait for the audio process.

Delete result removes its generated export, PCM and BRB capture; shared encoded
visuals survive while another result refers to them. Last-use deletion removes the
visual too. Stage selections, current playback and suspended return sources protect
media. Uploaded assets are managed separately. Durable deletion journals cover the
new paths. Interrupted jobs become explicitly interrupted on restart and require
Retry; captured input revisions are preserved.
