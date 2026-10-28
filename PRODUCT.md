# Restreamer
<!-- impeccable:product-schema 1 -->

## Platform
web

## Users and purpose
The operator prepares and runs a livestream, returning periodically rather than using every authoring feature daily. Success means recognizing where to prepare content and operate the broadcast without coaching. See the approved [UX plan](docs/design/ux-simplification-plan.md) for authoritative requirements and interview decisions.

## Operating context and constraints
Full-window desktop production workspace; phone for operation and ready-media browsing/selection. Keep independent stream delivery, authenticated access, immutable prepared selections and explicit active replacement. Designs and generated files are distinct from separately managed uploads. Prestream/Ending use scene sequences; BRB uses animated themes. Follow-music playback loops independently until explicitly stopped.

## Principles
Make navigation and actions distinguishable. Reveal infrequent controls when relevant. Preserve current playback while preparing next-use content. Show failures and recovery explicitly. Use human-meaningful names and previews.

## Evidence
Existing app source and isolated test fixture; user-approved UX plan and ADRs 0007/0008. Prototype uses synthetic names and state; artwork comes from prior project previews. No real broadcast or production mutation is part of the prototype.

## Accessibility
Keyboard, visible focus, semantic controls, touch targets, zoom/reflow, reduced motion and real-phone playback verification are required by the approved plan.
