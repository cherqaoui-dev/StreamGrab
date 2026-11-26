import { useEffect, useRef, useState } from "preact/hooks";
import { formatBytes, formatDuration, formatEta, formatRelative, formatSize, formatSpeed, formatViews, stageLabel } from "../../lib/format";
import { nextContainer, step } from "../../lib/keyboard";
import { defaultSettings, emptySnapshot, type AudioRow, type FormatRow, type Job, type Settings, type Snapshot } from "../../protocol/types";
import { Icon } from "./icons";

type Tab = "video" | "audio" | "queue" | "history";

const containers = [
  { id: "mp4", label: "MP4" },
  { id: "webm", label: "WebM" },
  { id: "mkv", label: "MKV" },
] as const;

export function App() {
  const [snapshot, setSnapshot] = useState<Snapshot>(emptySnapshot());
  const [tab, setTab] = useState<Tab>("video");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const [selected, setSelected] = useState(0);
  const [audioIndex, setAudioIndex] = useState(0);
  const [tip, setTip] = useState<string | null>(null);
  const portRef = useRef<{ postMessage: (message: unknown) => void; disconnect: () => void } | null>(null);
  const workspaceRef = useRef<HTMLDivElement>(null);
  const settingsReturn = useRef<HTMLElement | null>(null);
  const skipSettingsFocus = useRef(true);

  useEffect(() => {
    const port = browser.runtime.connect({ name: "popup" });
    portRef.current = port;
    port.onMessage.addListener((message: { type?: string; snapshot?: Snapshot }) => {
      if (message.type === "SNAPSHOT" && message.snapshot) setSnapshot(message.snapshot);
    });
    void browser.tabs.query({ active: true, currentWindow: true }).then((tabs) => {
      port.postMessage({ type: "ACTIVE_TAB", url: tabs[0]?.url, title: tabs[0]?.title });
    });
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setSettingsOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      port.disconnect();
    };
  }, []);

  useEffect(() => {
    workspaceRef.current?.toggleAttribute("inert", settingsOpen);
    if (skipSettingsFocus.current) {
      skipSettingsFocus.current = false;
      return;
    }
    if (settingsOpen) document.querySelector<HTMLElement>(".settings button")?.focus();
    else settingsReturn.current?.focus();
  }, [settingsOpen]);

  useEffect(() => {
    if (!snapshot.notice) return;
    setToast(snapshot.notice);
    if (snapshot.notice === "Already in queue") setTab("queue");
    const timer = window.setTimeout(() => setToast(null), 2600);
    return () => window.clearTimeout(timer);
  }, [snapshot.noticeId]);

  const portSend = (message: Record<string, unknown>) => {
    portRef.current?.postMessage(message);
  };

  const visibleJobs = snapshot.jobs.filter((job) => !snapshot.hiddenJobIds.includes(job.id));
  const activeJobs = visibleJobs.filter((job) =>
    ["downloading_video", "downloading_audio", "muxing", "paused", "queued"].includes(job.state),
  );
  const running = visibleJobs.find((job) =>
    ["downloading_video", "downloading_audio", "muxing", "paused"].includes(job.state),
  );
  const queuedCount = visibleJobs.filter((job) =>
    ["queued", "downloading_video", "downloading_audio", "muxing", "paused"].includes(job.state),
  ).length;
  const history = visibleJobs.filter((job) => job.state === "done");
  const clearable = visibleJobs.some((job) => ["done", "failed", "cancelled"].includes(job.state));
  const info = snapshot.info;
  const blocked = snapshot.infoError;
  const hostDown = snapshot.host !== "ready";
  const settings = snapshot.settings ?? defaultSettings();
  const presetRows = info?.presets?.[settings.codecOrder]?.[settings.container];
  const rows = (presetRows ?? info?.rows ?? []).filter((row) => settings.maxHeight === 0 || row.height <= settings.maxHeight);
  const audioRows = info?.audio ?? [];
  const selectedRow = rows[Math.min(selected, Math.max(rows.length - 1, 0))];
  const lowDisk =
    snapshot.disk != null &&
    selectedRow != null &&
    snapshot.disk.freeBytes < Math.max(selectedRow.estimatedBytes * 1.2, 5 * 1024 ** 3);

  const copyLink = async () => {
    if (!selectedRow?.videoUrl) return;
    const lines = [`Video: ${selectedRow.videoUrl}`];
    if (selectedRow.audioUrl) lines.push(`Audio: ${selectedRow.audioUrl}`);
    lines.push("Expires in ~6h and is tied to this network.");
    await navigator.clipboard.writeText(lines.join("\n"));
    setToast("Link copied · expires in ~6h");
  };

  const blockedByDisk = (bytes: number) => {
    if (!snapshot.disk || bytes <= 0 || snapshot.disk.freeBytes >= bytes * 1.2) return false;
    setToast(`Not enough space (needs ${formatBytes(bytes * 1.2)}, ${formatBytes(snapshot.disk.freeBytes)} free)`);
    return true;
  };
  const get = (row: FormatRow) => {
    if (hostDown || blocked || row.available === false) return;
    if (blockedByDisk(row.estimatedBytes)) return;
    portSend({ type: "ENQUEUE", kind: "video", row });
  };
  const getAudio = (row: AudioRow) => {
    if (hostDown || blocked || !row.formatId) return;
    if (blockedByDisk(row.estimatedBytes)) return;
    portSend({ type: "ENQUEUE", kind: "audio", audio: row });
  };
  const updateSettings = (patch: Partial<Settings>) => {
    portSend({ type: "UPDATE_SETTINGS", settings: patch });
    if (typeof patch.autoUpdate === "boolean") portSend({ type: "SET_AUTO_UPDATE", enabled: patch.autoUpdate });
  };
  const openSettings = (event: { currentTarget: HTMLElement }) => {
    settingsReturn.current = event.currentTarget;
    setSettingsOpen(true);
  };
  const videoBlocked = hostDown || Boolean(blocked);

  return (
    <div class="shell">
      <div class="workspace" ref={workspaceRef}>
      <header class="header">
        <div class="brand">
          <div class="mark" aria-hidden="true">
            <Icon name="download" />
          </div>
          <div>
            <div class="brand-line">
              <h1>StreamGrab</h1>
              <span class="pro">PRO</span>
            </div>
            <button class="version" type="button" onClick={openSettings}>
              <span class={hostDown ? "dot warn" : "dot ok"} />
              {snapshot.handshake?.ytDlpVersion ? `yt-dlp ${snapshot.handshake.ytDlpVersion}` : "yt-dlp —"}
            </button>
          </div>
        </div>
        <div class="header-actions">
          <button type="button" aria-label="Copy direct stream link" aria-disabled={!selectedRow?.videoUrl} title={selectedRow?.videoUrl ? "Copy direct stream link" : "No video detected"} onClick={() => void copyLink()}>
            <Icon name="link" />
          </button>
          <button type="button" aria-label="Settings" title="Settings and diagnostics" onClick={openSettings}>
            <Icon name="tune" />
          </button>
        </div>
      </header>

      {snapshot.host === "disconnected" && (
        <div class="banner" role="status">
          <strong>Companion not connected</strong>
          <p>Run the StreamGrab host installer, then retry. Downloads stay on this machine.</p>
          <button type="button" onClick={() => portSend({ type: "RETRY_HOST" })}>Retry</button>
        </div>
      )}
      {snapshot.host === "incompatible" && (
        <div class="banner bad" role="status">
          <strong>Update companion</strong>
          <p>This extension needs host protocol {1}. Rebuild and register the host.</p>
        </div>
      )}

      <main>
        <Detection snapshot={snapshot} />
        <div class="tabs" role="tablist" aria-label="Media format modes">
          {(["video", "audio", "queue", "history"] as Tab[]).map((item) => (
            <button
              key={item}
              role="tab"
              data-tab={item}
              aria-selected={tab === item}
              tabIndex={tab === item ? 0 : -1}
              class={tab === item ? "tab on" : "tab"}
              onClick={() => setTab(item)}
              onKeyDown={(event) => moveTab(event, item, setTab)}
            >
              <Icon name={item} />
              <span>{label(item)}</span>
              {item === "queue" && queuedCount > 0 && <span class="badge">{queuedCount}</span>}
            </button>
          ))}
        </div>

        {tab === "video" && (
          <section class="panel" role="tabpanel">
            <div class="panel-head">
              <span>Available stream qualities</span>
              <div class="segment" role="radiogroup" aria-label="Container">
                {containers.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    role="radio"
                    data-container={item.id}
                    aria-checked={settings.container === item.id}
                    tabIndex={settings.container === item.id ? 0 : -1}
                    class={settings.container === item.id ? "on" : ""}
                    onClick={() => updateSettings({ container: item.id })}
                    onKeyDown={(event) => moveContainer(event, item.id, updateSettings)}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>
            {snapshot.infoLoading && <Skeleton />}
            {!snapshot.page && <Empty text="Open a YouTube video to get started" />}
            {snapshot.page && blocked && <Blocked error={blocked} onUpdate={() => portSend({ type: "UPDATE_CORE", retry: true })} />}
            {rows.map((row, index) => (
              <article key={row.height} class={index === selected ? "row selected" : "row"}>
                <div>
                  <div class="row-title">
                    <strong>{row.label}</strong>
                    {row.fps >= 50 && <em>{row.fps}fps</em>}
                    {row.hdr && <em>HDR</em>}
                    {row.fastest && <em class="fast">Fastest</em>}
                  </div>
                  <p>
                    {row.videoCodec}
                    {row.audioCodec ? ` · ${row.audioCodec}` : ""} · {formatSize(row.estimatedBytes, row.exact)}
                    {row.transcoded ? " · transcoded audio" : ""}
                  </p>
                </div>
                <button
                  type="button"
                  data-get={index}
                  class={index === 0 ? "get primary" : "get"}
                  aria-disabled={videoBlocked || row.available === false}
                  title={row.available === false ? row.reason : "Download this tier"}
                  onClick={() => {
                    setSelected(index);
                    get(row);
                  }}
                  onFocus={() => setSelected(index)}
                  onKeyDown={(event) => moveRow(event, index, rows.length, "data-get", setSelected)}
                >
                  <Icon name="download" />
                  Get
                </button>
              </article>
            ))}
            {snapshot.page && !snapshot.infoLoading && !blocked && rows.length === 0 && snapshot.host === "ready" && (
              <Empty text="No tiers were found for this container" />
            )}
          </section>
        )}

        {tab === "audio" && (
          <section class="panel" role="tabpanel">
            <div class="panel-head">
              <span>Audio extraction</span>
              <span class="accent">Tags + cover</span>
            </div>
            {snapshot.infoLoading && <Skeleton />}
            {!snapshot.page && <Empty text="Open a YouTube video to get started" />}
            {snapshot.page && blocked && <Blocked error={blocked} onUpdate={() => portSend({ type: "UPDATE_CORE", retry: true })} />}
            {snapshot.page && !snapshot.infoLoading && !blocked && audioRows.length === 0 && snapshot.host === "ready" && (
              <Empty text="No audio stream was found for this video" />
            )}
            {audioRows.map((row, index) => (
              <article key={row.id} class={index === audioIndex ? "row selected" : "row"}>
                <div class="grow">
                  <div class="row-title">
                    <strong>{row.label}</strong>
                    {row.tag && <em class={row.tag === "Direct" ? "fast" : ""}>{row.tag}</em>}
                    {row.transcoded && <em>Transcoded</em>}
                    {settings.audioFormat === row.id && <em>Default</em>}
                    {row.id !== "opus" && (
                      <button type="button" class="info" aria-label="About this conversion" onClick={() => setTip(tip === row.id ? null : row.id)}>
                        i
                      </button>
                    )}
                  </div>
                  <p>{row.spec} · {formatSize(row.estimatedBytes, row.exact)}</p>
                  {tip === row.id && (
                    <p class="tip">YouTube audio is lossy. This format is converted from the best available source ({row.source}). It won't add detail but preserves it in your chosen format.</p>
                  )}
                </div>
                <button
                  type="button"
                  class="get"
                  data-audio={row.id}
                  data-audio-index={index}
                  aria-disabled={videoBlocked || !row.formatId}
                  onClick={() => {
                    setAudioIndex(index);
                    getAudio(row);
                  }}
                  onFocus={() => setAudioIndex(index)}
                  onKeyDown={(event) => moveRow(event, index, audioRows.length, "data-audio-index", setAudioIndex)}
                >
                  <Icon name="download" />
                  Get
                </button>
              </article>
            ))}
          </section>
        )}

        {tab === "queue" && (
          <section class="panel" role="tabpanel">
            <div class="panel-head">
              <span>Batch queue ({queuedCount})</span>
              <button type="button" class="linkish" aria-disabled={!clearable} onClick={() => { if (clearable) portSend({ type: "CLEAR_DONE" }); }}>
                Clear done
              </button>
            </div>
            {visibleJobs.length === 0 && <Empty text="Nothing in the queue" />}
            {visibleJobs
              .filter((job) => job.state !== "done")
              .map((job) => (
                <QueueItem
                  key={job.id}
                  job={job}
                  focused={snapshot.focusJobId === job.id}
                  onPause={() => portSend({ type: job.state === "paused" ? "RESUME" : "PAUSE", jobId: job.id })}
                  onRemove={() => portSend({ type: "CANCEL", jobId: job.id })}
                  onOpen={() => job.outputPath && portSend({ type: "SHOW_IN_FOLDER", path: job.outputPath })}
                  onRetry={(container) => portSend({ type: "RETRY_MUX", jobId: job.id, container })}
                />
              ))}
          </section>
        )}

        {tab === "history" && (
          <section class="panel" role="tabpanel">
            <div class="panel-head">
              <span>Recent downloads ({history.length})</span>
              <button
                type="button"
                class="linkish"
                aria-disabled={history.length === 0}
                onClick={() => {
                  if (history.length === 0) return;
                  if (window.confirm("Clear history metadata? Files already saved stay on disk.")) {
                    portSend({ type: "CLEAR_HISTORY" });
                  }
                }}
              >
                Clear all
              </button>
            </div>
            {history.length === 0 && <Empty text="No downloads yet" />}
            {history.map((job) => (
              <article key={job.id} class="row">
                <div class="grow">
                  <strong class="ellipsis">{job.title || job.videoId}</strong>
                  <p>
                    {jobSummary(job)} · {formatRelative(job.createdAt)}
                  </p>
                </div>
                <button type="button" aria-label="Show in folder" aria-disabled={!job.outputPath} onClick={() => job.outputPath && portSend({ type: "SHOW_IN_FOLDER", path: job.outputPath })}>
                  <Icon name="folder" />
                </button>
              </article>
            ))}
          </section>
        )}

        {running && (
          <section class="active" aria-live="polite">
            <div class="panel-head">
              <span>Active task</span>
              <span class="accent">{jobStage(running)}</span>
            </div>
            <article class="task">
              <div class="task-top">
                <div class="grow">
                  <strong class="ellipsis">{running.title || running.videoId}</strong>
                  <p>
                    <span class="accent">{Math.round(running.percent)}%</span>
                    {" · "}
                    {formatBytes(running.bytesDone)} / {formatBytes(running.bytesTotal || running.estimatedBytes)}
                    {" · "}
                    {running.stage === "muxing" ? jobStage(running) : formatSpeed(running.speedBps)}
                    {" · "}
                    {formatEta(running.etaSec)}
                  </p>
                </div>
                <button type="button" aria-label={running.state === "paused" ? "Resume download" : "Pause download"} onClick={() => portSend({ type: running.state === "paused" ? "RESUME" : "PAUSE", jobId: running.id })}>
                  <Icon name={running.state === "paused" ? "play" : "pause"} />
                </button>
                <button type="button" aria-label="Cancel download" onClick={() => setConfirmId(running.id)}>
                  <Icon name="close" />
                </button>
              </div>
              <div
                class="bar"
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={Math.round(running.percent)}
              >
                <span style={{ width: `${Math.max(0, Math.min(100, running.percent))}%` }} />
              </div>
              {confirmId === running.id && (
                <div class="confirm">
                  <span>Cancel and discard the partial file?</span>
                  <button type="button" onClick={() => { portSend({ type: "CANCEL", jobId: running.id }); setConfirmId(null); }}>
                    Cancel
                  </button>
                  <button type="button" onClick={() => setConfirmId(null)}>Keep</button>
                </div>
              )}
            </article>
          </section>
        )}

        <div class="utilities">
          <button
            type="button"
            onClick={() => {
              setTab("audio");
              const index = Math.max(0, audioRows.findIndex((row) => row.id === settings.audioFormat));
              setAudioIndex(index);
              window.setTimeout(() => {
                document.querySelector<HTMLButtonElement>(`[data-audio-index="${index}"]`)?.focus();
              }, 0);
            }}
          >
            <Icon name="music" /> Extract audio
          </button>
          <button type="button" onClick={() => setTab("queue")}>
            <Icon name="playlist" /> Queue playlist
          </button>
        </div>
      </main>

      <footer class={lowDisk ? "footer warn" : "footer"}>
        <span>
          <i class={lowDisk ? "dot warn" : "dot ok"} />
          {snapshot.disk ? `${formatBytes(snapshot.disk.freeBytes)} free` : "Storage —"}
        </span>
        <button type="button" aria-disabled={hostDown} onClick={() => { if (!hostDown) portSend({ type: "OPEN_FOLDER" }); }}>
          <Icon name="folder" /> Downloads
        </button>
      </footer>
      </div>

      {toast && <div class="toast" role="status">{toast}</div>}
      {settingsOpen && (
        <Settings
          snapshot={snapshot}
          onClose={() => setSettingsOpen(false)}
          onChange={updateSettings}
          onPick={() => portSend({ type: "PICK_DIRECTORY" })}
          onUpdate={() => portSend({ type: "UPDATE_CORE", retry: true })}
        />
      )}
      <span class="sr">{activeJobs.length}</span>
    </div>
  );
}

function Detection({ snapshot }: { snapshot: Snapshot }) {
  const page = snapshot.page;
  const info = snapshot.info;
  if (!page) {
    return (
      <section class="detect empty-card">
        <p>Open a YouTube video to get started</p>
      </section>
    );
  }
  const title = info?.title || page.title || "YouTube video";
  const channel = info?.channel || page.channel;
  const views = info ? formatViews(info.viewCount) : "";
  const chips = info?.chips ?? [];
  return (
    <section class="detect">
      <div class="thumb">
        <img src={info?.thumbnail || page.thumbnail} alt="" />
        {info?.durationSec ? <span class="duration">{formatDuration(info.durationSec)}</span> : null}
        <span class={snapshot.infoLoading ? "pulse live" : "pulse"} title={snapshot.infoLoading ? "Detecting" : "Video detected"} />
        {page.kind === "shorts" && <span class="short">Short</span>}
      </div>
      <div class="detect-copy">
        <h2>{title}</h2>
        <p class="meta">{[channel, views].filter(Boolean).join(" · ")}</p>
        <div class="chips">
          {chips.map((chip) => (
            <span key={chip}>{chip}</span>
          ))}
        </div>
      </div>
    </section>
  );
}

function QueueItem({
  job,
  focused,
  onPause,
  onRemove,
  onOpen,
  onRetry,
}: {
  job: Job;
  focused: boolean;
  onPause: () => void;
  onRemove: () => void;
  onOpen: () => void;
  onRetry: (container: string) => void;
}) {
  const live = ["downloading_video", "downloading_audio", "muxing", "paused"].includes(job.state);
  return (
    <article class={focused ? "queue-item focused" : "queue-item"}>
      <div class="grow">
        <strong class="ellipsis">{job.title || job.videoId}</strong>
        <p>
          {live ? `${Math.round(job.percent)}% · ${formatSpeed(job.speedBps)}` : jobSummary(job)}
          {job.error ? ` · ${job.error.message}` : ""}
        </p>
        {live && (
          <div class="bar" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(job.percent)}>
            <span style={{ width: `${job.percent}%` }} />
          </div>
        )}
        {job.state === "failed" && job.error?.code === "MUX_FAILED" && (
          <div class="confirm">
            <button type="button" onClick={() => onRetry(job.container || "mp4")}>Retry mux</button>
            <button type="button" onClick={() => onRetry("mkv")}>Retry as MKV</button>
          </div>
        )}
      </div>
      {live ? (
        <button type="button" aria-label={job.state === "paused" ? "Resume" : "Pause"} onClick={onPause}>
          <Icon name={job.state === "paused" ? "play" : "pause"} />
        </button>
      ) : job.state === "done" ? (
        <button type="button" aria-label="Show in folder" onClick={onOpen}>
          <Icon name="check" />
        </button>
      ) : (
        <button type="button" aria-label="Remove from queue" onClick={onRemove}>
          <Icon name="close" />
        </button>
      )}
    </article>
  );
}

function Settings({
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
        <button type="button" onClick={onClose} aria-label="Back">
          Back
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

function Blocked({ error, onUpdate }: { error: { code: string; message: string }; onUpdate: () => void }) {
  return (
    <div class="blocked">
      <strong>{labelFor(error.code)}</strong>
      <p>{error.message}</p>
      {error.code === "EXTRACTION_FAILED" && (
        <button type="button" class="get" onClick={onUpdate}>Update core</button>
      )}
    </div>
  );
}

function Empty({ text }: { text: string }) {
  return <p class="empty">{text}</p>;
}

function Skeleton() {
  return <div class="skeleton" aria-hidden="true" />;
}

function jobSummary(job: Job): string {
  const size = formatBytes(job.bytesTotal || job.estimatedBytes);
  if (job.kind === "audio") return `${job.container.toUpperCase()} · ${size}`;
  const container = (job.container || "mp4").toUpperCase();
  return `${job.height}p ${container} · ${size}`;
}

function jobStage(job: Job): string {
  if (job.kind === "audio" && (job.stage === "muxing" || job.state === "muxing")) return "Tagging";
  return stageLabel(job.stage) || job.state;
}

function label(tab: Tab): string {
  if (tab === "video") return "Video";
  if (tab === "audio") return "Audio";
  if (tab === "queue") return "Queue";
  return "History";
}

function labelFor(code: string): string {
  switch (code) {
    case "AGE_RESTRICTED":
      return "Age-restricted";
    case "DRM_PROTECTED":
      return "DRM-protected";
    case "LIVE_STREAM":
      return "Live";
    case "DISK_FULL":
      return "Disk full";
    case "MUX_FAILED":
      return "Mux failed";
    case "HOST_INCOMPATIBLE":
      return "Update companion";
    default:
      return "Couldn't read video";
  }
}

function moveTab(event: KeyboardEvent, current: Tab, setTab: (tab: Tab) => void) {
  const order: Tab[] = ["video", "audio", "queue", "history"];
  if (event.key === "ArrowDown") {
    event.preventDefault();
    const panel = document.querySelector("[role='tabpanel']");
    const row = panel?.querySelector<HTMLElement>("[data-get], [data-audio-index]");
    (row ?? panel?.querySelector<HTMLElement>("button"))?.focus();
    return;
  }
  if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
  event.preventDefault();
  const next = order[step(order.indexOf(current), event.key === "ArrowRight" ? 1 : -1, order.length)];
  setTab(next);
  (event.currentTarget as HTMLElement).parentElement?.querySelector<HTMLElement>(`[data-tab="${next}"]`)?.focus();
}

function moveContainer(event: KeyboardEvent, current: string, update: (patch: Partial<Settings>) => void) {
  if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
  event.preventDefault();
  const next = nextContainer(current, event.key === "ArrowRight" ? 1 : -1);
  update({ container: next });
  (event.currentTarget as HTMLElement).parentElement?.querySelector<HTMLElement>(`[data-container="${next}"]`)?.focus();
}

function moveRow(
  event: KeyboardEvent,
  index: number,
  count: number,
  attribute: string,
  setSelected: (index: number) => void,
) {
  if (event.key === "ArrowUp" && index === 0) {
    event.preventDefault();
    document.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus();
    return;
  }
  if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
  event.preventDefault();
  const next = step(index, event.key === "ArrowDown" ? 1 : -1, count);
  setSelected(next);
  document.querySelector<HTMLElement>(`[${attribute}="${next}"]`)?.focus();
}
