# Studio Media verification (#43)

Use the approved authenticated browser/HTTP seam with disposable media and dummy credentials. Preparation and playback continue to use the existing server APIs.

## Browser scenario (written before UI implementation)

1. Open Studio → Media. Uploaded scene assets and uploaded library videos are distinct from designs and generated results. Empty collections and loading/unavailable status are explicit.
2. Upload an image, video and audio asset. Observe byte progress followed by server preparation and a saved immutable revision. An invalid upload displays the validation failure without removing prior revisions. Upload a replacement and review both exact revisions; existing designs keep their chosen revision.
3. Inspect saved reference names/revisions and reject deletion while referenced. Explicitly delete an unused asset, checking its physical files in the disposable fixture.
4. Upload a library MP4 in Media, observe preparation progress/readiness, preview its exact revision, and follow its explicit selection link into Live without changing playback through navigation. Invalid preparation and storage errors remain visible; retry uses the existing endpoint.
5. Delete an eligible uploaded library video separately. Verify its original, prepared copies and owned revision files are physically removed. Reject selected, playing, return and shared media deletion; preserve designs, generated results and unrelated authoring uploads.
6. At desktop 1920×1080 and 1440×900, tablet/narrow 1024×768, and phone 390×844, verify collection navigation, readable status, keyboard focus and no horizontal clipping. Phone permits browsing/preview/selection and explains desktop requirements; uploads, preparation and deletion controls are unavailable.

## HTTP tracer

`TestDeleteLibraryUploadRemovesOnlyItsFiles` failed with HTTP 405 before implementation. The new explicit deletion boundary uses the same transaction/recovery mechanism as generated-media cleanup, while existing asset endpoints retain their contracts. Regression coverage includes selection/playing/return protection, fresh request versions, authentication/origin rejection, storage rollback, and physical files.

## Verification evidence (2026-10-12)

- Authenticated disposable browser fixture `localhost:18843`, dummy `stage-test` credentials: inspected empty asset collection, image/video/audio catalog, exact image revision picker, uploaded-library readiness, exact-revision preview, and explicit selection links. Broadcast remained OFF throughout preparation/browsing.
- Inspected 1440×900, 1024×768, and 390×844 layouts; phone document width and scroll width both 390, preview/selection retained and upload/preparation/deletion hidden. Screenshots: [desktop](screenshots/media-desktop.jpg), [narrow](screenshots/media-narrow.jpg), [phone](screenshots/media-phone.jpg). 1920×1080 was used for the deletion-confirmation attempt below.
- Browser testing caught and fixed a relative-fetch failure with credential-bearing localhost fixture URLs. Requests now resolve against the credential-free origin, as existing generator requests do.
- Browser upload chooser could not set files because the Chrome extension's file URL permission is disabled; no browser security setting was changed. Image/video/audio/library fixtures were uploaded through the real authenticated HTTP endpoints instead. Byte-progress/upload-cancellation UI remains a final browser integration check.
- Browser deletion click opened/timed out around its confirmation, then the CUA session timed out. A successful browser deletion was not verified; final integration must revisit it. Authenticated HTTP tests verify successful deletion and physical original/prepared/revision cleanup, rejected selection/playing/return deletion, stale version and origin/authentication rejection, and storage rollback.
- Full race suite completed in 313 seconds with only the known `TestBRBThemePublisherDisconnectDoesNotStopPreparation` fixture failure: its empty publish key is now correctly rejected by ingestion authentication. Corrected that test to initialize a new fixture server with an explicit dummy key; focused race test passed. The final integration CI gate remains required.
