# Asterodia branding

The current transparent color icon is supplied by the project owner in
`asterodia-icons.zip`. `asterodia.svg`, `asterodia.ico`, `png/`, and `svg/` retain
the supplied artwork and size variants. Older dark/monochrome files are archival;
they are not used for the current application branding.

Desktop, web, demo and homepage headers display the SVG as an image so its blue,
gold and crystal colors remain intact in both light and dark themes. Favicons
use the same transparent color SVG with the supplied ICO as a fallback.

`build/appicon.png` is the supplied 1024px PNG. `wails3 task common:generate:icons`
generates macOS ICNS and then restores the supplied Windows ICO, preserving all
10 resolutions (16, 20, 24, 32, 40, 48, 64, 96, 128 and 256px). No background is
added. IconComposer/Assets.car background templates are not used.

## Compatibility

The repository, Pages URL, Go module, application identifiers, new database and
browser preference keys use Asterodia. The Google verification file is also
published at the new Pages URL.

On first use, `asterodia.db` is created from a validated SQLite snapshot of the
legacy database, including committed WAL content. The original remains available
for recovery, and an existing new database is never overwritten. Both library
locks are held so an old executable cannot run alongside the new application.
Browser preferences are copied into new keys on first read; existing new values
take priority. The legacy data-directory environment variable is a fallback only.

Release preparation produces `Asterodia-windows-<arch>.exe`. Release verification
adds an identical `StorageBucket-windows-<arch>.exe` alias and checksum so existing
clients can update. The new updater prefers Asterodia assets and also accepts old
releases. Homepage downloads retain the existing release URLs until a new branded
release is published.
