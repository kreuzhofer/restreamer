# Purpose-designed OBS frames

Six thin pixel-art frames with individual irregular inner contours and real alpha transparency. Source PNGs live in `internal/relay/artwork/obs-frames/` and are embedded in the server. Generated with the built-in image-generation tool, using existing theme artwork as a style reference. Full prompts are in `prompts.json`.

The approved Pac-Man prototype is archived on `prototype/theme-border` (commit `46b808e`). The user rejected adjustable rectangular cutouts and approved one fixed, thin, individually designed frame per artwork. The five companion frames extend that decision to Arcade After Hours r2, Neon Night r2 and Maker Workshop normal/Christmas/Halloween.

Sources are 1672×941 RGBA. The export renderer scales with nearest-neighbor sampling to 1920×1080 and clears only alpha values of 1/255 or less (invisible generator noise). It preserves the authored inner contour. Frames use fixed artwork colors; scene samples use the current editor settings, uploaded backgrounds and logos. Custom variants with built-in artwork share its frame; styles without it report that a frame is unavailable.

Regression coverage in `internal/relay/theme_exports_test.go` decodes every export, verifies dimensions, a clear central area, a mostly transparent canvas, scene opacity, validation, authentication, unchanged saved themes and bounded concurrent previews.
