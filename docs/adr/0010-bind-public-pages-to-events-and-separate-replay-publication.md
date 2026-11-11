---
status: superseded by ADR-0012
---

# Bind public pages to events and separate replay publication

For [public streaming page targets (#84)](https://github.com/kreuzhofer/restreamer/issues/84), give each streaming event its own persistent identity and viewer URL instead of reusing a channel URL across broadcasts; explicitly finishing the event permanently closes it to new live content, while temporary disconnections or disabled delivery do not finish it. Include optional recording enabled before the event begins and an operator-managed stream archive, with replay published only by an explicit choice and served through the original event URL. This introduces an event and recording lifecycle beyond the existing instance-wide destinations, accepting storage and archive-management complexity so shared links remain tied to the intended event and retained recordings do not automatically become public; exact finish controls, failure recovery, retention, storage, and access-revocation behavior remain to be specified.
