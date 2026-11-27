import { useEffect, useRef, useState } from "preact/hooks";
import { formatBytes } from "../../lib/format";
import { isFinished, isLive, replaceJob } from "../../lib/jobs";
import { defaultSettings, emptySnapshot, type AudioRow, type FormatRow, type Job, type Settings, type Snapshot } from "../../protocol/types";
import { ActiveTask } from "./components/ActiveTask";
import { AudioPanel } from "./components/AudioPanel";
import { Detection } from "./components/Detection";
import { HistoryPanel } from "./components/HistoryPanel";
import { QueuePanel } from "./components/QueuePanel";
import { SettingsDialog } from "./components/SettingsDialog";
import { VideoPanel } from "./components/VideoPanel";
import { Icon } from "./icons";
import { moveTab, tabs, type Tab } from "./navigation";

export function App() {
  const [snapshot, setSnapshot] = useState<Snapshot>(emptySnapshot());
  const [tab, setTab] = useState<Tab>("video");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [selected, setSelected] = useState(0);
  const [audioIndex, setAudioIndex] = useState(0);
  const portRef = useRef<{ postMessage: (message: unknown) => void; disconnect: () => void } | null>(null);
  const workspaceRef = useRef<HTMLDivElement>(null);
  const settingsReturn = useRef<HTMLElement | null>(null);
  const skipSettingsFocus = useRef(true);

  useEffect(() => {
    const port = browser.runtime.connect({ name: "popup" });
    portRef.current = port;
    port.onMessage.addListener((message: { type?: string; snapshot?: Snapshot; job?: Job }) => {
      if (message.type === "SNAPSHOT" && message.snapshot) setSnapshot(message.snapshot);
      const job = message.type === "JOB" ? message.job : undefined;
      if (job) setSnapshot((previous) => ({ ...previous, jobs: replaceJob(previous.jobs, job) }));
    });
    void browser.tabs.query({ active: true, currentWindow: true }).then(([current]) => {
      port.postMessage({ type: "ACTIVE_TAB", tabId: current?.id, url: current?.url, title: current?.title });
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
  const running = visibleJobs.find(isLive);
  const queuedCount = visibleJobs.filter((job) => !isFinished(job)).length;
  const history = visibleJobs.filter((job) => job.state === "done");
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
  const updateCore = () => portSend({ type: "UPDATE_CORE", retry: true });

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
          {tabs.map((item) => (
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
          <VideoPanel
            snapshot={snapshot}
            settings={settings}
            rows={rows}
            selected={selected}
            setSelected={setSelected}
            disabled={videoBlocked}
            onGet={get}
            onChange={updateSettings}
            onUpdate={updateCore}
          />
        )}

        {tab === "audio" && (
          <AudioPanel
            snapshot={snapshot}
            settings={settings}
            selected={audioIndex}
            setSelected={setAudioIndex}
            disabled={videoBlocked}
            onGet={getAudio}
            onUpdate={updateCore}
          />
        )}

        {tab === "queue" && <QueuePanel jobs={visibleJobs} focusJobId={snapshot.focusJobId} send={portSend} />}

        {tab === "history" && <HistoryPanel jobs={history} send={portSend} />}

        {running && <ActiveTask job={running} send={portSend} />}

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
        <SettingsDialog
          snapshot={snapshot}
          onClose={() => setSettingsOpen(false)}
          onChange={updateSettings}
          onPick={() => portSend({ type: "PICK_DIRECTORY" })}
          onUpdate={updateCore}
        />
      )}
    </div>
  );
}

function label(tab: Tab): string {
  if (tab === "video") return "Video";
  if (tab === "audio") return "Audio";
  if (tab === "queue") return "Queue";
  return "History";
}
