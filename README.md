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

## Upgrading from 1.x

Use Settings → General Settings → Import Database from an Earlier Version
to merge an existing database into your current library.

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
The previous Electron implementation is preserved in `legacy/electron/`.

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