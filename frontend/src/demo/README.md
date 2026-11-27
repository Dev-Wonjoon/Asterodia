# GitHub Pages playground

The demo uses the released desktop gallery, search, album manager, download queue,
and media viewer. `vite.demo.config.ts` replaces only the API and media URL
adapters. It does not import the native Wails adapter or contact a download engine.

- `npm --prefix frontend run dev:demo` — open `/demo.html` locally.
- `npm --prefix frontend run build:demo` — type-check and rebuild `docs/demo/`.
- `npm --prefix frontend run build:pages` — rebuild the landing page stylesheet.

Commit `docs/demo/` with its sources: GitHub Pages publishes `main /docs` without
running Vite. The build uses relative URLs so it also works under `/Asterodia/`.

All changes live in memory. Reloading or Reset restores the seed. Sample downloads
finish after about seven seconds and append a new image to the Instagram profile
album. Submitted URLs are stored only as mock metadata and are never requested.
Native folder operations and editing/export return a demo availability message.

`demo-public/samples/` contains original SVG illustrations and a silent, eight-second
procedural animation created for this project, distributed under the repository's
MIT license. No external sample media or image service is used.
