# Asterodia branding

`asterodia.svg` is the original transparent, white Asterodia symbol supplied by
the project owner. Application icons are generated from this source with no
background. The dark background variant is not used.

Desktop, web and demo headers render the transparent shape in the current theme's
foreground color. The browser favicon uses the same shape with a light/dark
foreground selected by the browser's color scheme.

Windows ICO and macOS ICNS are generated from `build/appicon.png` by
`wails3 task common:generate:icons`. PNG and ICO copies in the frontend and docs
must retain transparency. IconComposer/Assets.car background templates are not used.

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
