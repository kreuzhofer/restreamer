# Brand packages

On desktop, in **Library → Brand packages**, choose a ZIP, review its contents, then import it. Use an included content template to create an independent show design. Copies include the template’s current saved edits while retaining the package’s exact theme revision. On phones, inspect installed kits and preview/download their themes; importing and authoring remain desktop tasks. Importing never changes stage selections or on-air media. The Themes page previews scene samples and downloads the package’s transparent OBS frame.

Packages are curated kits, not arbitrary folders of brand guidelines. A designer must select the media, define layouts and provide the relationships below. The generic importer ships with Restreamer; proprietary corporate kits stay outside its repository and container image.

## Build a sample

```sh
python3 tools/build-example-brand-package.py /tmp/example.restreamer-brand.zip
```

This uses only the Python standard library and generates original example artwork, a theme and three editable templates. It needs no internal brand files. Import the resulting ZIP through the authenticated dashboard.

## Format version 1

The ZIP contains `manifest.json` at its root and the files named by its assets. Use regular file entries only, relative paths, no enclosing folder, directory entries, symlinks, duplicate paths, scripts or undeclared files. ZIP metadata and file ordering do not affect package identity.

```json
{
  "format_version": 1,
  "id": "example-studio",
  "version": 1,
  "name": "Example studio",
  "notes": "Usage notes for the operator",
  "assets": [
    {"id":"logo","name":"Studio logo","kind":"image","path":"assets/logo.png","sha256":"<64 lowercase hex digits>","source":"Original artwork"}
  ],
  "themes": [
    {"id":"primary","name":"Studio primary","style":"<style object; see sample builder>"}
  ],
  "templates": [
    {"id":"welcome","name":"Starting soon","theme":"primary","content":{"stage":"prestream","scenes":[{"id":"title","layout":"title","text":"Starting soon","duration_seconds":8}]}}
  ]
}
```

Package and local object IDs use lowercase letters, digits and hyphens, up to 64 characters; the first character must be a letter or digit. Package versions are integers from 1 to 1,000,000. Local identities must be unique within their category. Names use the existing 180-byte asset/name limit. Notes allow 4096 bytes and each asset’s optional source note allows 2048 bytes. Unknown fields and unsupported format versions are rejected: format version is the compatibility contract, not the application release number.

References inside styles and content use `{"id":"local-asset-id","revision":1}`. Templates refer to a local theme ID separately from their content. Import resolves these into immutable stored asset/theme references. Do not embed server paths, URLs as downloadable dependencies, or references to another instance’s asset IDs.

Supported asset kinds:

| Kind | Accepted input |
|---|---|
| `image` | PNG/JPEG, up to 10 MiB and 20 megapixels; normalized to PNG with transparency preserved |
| `video` | H.264 MP4 with optional mono/stereo AAC; up to 1080p60, 10 minutes and 512 MiB |
| `audio` | Existing music-upload formats and limits (MP3/PCM WAV, up to 32 MiB/10 minutes); normalized by the music importer |
| `font` | Parseable OpenType/TrueType, up to 4 MiB; WOFF and WOFF2 are not accepted |

Checksums refer to the original packaged bytes, before normalization. Only include fonts/media you have permission to share with the intended installation. Importing does not grant distribution rights.

## Theme extensions

Styles otherwise use the normal versioned theme schema. The sample builder contains a complete base style.

- `font: "brand-font"` with `font_asset` selects a packaged font. Validation and rendering use the same parsed font metrics. Other fonts retain their existing behavior.
- `motion: {"asset":{"id":"loop","revision":1},"cycle_seconds":8}` selects a video background. It must be at most 64 MiB, have a whole-second cycle of 1–120 seconds and match that declared duration within 0.04 seconds. It is contained within the output frame, preserving aspect ratio; uncovered areas use `background_color`. It repeats continuously across scene boundaries. It cannot be combined with built-in artwork or another decorative effect. Source audio is always muted; use soundtrack selection for music. Prepare an actually seamless master: the importer validates duration, not artistic continuity.
- `background` may provide a still poster for the motion theme. The draft’s frame preview uses actual video frames; static theme samples use this poster.
- `obs_border` points to a purpose-designed transparent PNG, available through Themes → OBS frame. Supply a 1920×1080 image with a large transparent opening.
- `logo_inset_percent` sets the vertical logo inset to 1–10% of frame height; omitted/zero preserves the existing 1% default. Logo size, corner and content-region checks remain in effect.

Templates support existing title/list/image/video content, timing and single-track soundtrack references. Package playlists are not supported in format 1. BRB templates use stage `brb`. Starting soon and Ending scenes can use `animation_loops` with a motion theme; BRB uses the selected theme’s full cycle. Editing templates and themes never rewrites existing designs or renders.

## Limits, updates and removal

The archive, total expanded input and total normalized asset output are each bounded to 512 MiB; at most 256 archive entries and a 256 KiB manifest are accepted. Existing asset/theme/template count limits also apply. Preparation checks available disk space, runs in the shared preparation admission queue and has a 15-minute timeout. Import staging is not visible to library readers.

Reimporting an identical package/version is a no-op. Different contents under an installed version are rejected; publish a new version. Each package version installs independent identities, so existing designs retain the exact version they selected. A new template copy selects the theme included with that package version.

Package-owned assets, themes and templates cannot be deleted individually while their kit is installed. Use **Review removal** in Brand packages first.

**Review removal** lists unused resources to delete and edited/referenced resources to retain. Removing a package never deletes a design or render. Its unused original templates/themes/assets are removed together with the package entry; modified resources and anything referenced outside the removed kit remain. A retained object identity may prevent reinstalling that removed version; use a new package version rather than overwriting it.

Imports record a recovery journal before publishing content and write the installed manifest last. Startup rolls back an interrupted uncommitted import before loading assets/themes. Package removal uses the existing transactional media-deletion journal. Keep the complete persistent library in backups, including `generator/packages`.

Export, remote package downloads/updates and executable theme plugins are intentionally outside this first version.
