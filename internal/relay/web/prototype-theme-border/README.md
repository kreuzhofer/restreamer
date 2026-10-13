# Throwaway OBS border preview

Question: how does adjusting a transparent opening within an existing theme work?

The user requested one preview before production implementation. The original adjustable rectangular cutout was rejected: only its thin setting was usable; wider cuts broke the artwork. It is retained in `rejected-cutout.html` as a discarded study.

Accepted direction: one purpose-built thin frame per theme, with an irregular, organic inner silhouette. No width slider. The current page shows a new Pac-Man frame made with the built-in image generator using the existing theme as a style reference. Small cables, tube bends and stepped hardware define the inner edge. Checkerboard and sample OBS backdrops are preview-only; the downloadable PNG preserves the generated alpha. Nothing is saved to the application. The user approved the thin v2 asset and requested equivalent individually designed frames for Arcade v2, Neon v2, and the three Maker Workshop themes.

The source asset is `pacman-purpose-built-v2.png`, 1672×941 RGBA. FFmpeg inspection found 84.87% of pixels fully transparent and a zero-alpha center. The initial generation prompt is retained in `purpose-built-prompt.txt`; the thinning pass is recorded alongside the exported asset in the workspace output directory.

```sh
python3 -m http.server 18867 --bind 127.0.0.1 --directory internal/relay/web/prototype-theme-border
```

Open http://127.0.0.1:18867/. This is deliberately isolated on a prototype branch; do not merge it into production. Production integration proceeds separately on feature/theme-downloads.
