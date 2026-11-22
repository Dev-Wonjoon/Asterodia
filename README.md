# StorageBucket

Download, organize, and edit your media in one place.

StorageBucket is a desktop application for managing images, videos,
and audio. Download media from supported URLs, register files already
on your PC, and organize your library with tags and favorites.

[한국어](./README.ko.md) ·
[Downloads](https://github.com/Dev-Wonjoon/StorageBucket/releases) ·
[Report an Issue](https://github.com/Dev-Wonjoon/StorageBucket/issues)

<!-- Add screenshots of the StorageBucket 2.0 interface here. -->

## Features

### Download Media
- Download media using yt-dlp and gallery-dl.
- Track download progress, inspect logs, and retry failed tasks.
- Download HLS and DASH streams.
- Configure browser cookies or a Netscape-format cookie file when authentication is required.

Supported sites and available formats depend on the external download
engines. Some sources may require authentication or may be unavailable.

### Organize Your Library
- Register images, videos, and audio from local folders and subfolders.
- Organize media with tags and favorites.
- Search by title, tag, author, or platform, with search suggestions.
- Apply actions to multiple library entries at once.
- Move files to registered folders.
- Automatically generate missing thumbnails.

Local folder imports register files in their existing locations without
copying or moving the originals.

### Gallery Pagination Rules

Gallery pagination in both the desktop app and the web interface must use
**infinite scrolling**. Load the next batch near the end of the list and append
it to the existing items. Do not replace this with page numbers, page selectors,
or first/previous/next/last page navigation controls. Provide a **Load more**
button as a fallback and a **Retry** button when loading fails.

Files from expanded albums share the same gallery feed with other albums.
Preserve the album's position on screen when expanding or collapsing it, and
fetch items incrementally.

### User Albums and Media Viewing

- Create an album with **새 앨범** and add selected groups or individual files from the gallery or detail view.
- User albums reference originals across folders. A file can belong to multiple albums; removing a membership or deleting an album keeps the originals.
- **앨범 편집** supports renaming, removing members, and drag or button reordering. Files initially follow addition order.
- **감상하기** opens an image viewer, the shared video player, or an audio player. Arrow keys and previous/next buttons navigate within the album; Esc closes it, stops playback, and preserves the gallery position.
- Auto next, shuffle, and album repeat are independent options, all off by default. Auto next advances finished videos and audio; images remain manual. Shuffle avoids duplicate items within a cycle and reshuffles for repeat.
- **한 폴더에 모으기** copies or moves files to a registered folder. Copy is the default and preserves original references; rescan the destination to register copies. Moves verify the copy before cleaning the source and retain file IDs across all albums. Filename collisions receive a numeric suffix.
- Schema 11 adds user albums and durable copy/move modes. Existing Wails databases migrate after a backup; Electron databases remain unsupported.

### Edit Media
- Create GIFs and animated WebP images from videos.
- Edit existing GIFs.
- Trim video clips and remove audio.
- Select ranges, crop, exclude frames, and preview output quality.
- Save edited results while preserving the original files.

### Access from a Browser
- Browse, search, and play media through the local web server.
- Access your library from mobile devices through a separately configured Tailscale connection.
- Submit and monitor editing jobs from mobile when mobile editing is enabled on the PC.

The desktop app must remain running. Media playback depends on the
formats supported by your browser.

## Getting Started

1. Download a release from the [Releases page](https://github.com/Dev-Wonjoon/StorageBucket/releases).
2. Launch StorageBucket.
3. Follow the engine setup prompt to install the required external tools.
4. Choose a download folder in Settings.
5. Add a media URL or import a local folder to start building your library.

External tools can also be installed or updated from Settings → Engine Management.

## External Tools

StorageBucket uses external tools for downloading, inspecting, and processing media.

| Tool | Purpose |
| --- | --- |
| yt-dlp | Video and audio downloads, including HLS and DASH |
| gallery-dl | Media downloads from supported galleries and social platforms |
| FFmpeg | Media processing, conversion, and video thumbnail extraction |
| FFprobe | Video and audio metadata inspection |
| ImageMagick | Image validation and previews |

Installing or updating these tools requires an internet connection.

## Importing a Wails Database

Use Settings → General Settings → Import Wails Database to merge a
StorageBucket Wails database into your current library.

**Starting with 2.1.0, automatic conversion and import of Electron databases are
no longer supported.** Older Electron-only formats (schema versions 0–2) are
rejected without modifying the source. Already converted Wails databases and
upgrades from Wails schema version 3 onward remain supported.

- A database backup is created before merging.
- Import brings over library records and history; it does not copy or move media files.
- Keep existing media files in their original locations.
- Imported download history is not automatically executed.

## Browser and Mobile Access

Start the local web server from Settings → Network Settings and open
the displayed local address in a browser on your PC.

For mobile access, configure Tailscale access to the local server separately.
Enable “Allow Mobile Editing” on the PC if you want to submit editing jobs
from a mobile device.

The server starts disabled each time the app launches and stops when
the app closes.

## App Updates

Starting with 2.1.0, Windows portable builds check for stable GitHub releases
after launch and every six hours while the app is running.
Open Settings → General Settings → App Updates to check manually,
review release notes, download an update, and restart to apply it.

- Downloads are verified against the release's SHA256SUMS file before they are staged.
- Restart is blocked while downloads, imports, file moves, editing, cleanup, or engine installation are in progress.
- Updates replace the app executable and preserve the database, settings, media, and external tools.
- The app's folder must be writable. Updating an installation in a protected folder may require a manual installation.
- Versions without the updater, including 2.0.0, require one manual upgrade to 2.1.0 or later.

### Preparing an Update Release

On Windows, build the release assets with:

```powershell
.\tools\prepare-release.ps1 -Arch amd64
```

The script reads the version from `build/config.yml`, builds the application,
and creates `StorageBucket-windows-amd64.exe` and `SHA256SUMS`
under `dist/releases/v<version>/`.
Upload both files to the same draft GitHub Release, use a matching `v<version>`
tag, and publish it as a full release once all assets are ready.
The updater selects the exact executable for its CPU architecture; installer
files and archives are not used as update payloads.

## Development

StorageBucket 2.0 uses Wails v3 and Go for the desktop backend,
React and TypeScript for the interface, and SQLite for local data storage.

Prerequisites:
- Go 1.26 or later
- Node.js and npm
- Wails v3 CLI and its platform build dependencies

Run in development mode:

```sh
wails3 task dev
```

Build the application:

```sh
wails3 build
```

The current application lives in the repository root and `frontend/`.
The archived Electron implementation and its database conversion assets have
been removed starting with 2.1.0. Database tests cover rejection of Electron
databases, Wails database upgrades and imports, and preservation of source data.

## Responsible Use

Use StorageBucket only with media you own or have permission to download
and manage. You are responsible for complying with copyright laws,
platform terms, and content licenses.

StorageBucket is not intended for DRM circumvention or unauthorized redistribution.

## License

StorageBucket is licensed under the [MIT License](./LICENSE).

Third-party libraries and external tools are covered by their respective
licenses. See [Third-Party Notices](./THIRD_PARTY_LICENSES.md) or view the
license notices in the app.
