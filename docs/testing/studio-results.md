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
