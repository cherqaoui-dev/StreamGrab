import { defaultSettings, type Settings, type Snapshot } from "../../../protocol/types";
import { Icon } from "../icons";

export function SettingsDialog({
  snapshot,
  onClose,
  onChange,
  onPick,
  onUpdate,
}: {
  snapshot: Snapshot;
  onClose: () => void;
  onChange: (patch: Partial<Settings>) => void;
  onPick: () => void;
  onUpdate: () => void;
}) {
  const host = snapshot.handshake;
  const current = snapshot.settings ?? defaultSettings();
  const folder = host?.downloadDir || snapshot.disk?.path || "—";
  const text = JSON.stringify(
    {
      host: snapshot.host,
      handshake: host,
      settings: current,
      error: snapshot.infoError,
      jobs: snapshot.jobs.map((job) => ({ id: job.id, state: job.state, error: job.error })),
    },
    null,
    2,
  );
  return (
    <section class="settings" role="dialog" aria-modal="true" aria-label="Settings and diagnostics">
      <header>
        <button type="button" onClick={onClose} aria-label="Back" title="Back">
          <Icon name="back" />
        </button>
        <h2>Settings</h2>
      </header>
      <p class="tip">Changes apply to new downloads.</p>
      <label>
        Container
        <select value={current.container} onChange={(event) => onChange({ container: event.currentTarget.value as Settings["container"] })}>
          <option value="mp4">MP4</option>
          <option value="webm">WebM</option>
          <option value="mkv">MKV</option>
        </select>
      </label>
      <label>
        Video codec order
        <select value={current.codecOrder} onChange={(event) => onChange({ codecOrder: event.currentTarget.value as Settings["codecOrder"] })}>
          <option value="av1-vp9-h264">AV1, then VP9, then H.264</option>
          <option value="vp9-av1-h264">VP9, then AV1, then H.264</option>
          <option value="h264-vp9-av1">H.264, then VP9, then AV1</option>
        </select>
      </label>
      <label>
        Max resolution
        <select value={String(current.maxHeight)} onChange={(event) => onChange({ maxHeight: Number(event.currentTarget.value) as Settings["maxHeight"] })}>
          <option value="0">Auto</option>
          <option value="2160">2160p</option>
          <option value="1440">1440p</option>
          <option value="1080">1080p</option>
          <option value="720">720p</option>
        </select>
      </label>
      <label>
        Default audio
        <select value={current.audioFormat} onChange={(event) => onChange({ audioFormat: event.currentTarget.value as Settings["audioFormat"] })}>
          <option value="mp3">MP3 320</option>
          <option value="m4a">M4A / AAC</option>
          <option value="opus">Opus</option>
          <option value="flac">FLAC</option>
        </select>
      </label>
      <div class="folder-row">
        <div>
          <span>Download folder</span>
          <strong>{folder}</strong>
        </div>
        <button type="button" class="get" aria-disabled={snapshot.host !== "ready"} onClick={() => { if (snapshot.host === "ready") onPick(); }}>Change</button>
      </div>
      <label class="check">
        <span>Auto-update yt-dlp</span>
        <input
          type="checkbox"
          checked={current.autoUpdate !== false}
          onChange={(event) => onChange({ autoUpdate: event.currentTarget.checked })}
        />
      </label>
      <div class="folder-row">
        <span>yt-dlp {host?.ytDlpVersion || "—"}</span>
        <button type="button" class="get" aria-disabled={snapshot.host !== "ready"} onClick={() => { if (snapshot.host === "ready") onUpdate(); }}>Update now</button>
      </div>
      {(snapshot.updateWarning || host?.updateWarning) && <p class="tip">{snapshot.updateWarning || host?.updateWarning}</p>}
      <h3>Diagnostics</h3>
      <dl>
        <div><dt>Host</dt><dd>{snapshot.host}</dd></div>
        <div><dt>yt-dlp</dt><dd>{host?.ytDlpVersion || "—"}</dd></div>
        <div><dt>FFmpeg</dt><dd>{host?.ffmpegVersion || "—"}</dd></div>
        <div><dt>Deno</dt><dd>{host?.deno ? host.denoVersion || "found" : "not found"}</dd></div>
        <div><dt>Protocol</dt><dd>{host?.protocolVersion ?? "—"}</dd></div>
      </dl>
      <button type="button" class="get primary" onClick={() => void navigator.clipboard.writeText(text)}>
        Copy diagnostics
      </button>
    </section>
  );
}
