# UX update: state and rollback

Stop the broadcast, stop the service, and make a consistent backup of the whole
persistent data volume before upgrading. Include BRB storage, library originals
and retained revisions, generator designs/jobs/assets/themes/templates, playlists,
stage selections, and managed configuration files. Keep the exact old image digest
or binary together with its configuration. Protect backup credentials as you do
the running installation.

The new reader accepts stage-media schema 1 and 2. Existing schema-1 selections
stay unchanged on startup. A distinct legacy fallback remains labelled as retained
fallback in Show setup; it is not replaced by the deliberate BRB selection.
Choosing shared or override fallback explicitly writes schema 2. Existing legacy
fallback APIs retain their compatibility behavior. Default legacy BRB encoding on
startup follows the pre-existing behavior; the UX migration does not generate new
show designs or videos. A restart always remains OFF.

New BRB designs use the existing design store with stage `brb`. Their generation
jobs capture appearance and music together. Friendly video names are optional job
metadata; renaming never changes a media revision. Current and suspended media
remain protected independently from next-use selections.

Do not point an older binary at newer writable state: older readers may reject
schema 2 or BRB designs. To roll back, stop the new service, retain a copy of its
state for recovery, restore the complete pre-upgrade backup into a separate volume,
and run the saved old image/binary against that restored volume and configuration.
Verify health, login, selections and previews while OFF. Rehearse in preview-only
mode before deliberately starting real delivery. Restoring only JSON without its
referenced media files is insufficient. Changes created after the backup are not
present in that restored installation.

Verification records belong in the implementation review report. Automated tests
cover legacy selection preservation, repeat startup, managed fallback restart and
selected/active/suspended fallback deletion protection. They do not replace a
backup of the installation being upgraded.
