# Final production studio review resolutions

Reviewed integration: `abd9cfe..2b05e41`. Standards and specification were reviewed independently. All three P2 findings were reproduced with failing regressions before fixes.

## Standards

**A host replacement could be deleted using a cached original version.** Library deletion now stages the transaction, then checks the staged original against the discovery identity, pre-stage identity and reviewed content/version before writing the commit marker. A mismatch rolls the whole transaction back and returns a conflict; rollback failure remains a storage error with restart recovery. The authenticated regression replaces or modifies a host original without rescanning, preserving its byte count and modification time, and proves that rejected deletion restores the file and retains the catalog entry. It also covers a different file with identical content, preventing identity changes from escaping the check. Existing selected/playing/return-media protection, storage-failure and recovery tests remain green.

**Symlinked parent directories could alias secret and non-secret stores.** Initialization now resolves the deepest existing ancestor and appends missing path components before comparing storage paths. This supports fresh files while rejecting state/ingestion/destination aliases before any write. The startup regression covers all three pairs through a symlinked parent and verifies that rejection creates no files. Relative/absolute alias coverage remains in place.

## Spec

**Invalid destination ports could be persisted as ready.** Shared target readiness now validates explicit ports as numeric values in 1–65535, including rejection of leading-zero representations of zero, overflow and an empty explicit port. Authenticated POST and PUT regressions verify rejection without changing the previously saved destination or store version, including after restart. Existing valid destinations and independent output delivery continue to pass.

## Validation

- Focused race suite passed: configuration 1.409s and relay 10.545s, covering destination management/delivery, managed ingestion, storage-path aliases, library/design/generated-result deletion and protection.
- `go vet ./...` and `git diff --check`: passed.
- No browser-visible behavior changed in these backend fixes; prior desktop/narrow/phone evidence is in [assembled acceptance](production-studio-final.md).
- The release coordinator additionally completed the previously blocked native upload-picker journey on the disposable localhost fixture: selected a copy of the approved concept PNG with the native Open dialog, submitted the real upload form, observed ready revision 1 and decoded its exact 1512×838 preview. A synthetic 1280×720, two-second MP4 was then selected through the same native picker and uploaded to Media. Its state advanced from discovering to ready; Preview exact revision plus native keyboard playback decoded `readyState=4` and reached 1.984 seconds at 1280×720. Stage stayed OFF throughout. No browser permission was changed.

Exact final-SHA GitHub CI and main image publication remain the release coordinator's delivery gate.
