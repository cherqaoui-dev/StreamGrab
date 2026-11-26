# StreamGrab — Product Requirements Document

| | |
|---|---|
| **Product** | StreamGrab, a pro-utility browser extension for downloading, inspecting, and extracting YouTube media |
| **Platforms** | Chrome (MV3) and Firefox (MV3), plus a local companion host (Windows, macOS, Linux) |
| **Document status** | Draft v1.0 for engineering and design review |
| **Design source of truth** | Google Stitch project ID `2139836994187367589` (accessible via the `google-stitch` skill) |
| **Priority legend** | **P0** = launch blocker · **P1** = required for a quality v1.0, can slip one patch · **P2** = fast follow / nice-to-have |

> **Design reference note.** All UI is already designed in the Google Stitch project above. This PRD describes behavior and requirements; it does **not** redefine layout, color, or typography. Where this document and Stitch disagree on visuals, Stitch wins; where they disagree on behavior, this PRD wins and the discrepancy should be raised in design review. Component names below (Header, Detection Card, Mode Tabs, Active Task Card, Utilities Footer) mirror the designed screens.

---

## 1. Product Overview & Goals

### 1.1 Problem Statement
Power users who archive, edit, or study YouTube content currently juggle CLI tools (yt-dlp, FFmpeg), web-based converters (ad-ridden, capped at low quality, privacy-hostile), and generic downloaders (no adaptive-stream merging, no 4K/HDR). The result is slow, error-prone workflows and lost fidelity.

### 1.2 Value Proposition
StreamGrab puts the full power of yt-dlp and FFmpeg one click away from the YouTube tab the user is already on:

- **Highest-fidelity downloads:** adaptive video+audio muxing up to 4K 60fps HDR.
- **Transparent inspection:** codecs, bitrates, containers, and estimated sizes are shown *before* download.
- **Local-first and private:** all extraction and muxing run on the user's machine; no cloud proxy, no account, no tracking by default.
- **Utility-grade density:** a compact 400×600 popup that rewards repeat use.

### 1.3 Target Users

| Persona | Needs | Key features |
|---|---|---|
| **Archivist / Researcher** | Preserve content at maximum quality with metadata | 4K/HDR, MKV, chapters, metadata embedding, batch queue |
| **Video Editor / Creator** | Source footage, reference clips, B-roll in edit-friendly formats | Codec/container control, MP4/MKV, 60fps, predictable sizes |
| **Audiophile / DJ / Musician** | Extract audio with tags and cover art | FLAC/MP3/Opus/M4A, ID3 + cover embedding |
| **Offline Learner / Commuter** | Download lectures and playlists for offline viewing | Queue, playlist batching (v1.1), disk-space awareness |

### 1.4 Goals & Non-Goals

**Goals**
1. One-click download of the active YouTube video at the best available quality in the chosen container.
2. Reliable audio extraction with tag and cover embedding.
3. Trustworthy queue/history management with clear, recoverable error states.
4. Cross-browser parity (Chrome and Firefox) from a single codebase.

**Non-Goals (v1.0)**
- Bypassing DRM, paywalls, or membership-gated content.
- Downloading from sites other than YouTube (architecture should not preclude it later).
- In-extension video editing beyond chapter clipping (v1.1).
- Cloud sync, accounts, or hosted services.
- Mobile browsers.

### 1.5 Success Metrics

| Metric | Target (90 days post-launch) |
|---|---|
| Download success rate (started → file saved) | ≥ 95% |
| Time from popup open to first "Get" click (median) | ≤ 5 s |
| Popup first meaningful paint | ≤ 300 ms (p95 ≤ 500 ms) |
| Companion host install completion (installed → first successful connection) | ≥ 80% |
| Weekly active retention (week 4) | ≥ 40% |
| Crash/unhandled error rate per 1,000 sessions | < 2 |
| Store rating (Chrome Web Store / AMO) | ≥ 4.4 |
| Support tickets attributable to "host not connected" | < 10% of all tickets |

Metrics are measured via opt-in, anonymous, local-first telemetry (see §4.3), or via store-level analytics where telemetry is off.

---

## 2. Architecture & Technical Constraints

### 2.1 System Overview

```
┌──────────────────────────── Browser ─────────────────────────────┐
│  Popup UI (400×600)  ◄──msg──►  Background (SW / Event Page)     │
│       ▲                                │          ▲              │
│       │                                │          │              │
│  Content Script (YouTube tab)          │   chrome.storage        │
│  (video ID, playlist, player state)    │                         │
└────────────────────────────────────────┼─────────────────────────┘
                                         │ Native Messaging (stdio JSON)
                        ┌────────────────▼─────────────────┐
                        │   StreamGrab Companion Host      │
                        │   • yt-dlp (core)                │
                        │   • FFmpeg (mux / transcode)     │
                        │   • JS runtime for YouTube       │
                        │     challenge solving            │
                        │   • Filesystem, disk space, open │
                        └────────────────┬─────────────────┘
                                         ▼
                                Local disk (user-chosen folder)
```

### 2.2 Manifest V3 Requirements

| Item | Chrome | Firefox |
|---|---|---|
| Background | `background.service_worker` | `background.scripts` (event page); MV3 service workers are not used |
| Extension ID for native host | `allowed_origins` in the host manifest | `allowed_extensions` using `browser_specific_settings.gecko.id` |
| Popup | `action.default_popup` | Same |
| Polyfill | `webextension-polyfill` (promise API) | Native `browser.*` |

**Permissions (principle of least privilege)**

| Permission | Purpose | Notes |
|---|---|---|
| `nativeMessaging` | Communicate with the companion host | Required (P0) |
| `activeTab` | Read the active tab URL when the popup opens | Preferred over broad `tabs` |
| `storage` | Settings, queue, history persistence | `storage.local` for queue/history, `storage.sync` optional for settings |
| `scripting` | Inject content script on demand for SPA navigation detection | Optional, if static content scripts are insufficient |
| `alarms` | Heartbeat / reconnect backoff | Optional |
| `offscreen` (Chrome only) | Only if background keep-alive or audio/DOM work is needed | Avoid unless proven necessary |
| `notifications` | Completion and failure notifications | Optional permission requested at first use |
| Host permission `*://*.youtube.com/*` | Content script for page detection | Required |

**Explicitly not requested:** `<all_urls>`, `webRequest`/`declarativeNetRequest` (stream URLs are resolved by yt-dlp, not intercepted), `downloads` (file writing is done by the host), `cookies` (see §2.4), `history`.

### 2.3 Stream Extraction & Muxing

- **Extraction:** yt-dlp performs format discovery, signature and `n`-parameter handling, and format selection. The extension never scrapes or decrypts streams itself. yt-dlp's current YouTube support depends on an external JavaScript runtime (e.g., Deno) and, in some cases, proof-of-origin tokens; the companion installer must bundle or provision these, and requirements should be re-verified against yt-dlp release notes before each release.
- **Adaptive muxing:** YouTube serves 1080p+ as separate video-only and audio-only streams. The host downloads both and muxes with FFmpeg (stream copy where possible; transcode only when the target container requires it).
- **Container/codec compatibility** (enforced by the host; shown in UI per row):

| Container | Video codecs | Audio codecs | Notes |
|---|---|---|---|
| MP4 (Muxed) | H.264, VP9, AV1 | AAC (Opus allowed but poorly supported by some players) | Default; audio may be re-encoded to AAC for compatibility |
| WebM | VP9, AV1 | Opus | Always stream-copy |
| MKV | Any | Any | Most flexible; recommended for HDR archival |

- **Realistic quality ceilings:** H.264 is generally offered up to 1080p; 4K and HDR are VP9/AV1 only. The UI must therefore show the *actual* codec per row, and the "Fastest" tag should go to the lowest-effort path (typically 1080p H.264 + AAC stream-copy), not a hard-coded tier.
- **Audio fidelity honesty:** YouTube's source audio is lossy (Opus/AAC). "FLAC Lossless" and "MP3 320 kbps" are *transcodes* of that source. The UI must not imply true lossless provenance (see FR-AUD-6).
- **Size estimation:** Use yt-dlp `filesize`, else `filesize_approx`, else `tbr × duration`. Display "~" prefix when estimated.

### 2.4 Authentication & Age-Restricted Content
Age-restricted and members-only content requires user cookies. The host (not the extension) will use yt-dlp's `--cookies-from-browser` with the user's explicit opt-in in Settings. The extension itself never reads, stores, or transmits cookies.

### 2.5 Native Messaging Constraints

- **Message size:** host→browser messages are capped at 1 MB; browser→host at 64 MiB. **Media bytes never flow through native messaging.** The host writes directly to disk; only JSON control and progress messages cross the bridge.
- **Connection model:** use a long-lived port (`connectNative`) for progress streaming, with a request/response envelope: `{ id, type, payload }` → `{ id, type, ok, payload | error }`.
- **MV3 lifecycle:** Chrome service workers are ephemeral. An open native port keeps the worker alive while downloads are active, but the architecture must tolerate worker restarts: queue state lives in `storage.local` and the host remains the source of truth for in-flight job state (reconcile on reconnect via `LIST_JOBS`).
- **Progress throttling:** host emits progress at most 4 Hz per job; UI coalesces to animation frames.
- **Host install:** signed installers per OS register the native messaging manifest (per-user, no admin required where possible). The extension provides a guided install flow (see EC-2).
- **Versioning:** handshake exchanges `{ hostVersion, ytDlpVersion, ffmpegVersion, protocolVersion }`. The header "core version" indicator displays `ytDlpVersion`. Incompatible `protocolVersion` triggers an "Update companion" state.

### 2.6 Storage & Filesystem

| Data | Location | Retention |
|---|---|---|
| Settings | `storage.local` (sync optional, P2) | Persistent |
| Queue state | `storage.local` | Until completed/removed |
| History (metadata only, no media) | `storage.local` | Last 200 entries; user-clearable |
| Media files | User-chosen directory on disk (host-written) | User-managed |
| Temp/partial files | Host temp dir, `.part` files | Cleaned on cancel/failure; orphan sweep at host start |

Open-folder, show-in-folder, and free-disk-space queries are host operations (browser APIs cannot do them).

### 2.7 Legal & Policy Constraints

- Downloading may violate YouTube's Terms of Service or copyright law depending on content and jurisdiction. First-run and store listing copy must state that users are responsible for having rights to the content they download.
- StreamGrab must not circumvent DRM (Widevine/EME-protected content) and must fail gracefully with an explicit message instead.
- Store-policy risk: Chrome Web Store policies on YouTube downloaders are restrictive and enforcement varies. **Mitigation:** distribute via the channels that permit it (Firefox AMO review, self-hosted CRX/XPI, developer-mode install), keep the extension thin, and confirm current policy before submission. This is a P0 open question (see §7).

---

## 3. Functional Requirements

### 3.0 Global UI Shell (applies to all tabs)

**FR-GLB-1 (P0)** Render within a fixed ~400×600 px popup with the dark, high-density theme as designed in Stitch project `2139836994187367589`. Vertical regions, top to bottom: Header → Detection Card → Mode Tabs → Tab Content (scrollable) → Persistent Active Task Card → Utilities & Footer.

**FR-GLB-2 (P0)** Header shows "StreamGrab" branding, core version indicator (yt-dlp version from handshake), a **Copy Direct Stream Link** button, and a **Settings & Codec Defaults** button.

**User story:** *As a power user, I want to see which core version I'm running so I can tell whether a failure is because my engine is outdated.*

**Acceptance criteria**
- Version label shows `yt-dlp <version>`; shows "—" with a warning dot if the host is disconnected.
- Tapping the version label (P2) opens the "Update core" flow.

**FR-GLB-3 (P0)** Keyboard accessibility: all controls reachable via Tab; visible focus ring; `Esc` closes Settings; arrow keys move between tabs and quality rows; `Enter` triggers "Get" on the focused row.

**FR-GLB-4 (P1)** WCAG 2.1 AA contrast for text and controls on the dark theme; all icon-only buttons have `aria-label`s; progress bars expose `role="progressbar"` with `aria-valuenow`.

### 3.1 Tab Detection & Detection Card

**FR-DET-1 (P0)** On popup open, query the active tab. If the URL matches YouTube watch (`/watch?v=`), Shorts (`/shorts/`), live (`/live/`), or embed-in-page contexts, request metadata from the host (`GET_INFO`) and render the Detection Card.

**FR-DET-2 (P0)** The Detection Card displays: thumbnail, duration badge, live-detection pulse indicator, title (2-line clamp), channel name, view count, and top-level stream spec chips (e.g., `4K 60FPS`, `HDR`, `AV1`, `Opus 160k`) derived from the best available formats.

**User story:** *As a user on a YouTube video, I want StreamGrab to recognize it automatically so I can download without copying URLs.*

**Acceptance criteria**
- Card renders skeleton within 100 ms of popup open and real data within 2 s (p90, warm host).
- Pulse indicator animates while detection is in progress or while the tab is confirmed active on a video; stops (static dot) when metadata is resolved; respects `prefers-reduced-motion`.
- Spec chips only list capabilities that are actually available (no "HDR" chip if no HDR format exists).
- Changing the active tab or SPA-navigating to another video updates the card within 1 s (content-script navigation events, e.g., `yt-navigate-finish`).
- Duration and view counts are formatted locale-aware (`1:02:33`, `1.2M views`).

**FR-DET-3 (P0)** Cache metadata per video ID for 10 minutes to make repeat popup opens instant.

**FR-DET-4 (P1)** Detect playlist context (`list=` param) and expose it to the Queue Playlist shortcut (see §3.5).

**FR-DET-5 (P1)** Detect live streams, premieres, age-restricted, members-only, and DRM-protected videos and show a specific status badge and message instead of a generic error (see §4.2).

**FR-DET-6 (P0)** **Copy Direct Stream Link** (header): copies the resolved direct URL for the currently selected best format to the clipboard and shows a toast "Link copied · expires in ~6h". Must warn that googlevideo links are time-limited and IP-bound, and that adaptive formats yield separate video and audio URLs (copy both, labelled, if applicable).

**Acceptance criteria**
- Disabled with tooltip when no video is detected.
- Never logs the URL to telemetry.

### 3.2 Video Tab

**FR-VID-1 (P0)** Container switcher with options **MP4 (Muxed)**, **WebM**, **MKV**. Selection persists per user (default from Settings) and instantly recalculates each row's codec pairing, estimated size, and availability.

**FR-VID-2 (P0)** Tiered quality rows, in descending order, only for tiers available for the video:

| Tier | Label |
|---|---|
| 2160p | 4K UHD |
| 1440p | 2K QHD |
| 1080p | Full HD (tagged **Fastest** where applicable) |
| 720p | Standard HD |

Each row shows: resolution label, frame rate (e.g., 60 fps), HDR tag (if applicable), video codec + audio codec (e.g., `AV1 · Opus`), estimated file size, and a **Get** button.

**User story:** *As an editor, I want to see codecs and sizes before downloading so I can pick the right file for my timeline.*

**Acceptance criteria**
- Rows for unavailable tiers are hidden (P0); resolutions below 720p are accessible via an expander (P2).
- If a tier is unavailable in the selected container (e.g., 4K in H.264-only MP4), the row shows the actual fallback codec (e.g., `VP9 · AAC` after audio transcode) or is disabled with a tooltip explaining why.
- Size estimates are prefixed with `~` when not exact and update within 100 ms of container change.
- Clicking **Get** adds a job to the Queue (starts immediately if concurrency allows), switches the Active Task Card to the job, and shows a non-blocking toast "Added to queue". The user stays on the Video tab.
- Double-clicking **Get** must not create duplicate jobs (idempotency key = videoId + format + container); a second click on an in-queue item focuses it in the Queue tab.
- **Fastest** tag is assigned to the row with the lowest expected completion time (stream-copy path, smallest transfer) and appears on at most one row.

**FR-VID-3 (P1)** Row-level "More" menu: custom format selector (itag/format ID), embed subtitles, embed chapters, embed thumbnail.

**FR-VID-4 (P1)** HDR handling: when HDR is selected with MP4 or WebM and the pairing could degrade or mislabel HDR metadata, show a one-time advisory recommending MKV.

**FR-VID-5 (P2)** Frame-rate filter (30/60) and "prefer smaller file" optimization toggle.

### 3.3 Audio Tab

**FR-AUD-1 (P0)** Audio extraction tiers, each with a **Get** button and spec line:

| Tier | Spec | Tag | Behavior |
|---|---|---|---|
| FLAC Lossless | 24-bit, 48 kHz | — | Decode best source audio → FLAC |
| MP3 High Quality | 320 kbps CBR | **Best** | Transcode → MP3 CBR |
| Opus Native | 160 kbps WebM | **Direct** | Stream-copy, no re-encode |
| M4A / AAC | 256 kbps VBR | — | Copy AAC if available; else transcode |

**FR-AUD-2 (P0)** All audio outputs embed **ID3/metadata tags** (title, artist = channel, album = playlist or channel, year, comment = source URL) and **cover art** (video thumbnail, square-cropped option in Settings) where the container supports it. Opus/WebM uses Vorbis comments/WebM tags; cover embedding for WebM is best-effort and documented.

**FR-AUD-3 (P1)** A "Tags & Cover" toggle row at the top of the tab (on by default) to disable embedding.

**FR-AUD-4 (P1)** Editable tag preview (title/artist/album) before download (collapsed by default).

**FR-AUD-5 (P0)** **Direct** and **Best** tags must be computed, not hard-coded: *Direct* = no re-encode path available; *Best* = highest quality tier suited for broad compatibility.

**FR-AUD-6 (P0)** Fidelity transparency: an info icon on FLAC/MP3/M4A rows opens a tooltip: "YouTube audio is lossy. This format is converted from the best available source (Opus ~160 kbps / AAC). It won't add detail but preserves it in your chosen format." Spec values shown reflect the *output container*, and where the source is lower than the target (e.g., 256 kbps AAC target vs. 128 kbps AAC source), the row shows a "Transcoded" chip.

**User story:** *As a DJ, I want to extract audio with cover art and tags so my library stays organized without manual tagging.*

**Acceptance criteria**
- Output file opens in common players (VLC, foobar2000, iTunes/Music) showing title, artist, and cover.
- Sample rate/bit-depth settings are honored: FLAC at 48 kHz / 24-bit as listed; no upsampling beyond source rate without a visible chip.
- Estimated size shown per row (~ prefix).

### 3.4 Queue Tab (Batch Queue Manager)

**FR-QUE-1 (P0)** Tab label shows a numeric **badge** = count of active + queued items; hidden at 0.

**FR-QUE-2 (P0)** List groups: **Active**, **Queued**, **Completed (this session)**.
- **Active item:** thumbnail (small), title, progress bar, %, bytes transferred / total, speed, **Pause** (toggles to Resume).
- **Queued item:** title, format summary (codec/container), estimated size, **Remove**.
- **Completed item:** title, "Saved" status with check, **Open folder** control.

**FR-QUE-3 (P0)** **Clear Done** action (header of the tab) removes only completed and failed-and-dismissed items; never active or queued ones. Disabled when there is nothing to clear.

**FR-QUE-4 (P0)** Concurrency: default 1 active download (configurable 1–3 in Settings); the next queued item starts automatically on completion, failure (after logging), or cancellation.

**FR-QUE-5 (P0)** Persistence: queue survives popup close and browser restart. On restart, in-flight items become **Paused (interrupted)** and offer one-click Resume (host resumes `.part` files when possible).

**FR-QUE-6 (P1)** Drag-to-reorder queued items; "Move to top" in the item menu.

**FR-QUE-7 (P1)** Failed items show an error chip with an expandable reason and **Retry** / **Copy diagnostics** actions.

**FR-QUE-8 (P1)** Playlist shortcut behavior (MVP): **Queue Playlist** (Utilities row) jumps to the Queue tab. If a playlist is detected, shows an inline prompt "Add this playlist (N videos)?" with the current default quality; in v1.0 this supports up to 25 videos at one fixed quality preset. Full batch playlists (selection, per-item quality, range) ship in v1.1 (§6).

**User story:** *As a learner, I want to queue several downloads and walk away so they finish in order without babysitting the popup.*

**Acceptance criteria**
- Pausing an active item frees the concurrency slot only if the user selects "Pause & start next" (P2); default keeps the slot reserved.
- Removing an active item is not allowed (use Cancel in the Active Task Card); removing a queued item takes effect immediately with a 5 s Undo toast.
- Progress updates render at ≥ 2 Hz without jank (no layout thrash; transform-based bars).

### 3.5 History Tab

**FR-HIS-1 (P0)** List of recent completed downloads (most recent first), each with: thumbnail, title, resolution or bitrate (e.g., `2160p · AV1` or `MP3 320k`), file size, relative timestamp ("2h ago", "Yesterday"), and a **Show in folder** button.

**FR-HIS-2 (P0)** **Clear All** removes history metadata only (never deletes files) after a confirm dialog that states this explicitly.

**FR-HIS-3 (P1)** Verify file existence on **Show in folder**; if the file is moved or deleted, show "File not found" with options "Remove entry" / "Locate…" (host-assisted).

**FR-HIS-4 (P1)** Search/filter by title (client-side), cap at 200 entries with oldest-first eviction.

**FR-HIS-5 (P2)** Re-download action from a history item (re-queues with the same settings).

**Acceptance criteria**
- Relative timestamps update at popup open and every minute while open.
- Show in folder selects the file in the OS file manager (Explorer `/select`, Finder reveal, `xdg-open`/DBus `ShowItems` where available, fallback to opening the folder).

### 3.6 Persistent Active Task Card

**FR-ACT-1 (P0)** Whenever at least one job is active, a compact card is pinned above the footer showing: title (truncated), progress bar, %, bytes transferred/total, transfer speed, **ETA**, and **Pause/Resume** and **Cancel** controls. It remains visible across all tabs.

**FR-ACT-2 (P0)** Multi-stage jobs show the current stage in place of speed where relevant: *Downloading video → Downloading audio → Muxing → Tagging*. During muxing (no transfer), show an indeterminate or time-based bar.

**FR-ACT-3 (P0)** **Cancel** prompts a lightweight confirm ("Cancel and discard partial file?") with a "Keep partial" option (P1). Cancel is idempotent and cleans temp files within 5 s.

**FR-ACT-4 (P1)** If concurrency > 1, the card shows the most-advanced job with a "+N more" chip that opens the Queue tab.

**FR-ACT-5 (P1)** Extension action badge shows the active count; completion triggers an optional system notification with "Open folder".

**Acceptance criteria**
- ETA is smoothed (EWMA) to avoid flicker; hidden when not computable; displays "—".
- Controls remain responsive (< 100 ms visual response) under load.
- The card disappears (with a 300 ms transition) when no job is active.

### 3.7 Quick Utilities & Footer

**FR-FTR-1 (P0)** **Extract Audio** shortcut switches to the Audio tab and focuses the first tier row.

**FR-FTR-2 (P0)** **Queue Playlist** shortcut switches to the Queue tab (see FR-QUE-8 for playlist behavior).

**FR-FTR-3 (P0)** Free disk space indicator for the active download directory (e.g., `182 GB free`). Refreshes at popup open and every 30 s during downloads. Turns warning color below a configurable threshold (default < 5 GB or < 1.5× the active job's remaining size) and critical color when insufficient for the pending job.

**FR-FTR-4 (P0)** **Downloads** folder link opens the configured download directory via the host.

### 3.8 Settings & Codec Defaults

Opened from the header Settings button as a full-popup panel (back arrow returns to the previous state).

| ID | Setting | Default | Priority |
|---|---|---|---|
| FR-SET-1 | Default container (MP4 / WebM / MKV) | MP4 | P0 |
| FR-SET-2 | Preferred video codec order (H.264 / VP9 / AV1) | AV1 > VP9 > H.264 (hardware-decode aware warning) | P0 |
| FR-SET-3 | Preferred max resolution (Auto / 2160 / 1440 / 1080 / 720) | Auto | P0 |
| FR-SET-4 | Default audio format | MP3 320 | P0 |
| FR-SET-5 | Download directory (chosen via host-native picker) | OS Downloads / `StreamGrab` | P0 |
| FR-SET-6 | Filename template (yt-dlp tokens, with live preview) | `%(title)s [%(id)s].%(ext)s` | P1 |
| FR-SET-7 | Embed metadata / thumbnail / chapters / subtitles (individual toggles) | On / On / On / Off | P1 |
| FR-SET-8 | Concurrent downloads (1–3) | 1 | P1 |
| FR-SET-9 | Speed limit (Off / custom MB/s) | Off | P2 |
| FR-SET-10 | Use browser cookies for age-restricted/members content (Off / Chrome / Firefox / Edge…) | Off | P1 |
| FR-SET-11 | Auto-update yt-dlp (daily check) and "Update now" | On | P0 |
| FR-SET-12 | Low-disk-space warning threshold | 5 GB | P1 |
| FR-SET-13 | Notifications on completion/failure | On | P2 |
| FR-SET-14 | Anonymous telemetry (see §4.3) | **Off** | P1 |
| FR-SET-15 | Companion diagnostics (host/yt-dlp/ffmpeg versions, log export) | — | P1 |
| FR-SET-16 | Reset to defaults | — | P2 |

**User story:** *As a creator who edits on a laptop with no AV1 hardware decode, I want to default to H.264/VP9 so downloaded files play smoothly in my editor.*

**Acceptance criteria**
- Changes apply to new jobs only (never mutate in-flight/queued jobs); the UI notes this when relevant.
- Invalid filename templates are rejected with inline errors; unsafe characters sanitized per OS.
- The settings panel validates that the download directory exists and is writable via the host before saving.

---

## 4. Non-Functional Requirements

### 4.1 Performance & Startup

| ID | Requirement | Target | Priority |
|---|---|---|---|
| NFR-PERF-1 | Popup first paint (shell + skeleton) | ≤ 150 ms p50, ≤ 300 ms p95 | P0 |
| NFR-PERF-2 | Detection Card populated (warm host/cached) | ≤ 500 ms; cold ≤ 2 s p90 | P0 |
| NFR-PERF-3 | Popup JS bundle (gzipped) | ≤ 150 KB; no heavy frameworks without need | P1 |
| NFR-PERF-4 | Idle resource usage (no active jobs) | No persistent timers; host process exits or idles at < 20 MB | P1 |
| NFR-PERF-5 | Progress UI update cost | < 4 ms per frame; no full re-render of lists | P1 |
| NFR-PERF-6 | Host cold start | ≤ 800 ms to handshake | P1 |
| NFR-PERF-7 | Throughput overhead | Download speed within 10% of standalone yt-dlp on the same network | P1 |

Popup state is reconstructed from `storage.local` plus a host `LIST_JOBS` call to avoid blocking the first paint on the host.

### 4.2 Error Handling (Content Restrictions)

| Condition | Detection | UI Behavior | Priority |
|---|---|---|---|
| **Age-restricted** | yt-dlp error / metadata flag | Badge "Age-restricted"; row **Get** disabled with CTA "Enable browser cookies in Settings" (if signed in) | P0 |
| **DRM-protected** (e.g., rentals/movies) | `drm` format flags / extraction error | Badge "DRM-protected"; message "StreamGrab doesn't support DRM content"; no Get | P0 |
| **Live stream (in progress)** | `is_live` | Badge "LIVE"; offer "Record from now" (P2) or disabled (P0); clear message | P0 |
| **Premiere / upcoming** | `is_upcoming` | "Available at <time>" and disabled | P1 |
| **Members-only / private** | auth error | "Sign-in required" with the cookies option | P1 |
| **Region-blocked** | geo error | "Not available in your region" | P1 |
| **Rate-limited / bot-check** | HTTP 429 / "confirm you're not a bot" | Auto-backoff with retry; explain; suggest cookies/updating core | P1 |
| **Extraction broken** (YouTube change) | `ExtractorError` pattern | "Update core" CTA that triggers yt-dlp update and retry | P0 |
| **Network loss mid-download** | I/O error | Auto-retry up to 3 times with backoff; then **Paused (network)** with Resume | P0 |

All errors surface a short human sentence, a stable error code, and a **Copy diagnostics** action. Raw stack traces are never shown in the main UI.

### 4.3 Privacy & Security

| ID | Requirement | Priority |
|---|---|---|
| NFR-SEC-1 | **No remote code execution:** the extension bundles all of its JS (MV3 CSP); no `eval`, no remotely hosted scripts. | P0 |
| NFR-SEC-2 | **Native host trust:** the host manifest restricts access to the StreamGrab extension ID(s); the host rejects messages from other origins and validates every message against a schema. | P0 |
| NFR-SEC-3 | **Input sanitization:** only YouTube URLs/IDs matching a strict allow-list are accepted; arguments are passed to yt-dlp/FFmpeg as an argv array (never through a shell) to prevent command injection; filename templates are validated. | P0 |
| NFR-SEC-4 | **Path safety:** output paths are normalized and confined to the configured download directory (no traversal). | P0 |
| NFR-SEC-5 | **Binary integrity:** host installers and the yt-dlp/FFmpeg binaries are signed or checksum-verified; auto-updates fetch yt-dlp only from the official release channel over HTTPS with checksum verification. | P0 |
| NFR-SEC-6 | **Data minimization:** the extension stores only settings, queue, and history metadata locally; no browsing history, no cookies; no transmission to StreamGrab servers. | P0 |
| NFR-SEC-7 | **Telemetry:** off by default; opt-in only; anonymous aggregate events (feature use, error codes, versions), never URLs, titles, IDs, or paths. Privacy policy published and linked from the store listing and Settings. | P1 |
| NFR-SEC-8 | **Cookie handling:** if enabled, cookies are read by yt-dlp locally at job time and never persisted by StreamGrab or logged. | P1 |
| NFR-SEC-9 | **Logs:** host logs are local, redact URLs/tokens by default, and are rotated (≤ 5 MB). | P1 |
| NFR-SEC-10 | **Supply chain:** pinned, lockfile-verified dependencies; reproducible builds for the extension; SBOM for the host. | P2 |

### 4.4 Reliability, Compatibility & Accessibility

- **Reliability (P0):** no data loss in queue across crashes; atomic writes for the final file (write to `.part`, rename on success).
- **Browsers (P0):** Chrome ≥ 120 (Chromium-based Edge/Brave supported best-effort); Firefox ≥ 128 (MV3 support).
- **OS (P0):** Windows 10+, macOS 12+, Ubuntu 22.04+ / equivalent; x64 and arm64.
- **Localization (P2):** string externalization from day one (`_locales`); English at launch.
- **Accessibility (P1):** see FR-GLB-3/4; screen-reader announcements for progress milestones (25/50/75/100%), not every tick.

---

## 5. Edge Cases & States

| ID | State | Trigger | Required behavior | Priority |
|---|---|---|---|---|
| **EC-1** | **No YouTube tab active** | Active tab URL isn't YouTube, or is a non-video page (home, search, channel) | Detection Card replaced by an empty state ("Open a YouTube video to get started") with a "Paste URL" field (P1) and a list of recent downloads. Video/Audio tabs show disabled states. Queue/History/Settings remain fully functional. | P0 |
| **EC-2** | **Companion host disconnected / not installed** | `connectNative` error (host not found, crashed, blocked by AV) | Full-width banner: "Companion not connected" with **Install** (OS-detected download link + 3-step guide), **Retry**, and **Diagnostics**. Detection Card shows tab-derived info only (title, thumbnail from the page) with Get buttons disabled. Auto-retry with backoff (1 s → 30 s). Core version shows "—". | P0 |
| **EC-3** | **Host version mismatch** | `protocolVersion` incompatible | Blocking "Update companion" state with a download link; no jobs start. | P0 |
| **EC-4** | **Failed muxing** | FFmpeg non-zero exit, incompatible codec/container, corrupt stream | Job marked **Failed (mux)**; keep the downloaded video/audio parts for 24 h; offer **Retry mux**, **Retry as MKV**, and **Copy diagnostics**. Never leave a zero-byte or truncated final file. | P0 |
| **EC-5** | **Insufficient disk space** | Free space < estimated size × 1.2 (buffer for temp files) at start, or runtime `ENOSPC` | Pre-flight: warn on the row and in the footer indicator; **Get** opens a confirm "Not enough space (needs ~X GB, Y GB free)" with Change folder / Cancel. Runtime: pause job as **Paused (disk full)** and keep partial for resume after freeing space. | P0 |
| **EC-6** | **Empty queue** | No active/queued/completed items | Illustration/empty copy "Nothing in the queue" with CTA to the Video tab; **Clear Done** disabled. | P0 |
| **EC-7** | **Empty history** | No completed downloads or after Clear All | "No downloads yet"; **Clear All** disabled. | P0 |
| **EC-8** | **Popup closed during download** | User closes the popup | Downloads continue in the host; badge and notifications reflect progress; reopening restores the exact state. | P0 |
| **EC-9** | **Browser closed during download** | Browser quit | Host finishes the current job when possible (configurable: "Finish current download after browser closes", default On) or leaves resumable `.part`; the queue restores as Paused (interrupted). | P1 |
| **EC-10** | **SPA navigation mid-detection** | User navigates while metadata loads | Stale responses are discarded by request ID; the card reflects the latest video only. | P0 |
| **EC-11** | **Duplicate download** | Same video + format already in queue/history | Inline notice "Already in queue" (focus item) or "Downloaded before" with options Download again / Show file. | P1 |
| **EC-12** | **Filename collision** | Target file exists | Append ` (1)`, `(2)`… by default; never silently overwrite (overwrite is an explicit setting). | P0 |
| **EC-13** | **Very long titles / emoji / non-Latin** | Title length > OS limit or illegal characters | Truncate to a safe length (e.g., 200 bytes) while preserving the ID; sanitize per OS; keep Unicode where supported. | P0 |
| **EC-14** | **Download directory missing/unwritable** | Folder removed or permission lost | Block start; show "Choose a new folder" action. | P0 |
| **EC-15** | **Network offline at popup open** | No connectivity | Detection from tab data only; Get disabled with an "Offline" chip; queue stays paused and auto-resumes when online. | P1 |
| **EC-16** | **yt-dlp update fails** | Update server/checksum failure | Keep the existing version; non-blocking warning in Settings; retry later. | P1 |
| **EC-17** | **Very large files (4K, > 4 GB)** | Output on FAT32 volume | Pre-flight filesystem check; recommend MKV or another drive; fail early with an explanation. | P2 |
| **EC-18** | **Shorts / vertical video** | `/shorts/` URL | Treated like a normal video; the Detection Card shows a "Short" chip; tiers reflect actual resolutions. | P1 |

---

## 6. Phased Roadmap

### 6.1 MVP — v1.0 (Core Download & Inspect)

**Goal:** a dependable single-video downloader/extractor with a trustworthy queue and an easy companion install.

| Area | Scope | Priority |
|---|---|---|
| Shell | Full 4-screen UI per Stitch project `2139836994187367589` (Header, Detection Card, Tabs, Active Task Card, Footer) | P0 |
| Detection | Active tab detection, metadata card, spec chips, SPA navigation, copy direct link | P0 |
| Video | Container switcher, 4 quality tiers, size estimates, one-click Get, mux via host | P0 |
| Audio | FLAC / MP3 / Opus / M4A with tags + cover | P0 |
| Queue | Active/queued/completed, pause/resume/remove, Clear Done, persistence, concurrency 1–3 | P0 |
| History | List, Show in folder, Clear All | P0 |
| Active Task | Progress, ETA, Pause/Cancel, staged status | P0 |
| Footer | Extract Audio / Queue Playlist shortcuts, free disk, Downloads link | P0 |
| Settings | Container/codec/audio defaults, directory, auto-update core | P0 |
| Error states | Age-restricted, DRM, live, host disconnected, mux failure, low disk | P0 |
| Distribution | Chrome (subject to store policy review) and Firefox builds, signed host installers for Win/macOS/Linux | P0 |
| Quality | Filename templates, metadata toggles, retry/diagnostics, a11y pass, drag-reorder | P1 |
| Playlist (basic) | Add up to 25 videos from the current playlist at one preset | P1 |

**v1.0 exit criteria:** all P0 acceptance criteria pass on both browsers and 3 OSes; ≥ 95% success rate across a 200-video test corpus (mix of resolutions, HDR, Shorts, long-form); no P0/P1 security findings; 7-day internal soak with no queue-corruption bugs.

### 6.2 v1.1 — Batch Playlists & Chapter Clipping

| Feature | Description | Priority |
|---|---|---|
| **Full playlist batching** | Playlist picker modal: select all/none/range, per-item checkbox, per-playlist quality/container preset, ordering, skip already-downloaded; per-playlist progress rollup; "Pause playlist" | P0 (for v1.1) |
| **Playlist-aware naming** | `%(playlist_index)s - %(title)s` templates, playlist folder creation | P1 |
| **Chapter clipping** | Display chapters from video metadata; select one or more chapters to download as separate files or one merged clip; frame-accurate cut via re-encode, or fast keyframe cut (user toggle) | P0 (for v1.1) |
| **Time-range clipping** | Manual start/end inputs with a mini timeline preview | P1 |
| **Chapter-to-track audio split** | Split audio into per-chapter tracks with tags (album = video, track number = chapter index) | P1 |
| **Subtitle management** | Language picker, embed vs sidecar, auto-captions | P1 |
| **Queue enhancements** | Scheduling ("download when idle / overnight"), bandwidth limits per schedule | P2 |
| **Import URLs** | Paste a list of URLs / import from a text file | P2 |

### 6.3 Future Considerations (v1.2+, unscheduled)

- Support for additional sites supported by yt-dlp (feature-flagged).
- Settings sync across devices (`storage.sync`), preset profiles ("Archive", "Edit-ready", "Podcast").
- Channel/subscription watch folders; SponsorBlock-style segment removal.
- Localization beyond English; light theme.
- Companion-less lightweight mode (WASM FFmpeg for small muxes) as a fallback. Note: limited by memory and format coverage, evaluate separately.

---

## 7. Open Questions & Risks

| # | Question / Risk | Impact | Owner | Priority |
|---|---|---|---|---|
| 1 | **Store policy:** will Chrome Web Store accept a YouTube downloader? Is the listing, if rejected, viable via AMO and self-hosting? | Distribution | Product/Legal | P0 |
| 2 | **YouTube anti-bot changes** (PO tokens, JS challenges) can break extraction at any time; need a fast yt-dlp update pipeline and a user-visible "Update core" path. | Reliability | Eng | P0 |
| 3 | JS runtime dependency for yt-dlp on YouTube: bundle (size, licensing) vs. require user install vs. auto-provision. | Install UX | Eng | P0 |
| 4 | FFmpeg licensing (LGPL vs. GPL build; patent-encumbered codecs like AAC/H.264 encoders) and redistribution obligations. | Legal | Legal/Eng | P0 |
| 5 | Terms-of-service / copyright positioning and the copy used in onboarding and the store listing. | Legal | Legal | P0 |
| 6 | Companion install friction (SmartScreen/Gatekeeper warnings, AV false positives): code-signing and notarization budget. | Adoption | Eng/Ops | P0 |
| 7 | Honesty of "Lossless/320k/256k" labels given lossy sources. Confirm the final copy with design and legal. | Trust | Design/PM | P1 |
| 8 | Direct stream link usefulness (IP-bound, expiring, split A/V). Confirm the UX in the Stitch designs and acceptable behavior. | UX | Design | P1 |
| 9 | Firefox MV3 host-permission prompting and event-page lifecycle differences; test matrix. | QA | Eng | P1 |
| 10 | Telemetry: ship at all in v1.0, or rely solely on store metrics? | Metrics | PM | P2 |

---

## Appendix A — Native Messaging Protocol (Draft)

**Envelope**
```json
{ "id": "uuid", "type": "GET_INFO", "payload": { "url": "https://www.youtube.com/watch?v=..." } }
{ "id": "uuid", "type": "GET_INFO", "ok": true, "payload": { /* metadata + format list */ } }
{ "id": "uuid", "type": "GET_INFO", "ok": false, "error": { "code": "AGE_RESTRICTED", "message": "..." } }
```

**Request types:** `HANDSHAKE`, `GET_INFO`, `ENQUEUE`, `PAUSE`, `RESUME`, `CANCEL`, `REMOVE`, `LIST_JOBS`, `GET_DISK_INFO`, `OPEN_FOLDER`, `SHOW_IN_FOLDER`, `PICK_DIRECTORY`, `UPDATE_CORE`, `GET_DIAGNOSTICS`.

**Host-initiated events:** `JOB_PROGRESS { jobId, stage, percent, bytesDone, bytesTotal, speedBps, etaSec }`, `JOB_STATE { jobId, state, error? }`, `DISK_WARNING { freeBytes, requiredBytes }`.

**Job states:** `queued → preparing → downloading_video → downloading_audio → muxing → tagging → done` plus `paused`, `failed`, `cancelled`.

**Error codes (initial):** `HOST_INCOMPATIBLE`, `AGE_RESTRICTED`, `DRM_PROTECTED`, `LIVE_STREAM`, `PREMIERE_UPCOMING`, `AUTH_REQUIRED`, `GEO_BLOCKED`, `RATE_LIMITED`, `EXTRACTION_FAILED`, `NETWORK_ERROR`, `MUX_FAILED`, `DISK_FULL`, `PATH_UNWRITABLE`, `CANCELLED`, `UNKNOWN`.

## Appendix B — Traceability to Stitch Screens

| Stitch screen / component (project `2139836994187367589`) | PRD sections |
|---|---|
| Header (branding, core version, Copy Link, Settings) | FR-GLB-2, FR-DET-6, §3.8 |
| Active Video Detection Card | §3.1 |
| Mode Tabs — Video | §3.2 |
| Mode Tabs — Audio | §3.3 |
| Mode Tabs — Queue (badge) | §3.4 |
| Mode Tabs — History | §3.5 |
| Persistent Active Task Card | §3.6 |
| Quick Utilities & Footer | §3.7 |
