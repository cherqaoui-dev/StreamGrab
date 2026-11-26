# StreamGrab

StreamGrab downloads the YouTube video in the active tab. A browser extension shows the formats. A local companion host does the download with yt-dlp and FFmpeg and writes the file to disk. Video and audio bytes never pass through the extension.

The behavior spec is `doc/StreamGrab_PRD.md`. This README describes the build that exists today: host 0.3.0, protocol 1, extension 0.1.0.

## What you can do

Open a YouTube watch page and use the popup:

- **Video.** MP4, WebM, or MKV, at 720p, 1080p, 1440p, and 4K when YouTube offers that tier. Codec order defaults to AV1, then VP9, then H.264. Unavailable pairs stay visible and explain why. **Get** starts that row.
- **Audio.** FLAC, MP3, Opus, and M4A. Direct and Best are computed from the real streams. Tags and cover art are embedded. FLAC, MP3, and M4A say that YouTube audio is already lossy.
- **Queue.** One download at a time. Pause, resume, and cancel. The queue survives closing the popup. A network drop retries three times, then pauses with Resume. A failed mux keeps the partial streams for 24 hours and offers Retry mux and Retry as MKV.
- **History.** Recent finished files, Show in folder, and Clear all. Clear removes the list entry, not the file.
- **Settings.** Default container, codec order, max resolution, default audio format, download folder, and yt-dlp auto-update.
- **Keyboard.** Tab reaches every control. Arrows move across tabs, container choices, and quality rows. Enter starts Get. Esc closes Settings.

The popup is 400×600. It uses the dark layout from the StreamGrab design and the system font.

## How it is put together

| Piece | Where | Role |
|---|---|---|
| Chrome extension | `extension/.output/chrome-mv3` | Manifest V3 popup. Chrome id `empedidcmdedncnndnonehnalohohacm`. |
| Firefox extension | `extension/.output/firefox-mv2` | Manifest V2 event page (`background.scripts`). Firefox id `streamgrab@local`. |
| Companion host | `%LOCALAPPDATA%\StreamGrab\streamgrab-host.exe` | Native messaging host `com.streamgrab.host`. Go module `streamgrab/host`. |

The popup talks only to the extension background. The background owns the native port. The host is the source of truth for in-flight jobs. Messages are a 4-byte length plus JSON. Logs go to stderr, not into that stream.

yt-dlp and FFmpeg are started with argument arrays. Only YouTube URLs are accepted.

## Requirements

Windows 10 or later, for the current user. No administrator prompt.

Install these yourself and leave them on your user or system PATH:

- **yt-dlp**
- **FFmpeg**
- **Deno 2.3 or newer**, which yt-dlp uses for YouTube

The host does not bundle Deno or FFmpeg. After a successful update it prefers its own copy of yt-dlp at `%LOCALAPPDATA%\StreamGrab\yt-dlp.exe` and falls back to yt-dlp on PATH. That update downloads the official GitHub release over HTTPS and checks SHA-256. If the check fails, the installed binary stays and Settings shows a warning.

The Windows package is unsigned.

## Install the host

From a built package, unzip `dist/StreamGrab-host-0.3.0-windows-x64.zip` and run `install.ps1`.

From this repo, after the package has been built:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File dist\windows\install.ps1
```

The script copies the host to `%LOCALAPPDATA%\StreamGrab`, writes the Chrome and Firefox native-messaging manifests, and registers them under `HKCU`. It then prints whether yt-dlp, FFmpeg, and Deno were found. Exit code 2 means the host is registered but one of those tools is missing or Deno is older than 2.3.

`uninstall.ps1` removes the host program and the two registry keys. It leaves downloads, the queue, settings, and a managed `yt-dlp.exe` in place.

## Install the extension

Build it first if `extension/.output` is missing. See [Build from source](#build-from-source).

**Chrome**

1. Open `chrome://extensions`.
2. Turn on Developer mode.
3. Load unpacked and choose `extension/.output/chrome-mv3`.

**Firefox**

1. Open `about:debugging`.
2. Choose This Firefox.
3. Load Temporary Add-on and choose any file in `extension/.output/firefox-mv2`.

Quit the browser completely after installing the host, including the Chrome tray icon. A browser that is already running does not see a PATH change, so Deno can be missing until that full restart. Then open a YouTube video and click the StreamGrab icon.

If the popup says the companion is not connected, run the installer again and use Retry. Downloads stay on this machine.

## Where files go

| Data | Location |
|---|---|
| Downloaded videos and audio | `%USERPROFILE%\Downloads\StreamGrab`, unless you pick another folder |
| Queue | `%APPDATA%\StreamGrab\jobs.json` |
| Host settings | `%APPDATA%\StreamGrab\settings.json` |
| Managed yt-dlp | `%LOCALAPPDATA%\StreamGrab\yt-dlp.exe` |

An existing file is not overwritten. The next one is saved as `title (1)`.

## Build from source

The extension needs Node.js. The host needs Go 1.24 or newer. If `go` is not on PATH, the package script uses `%LOCALAPPDATA%\streamgrab-go\go\bin\go.exe`.

```powershell
cd extension
npm install
npm test
npm run build
npm run build:firefox
```

`npm run build` writes `extension/.output/chrome-mv3`. `npm run build:firefox` writes `extension/.output/firefox-mv2`.

The host package, including the installer scripts:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\package-host.ps1
```

That fills `dist/windows` and writes `dist/StreamGrab-host-0.3.0-windows-x64.zip`. `scripts/register-host.ps1` builds that package and runs the installer. It is the developer path. A machine without Go runs `install.ps1` from the zip.

Reload the unpacked extension after a new extension or host build.

## Not in this version

macOS and Linux installers, a signed Windows installer, and bundling Deno or FFmpeg. Also still out: playlists, chapter clipping, browser cookies, drag-to-reorder, and telemetry.
