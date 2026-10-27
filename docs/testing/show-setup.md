# Show setup and readable renders

The approved concept brings Prestream, BRB and Ending assignments into Studio's
landing page, linked directly from Live. Authoring stays in a dedicated editor
with shared stage navigation. Existing playback and exact-revision activation
contracts are retained.

Generated results use stable queue sequence numbers, timestamps, captured-change
summaries and retained-video thumbnails. Deleting older results does not renumber
survivors. Technical hashes remain available under Details. Music is a compact
list with duration, uses, preview and existing protected deletion.

BRB retains its existing bounded active/candidate model, explicitly described in
the UI. Optional text and volume inputs on theme preparation capture edits without
activation; omission preserves the existing API behavior. File uploads and legacy
resets retain their separately labelled prepare-and-activate action. Playlists and
full multi-design BRB history are outside this UX change.

## Validation

- Node boundary tests cover stable render labels, snapshot change summaries,
  grouping, and preserving unrelated selections.
- The authenticated HTTP regression prepares changed BRB text and volume,
  verifies the active generation is unchanged, and rejects invalid draft inputs.
- Authenticated localhost fixture with no destinations: generated two revisions
  of a Workshop Prestream and a Neon Ending, and uploaded ten silent WAV tracks.
- Browser verified exact-render assignment, retained assigned/latest distinction,
  dedicated editor routing and history, ten music rows, BRB prepare-only with the
  previous screen unchanged, and exact candidate video loading before activation
  became available. No real broadcast or destination was used.
- Desktop, 1024×768 and 390×844 inspected. Phone document width was 390px for
  Show setup and Music; authoring remains desktop-only. Desktop action buttons fit
  below all three preview cards. Browser reported no JavaScript errors.
- Required delivery evidence is recorded in the final commit's GitHub CI run.
