# StreamGrab

Download the YouTube video in your active tab. A browser extension shows the available formats, and a local companion program does the download with [yt-dlp](https://github.com/yt-dlp/yt-dlp) and FFmpeg. Video and audio data never pass through the extension.

| | |
|---|---|
| Platform | Windows 10 or later, current user only (no administrator prompt) |
| Browsers | Chrome (Manifest V3), Firefox 128 or later (Manifest V2, temporary add-on) |
| Versions | Host 0.3.0, extension 0.1.0, protocol 1 |

This README describes what the code does today. The intended behavior is specified in [`doc/StreamGrab_PRD.md`](doc/StreamGrab_PRD.md).

## Contents

- [Features](#features)
- [Requirements](#requirements)
- [Quick start](#quick-start)
- [Using StreamGrab](#using-streamgrab)
- [Files and data](#files-and-data)
- [Updating and uninstalling](#updating-and-uninstalling)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Known issues](#known-issues)
- [Not in this version](#not-in-this-version)

## Features

- **Video:** MP4, WebM or MKV at 720p, 1080p, 1440p and 4K, when YouTube offers that tier. Codec preference is configurable (default AV1, then VP9, then H.264). A tier that can't be saved in the chosen container stays visible and says why. MP4 and MKV files get the video's thumbnail embedded as cover art; WebM can't hold one this way.
- **Audio:** FLAC, MP3, Opus and M4A, with embedded tags and cover art. Each option is based on the streams the video actually has. YouTube audio is lossy, so converting it, including to FLAC, adds no detail.
- **Queue:** one download at a time, with pause, resume and cancel. It survives closing the popup. Pausing the current download holds the queue until you resume or cancel it.
- **Recovery:** network failures are retried three times and then paused. A failed video mux can be retried in the same container or as MKV, and a failed audio conversion can be retried.
- **History:** recent downloads with *Show in folder* and *Clear all*. Clearing never deletes files. The host keeps the 200 most recent finished, failed and cancelled jobs.
- **Settings:** container, codec order, maximum resolution, default audio format, download folder and yt-dlp auto-update.
- **Keyboard:** Tab and arrow keys move through the popup, Enter starts a download, Esc closes Settings.

Watch, Shorts, live and `youtu.be` URLs are recognized. Streams that are live at the moment, DRM-protected videos and videos that need a signed-in age check can't be downloaded.

## Requirements

**To run it**, have these on your user or system `PATH`:

| Tool | Notes |
|---|---|
| [yt-dlp](https://github.com/yt-dlp/yt-dlp) | The host can later keep its own updated copy |
| [FFmpeg](https://ffmpeg.org/download.html) | Merges and converts streams |
| [Deno](https://deno.com/) 2.3 or newer | Used by yt-dlp for YouTube |

**To build it**, you also need:

- [Node.js](https://nodejs.org/) 20.12 or newer
- [Go](https://go.dev/dl/) 1.24 or newer

The repository contains source only. The extension and the host are built on your machine, and the host is unsigned.

## Quick start

All commands are PowerShell, run from the project root.

**1. Clone** (the repository is private, so you need access):

```powershell
git clone https://github.com/cherqaoui-dev/StreamGrab.git
cd StreamGrab
```

**2. Build the extension:**

```powershell
cd extension
npm install
npm run build
cd ..
```

**3. Build and install the host:**

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\register-host.ps1
```

This builds the host, copies it to `%LOCALAPPDATA%\StreamGrab` and registers it for Chrome and Firefox. If it reports that yt-dlp, FFmpeg or Deno is missing or too old, install that tool and run the command again.

**4. Restart the browser.** Quit it completely, including the tray icon, so it picks up `PATH` changes.

**5. Load the extension.**

- **Chrome:** open `chrome://extensions`, turn on *Developer mode*, choose *Load unpacked* and select `extension\.output\chrome-mv3`.
- **Firefox:** run `npm run build:firefox` in `extension`, open `about:debugging`, choose *This Firefox*, then *Load Temporary Add-on* and pick any file in `extension\.output\firefox-mv2`. Firefox removes it when it closes.

**6. Download.** Open a YouTube video, click the StreamGrab icon and choose **Get**.

### Installing without Go

Build the package on a machine that has Go:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\package-host.ps1
```

Copy `dist\StreamGrab-host-0.3.0-windows-x64.zip` to the target PC, unzip it and run `install.ps1`. The package holds the host only; build the extension separately and copy its output folder across.

`install.ps1` exits with code 2 when the host is registered but a required tool is missing or too old.

## Using StreamGrab

- The popup follows the tab it was opened on. Video details are looked up only while the popup is open.
- **Get** adds the download to the queue. Asking for the same format twice shows the existing job.
- **Cancel** discards the download's partial files. *Remove from queue* then takes it off the list.
- Closing the browser stops the current download. It is offered for resume next time.
- Changing the download folder affects new downloads. Started ones stay where they are.
- StreamGrab refuses a download when free space is below 1.2 times its estimated size.
- An existing file is not overwritten. The next copy is saved as `title [videoID] (1)`.

## Files and data

| Data | Location |
|---|---|
| Downloads | `%USERPROFILE%\Downloads\StreamGrab`, or the folder you choose |
| Queue | `%APPDATA%\StreamGrab\jobs.json` |
| Host settings (folder, auto-update) | `%APPDATA%\StreamGrab\settings.json` |
| Host program | `%LOCALAPPDATA%\StreamGrab\streamgrab-host.exe` |
| Managed yt-dlp | `%LOCALAPPDATA%\StreamGrab\yt-dlp.exe` |
| Format preferences and popup cache | Browser extension storage |

**Partial files.** A failed mux keeps its partial streams. Cancelling or removing a job deletes the files that job wrote. When the host starts, it also removes partial files older than 24 hours only if their name carries the `[videoID]` of a finished, failed or cancelled job, and no other job for that video is still pending. Other files are left alone.

**Damaged state files.** If `jobs.json` can't be parsed, it is renamed to `jobs.json.corrupt-<time>` and the queue starts empty. `settings.json` is set aside the same way the next time settings are saved.

**yt-dlp updates.** With auto-update on, the host checks at most once a day. It downloads the official GitHub release over HTTPS and verifies its SHA-256 checksum. If the update fails, the installed version is kept and a warning appears in Settings. The managed copy is preferred over the one on `PATH`.

## Updating and uninstalling

After pulling new code, quit the browser, then run from the project root:

```powershell
cd extension
npm install
npm run build
cd ..
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\register-host.ps1
```

Open the browser and reload the extension.

To uninstall, run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File installer\windows\uninstall.ps1
```

This removes the host program and its browser registration. Downloads, the queue, settings and the managed yt-dlp stay. Remove the extension from the browser's extension page.

## Troubleshooting

| Problem | What to do |
|---|---|
| Popup says the companion is not connected | Run `register-host.ps1` (or `install.ps1`) again, then choose *Retry* |
| Deno shows as not found | Quit the browser completely, including the tray icon, and reopen it |
| A video can't be read | In Settings, choose *Update now*, and check that Deno is 2.3 or newer |
| `npm run build` fails with `EBUSY ... rmdir` | Quit the browser and close any window or terminal open inside `extension\.output`. To find the holder, run `resmon`, open *CPU*, and search *Associated Handles* for the folder name |
| `go` is not recognized | Install Go and open a new terminal. The package script also looks for `%LOCALAPPDATA%\streamgrab-go\go\bin\go.exe` |
| Windows warns about the host | Expected, because the build is unsigned |

Settings has a **Copy diagnostics** button with the host status, tool versions and job errors.

## Development

| Path | Contents |
|---|---|
| `extension/` | WXT, Preact and TypeScript extension |
| `host/` | Go native messaging host (module `streamgrab/host`) |
| `protocol/fixtures/` | Sample messages used by tests |
| `installer/windows/` | Install and uninstall scripts |
| `scripts/` | Packaging and developer scripts |
| `doc/` | Product requirements |

### How it fits together

```
popup  <-->  extension background  <-- native messaging -->  host  -->  yt-dlp / FFmpeg  -->  disk
```

- The popup talks only to the background, which owns the native port.
- The host (`com.streamgrab.host`) is the source of truth for jobs.
- Each message is a 4-byte length followed by JSON on stdin and stdout. Logs go to stderr.
- External tools are started with argument arrays, and only YouTube URLs are accepted.
- Extension IDs are `empedidcmdedncnndnonehnalohohacm` (Chrome) and `streamgrab@local` (Firefox). The installer registers these.

### Commands

In `extension`:

| Command | Purpose |
|---|---|
| `npm run dev` | Development build with reload |
| `npm run typecheck` | TypeScript check |
| `npm test` | Unit tests (Vitest) |
| `npm run build` | Chrome build in `.output/chrome-mv3` |
| `npm run build:firefox` | Firefox build in `.output/firefox-mv2` |

In `host`:

```powershell
go test ./...
```

## Known issues

- Chrome and Firefox each start their own host. Running both at once lets them overwrite each other's queue.
- Downloads that were in the queue before this version don't record their files, so cancelling one leaves its partial files behind.

## Not in this version

macOS and Linux installers, a signed Windows installer, bundled Deno or FFmpeg, playlists, chapter clipping, browser cookies, drag-to-reorder and telemetry.
