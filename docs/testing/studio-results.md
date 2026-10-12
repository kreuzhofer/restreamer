# Studio generated-results verification

Approved seams: authenticated browser against the real app, authenticated HTTP/status, and existing local RTMP regression tests. All fixtures and media are disposable.

## Scenario written before implementation

1. Open two independent designs with generated results. Results show only the open design's history, newest first. A new design has no history even when other designs have generated output.
2. Keep draft version, latest attempt, last ready output, selected stage revision and actual active revision separately visible. A newer cancelled/failed attempt retains the older ready output and its exact preview.
3. Generate a long draft; navigate to a collection and verify the compact shared queue still shows its captured draft and offers cancellation. Queue work continues across navigation, and retry uses the captured revision.
4. Preview a ready revision, follow the existing explicit activation flow, and generate new content while it is on air. Active revision remains unchanged. Use existing HTTP/local RTMP tests for stale-context, ending restrictions and real file-deletion consequences.
5. Delete an eligible result through history, verify its removal and preserved last-ready choice; protected active/selected media reports an error without disappearing. Reuse physical cleanup tests for shared outputs and uploaded assets.
6. At desktop, narrow desktop and phone widths, inspect readable result identities, keyboard focus, panel scrolling and preview. Phone permits review/selection but no generation/retry/upload/authoring.

## Red observation

The #42 authenticated browser run on 2026-10-12 exposed the baseline defect: a newly saved independent conflict-copy design displayed the original design's ready job in Results & queue. The baseline renders all jobs without a design filter, and has no separate latest-attempt/last-ready summary or collection-level queue.

The initial green inspection also exposed a preview-fit/status issue: the inherited exact player could exceed the center panel at 1920×1080, and its status still said Playing after the two-pass preview ended. The follow-up keeps the player proportional within the results panel and reports finished/paused playback explicitly.

## Green evidence (2026-10-12)

The authenticated disposable app on localhost:18846 verified:

- An independent copy has zero history while the original design retains its own results.
- Version 5 was explicitly selected for PRESTREAM and started in rehearsal. Generating version 6 preserved the selected and active version 5. A stale stage confirmation was rejected; reopening with current context allowed the explicit rehearsal transition.
- Version 7 generation remained visible from Media and could be cancelled there. Returning to the editor showed latest attempt cancelled (version 7), last ready output version 6, and selected/active version 5 separately. History retained captured versions and theme identities.
- Exact version 6 playback reached readyState 4, reported intrinsic 1280×720 video, and advanced time. The fitted player measured 512×288 at a 1440px viewport. Finishing the bounded preview changed its status to Finished without changing broadcast selection.
- Desktop (1920×1080), narrow desktop (1024×768), and phone (390×844) had no horizontal document overflow. Phone exposed review and a ready exact player while authoring controls had no rendered rectangles. Screenshots show reviewed output rather than a loading player; the fixture restart before capture explains the OFF/None active state in the images.

Screenshots: [desktop](screenshots/studio-results-desktop.jpg), [narrow desktop](screenshots/studio-results-narrow.jpg), [phone](screenshots/studio-results-phone.jpg).

Focused race tests passed: `TestGeneratorCancellationAndFailurePreserveReadyRevision`, `TestGeneratorFailedRetryRetainsCapture`, `TestDeleteGeneratedResultKeepsSharedOutputUntilLastOwner`, `TestDeleteGeneratedResultProtectsPlayingAndReturnMedia`, and `TestGeneratedEndingCandidateSurvivesStaleSelectionCompletionAndSessions`. These exercise cancellation/failure retention, retry capture, physical shared-file cleanup, playing/return protection, and exact ending revisions through existing public HTTP/local RTMP seams. `make check-web` (5 tests), `go vet ./...`, the application build, JavaScript syntax, and diff whitespace checks passed.

The browser's native deletion confirmation stalled the browser transport. The dialog was recovered, but that browser deletion outcome is not claimed as verified. Protected deletion and physical cleanup are covered by the passing HTTP/file regression tests above. Root owns the final combined integration checks and release verification.
