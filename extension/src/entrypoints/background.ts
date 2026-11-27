import { isFinished, isLive, replaceJob, withProgress, type Progress } from "../lib/jobs";
import { KEYS, LEGACY_KEY, readStored, toStored, type Part } from "../lib/storage";
import { isFromWatchedTab } from "../lib/tabs";
import { isFresh, parseYouTubeUrl } from "../lib/youtube";
import {
  CACHE_MS,
  HOST_NAME,
  PROTOCOL_VERSION,
  defaultSettings,
  emptySnapshot,
  type AudioRow,
  type Envelope,
  type FormatRow,
  type Handshake,
  type Job,
  type PageVideo,
  type Settings,
  type Snapshot,
  type VideoInfo,
} from "../protocol/types";

const state: Snapshot = emptySnapshot();
const cache = new Map<string, VideoInfo>();
const pending = new Map<
  string,
  {
    resolve: (message: Envelope) => void;
    reject: (error: Error) => void;
    timer: ReturnType<typeof setTimeout>;
  }
>();
const popups = new Set<Browser.runtime.Port>();
let native: Browser.runtime.Port | null = null;
// The tab the popup was last opened on. Page messages from other tabs are ignored.
let watchedTabId: number | null = null;
// lookup numbers the latest video lookup; replies to older ones are dropped.
let lookup = 0;
let loadingId: string | null = null;
let retryMs = 1000;
let retryTimer: ReturnType<typeof setTimeout> | undefined;
let diskTimer: ReturnType<typeof setInterval> | undefined;

export default defineBackground(() => {
  // Messages wait for the saved state, so they can't overwrite it.
  const ready = restore();
  void ready.then(() => connect());
  browser.runtime.onConnect.addListener((port) => {
    if (port.name !== "popup") return;
    popups.add(port);
    port.onMessage.addListener((message) => {
      void ready.then(() => onPopup(message as PopupMessage));
    });
    port.onDisconnect.addListener(() => popups.delete(port));
    void ready.then(() => {
      if (popups.has(port)) port.postMessage({ type: "SNAPSHOT", snapshot: state });
    });
  });
  browser.runtime.onMessage.addListener((message, sender) => {
    if (!isFromWatchedTab(sender.tab?.id, watchedTabId)) return;
    if (message?.type === "PAGE_VIDEO") applyPage(message.payload as PageVideo);
    if (message?.type === "PAGE_CLEAR") clearPage();
  });
});

type PopupMessage =
  | { type: "GET_STATE" }
  | { type: "ACTIVE_TAB"; tabId?: number; url?: string; title?: string }
  | { type: "ENQUEUE"; kind: "video" | "audio"; row?: FormatRow; audio?: AudioRow }
  | { type: "PAUSE"; jobId: string }
  | { type: "RESUME"; jobId: string }
  | { type: "CANCEL"; jobId: string }
  | { type: "CLEAR_DONE" }
  | { type: "CLEAR_HISTORY" }
  | { type: "OPEN_FOLDER" }
  | { type: "SHOW_IN_FOLDER"; path: string }
  | { type: "RETRY_HOST" }
  | { type: "FOCUS_JOB"; jobId: string }
  | { type: "UPDATE_SETTINGS"; settings: Partial<Settings> }
  | { type: "PICK_DIRECTORY" }
  | { type: "UPDATE_CORE"; retry?: boolean }
  | { type: "SET_AUTO_UPDATE"; enabled: boolean }
  | { type: "RETRY_MUX"; jobId: string; container?: string };

async function restore(): Promise<void> {
  const raw = await browser.storage.local.get([...Object.values(KEYS), LEGACY_KEY]);
  const data = readStored(raw);
  for (const entry of data.cache ?? []) {
    if (isFresh(entry.fetchedAt) && entry.presets) cache.set(entry.id, entry);
  }
  state.jobs = data.jobs ?? [];
  state.hiddenJobIds = data.hiddenJobIds ?? [];
  state.settings = { ...defaultSettings(), ...data.settings };
  if (raw[LEGACY_KEY]) {
    await persist("cache", "jobs", "hiddenJobIds", "settings");
    await browser.storage.local.remove(LEGACY_KEY);
  }
}

function connect(): void {
  if (retryTimer) clearTimeout(retryTimer);
  dropHost();
  state.host = "connecting";
  broadcast();
  let port: Browser.runtime.Port;
  try {
    port = browser.runtime.connectNative(HOST_NAME);
  } catch {
    failHost();
    return;
  }
  native = port;
  port.onMessage.addListener((message) => {
    if (native === port) onNative(message as Envelope);
  });
  port.onDisconnect.addListener(() => {
    if (native === port) failHost();
  });
  void handshake(port);
}

async function handshake(port: Browser.runtime.Port): Promise<void> {
  try {
    const response = await request("HANDSHAKE", {});
    if (!response.ok) {
      failHost();
      return;
    }
    const payload = response.payload as Handshake;
    state.handshake = payload;
    if (payload.protocolVersion !== PROTOCOL_VERSION) {
      state.host = "incompatible";
      broadcast();
      return;
    }
    state.host = "ready";
    state.updateWarning = payload.updateWarning || null;
    if (typeof payload.autoUpdate === "boolean") {
      state.settings = { ...state.settings, autoUpdate: payload.autoUpdate };
      await persist("settings");
    }
    retryMs = 1000;
    await refreshJobs();
    await refreshDisk();
    if (state.page) ensureInfo(state.page);
    broadcast();
    armDiskTimer();
  } catch {
    if (native === port) failHost();
  }
}

function notify(text: string): void {
  state.notice = text;
  state.noticeId += 1;
}

// dropHost closes the current connection and fails whatever was waiting on it.
function dropHost(): void {
  const port = native;
  native = null;
  port?.disconnect();
  for (const waiter of pending.values()) {
    clearTimeout(waiter.timer);
    waiter.reject(new Error("disconnected"));
  }
  pending.clear();
  cancelLookup();
}

function failHost(): void {
  dropHost();
  if (state.host !== "incompatible") state.host = "disconnected";
  state.handshake = state.host === "incompatible" ? state.handshake : null;
  broadcast();
  if (state.host === "disconnected") {
    if (retryTimer) clearTimeout(retryTimer);
    retryTimer = setTimeout(connect, retryMs);
    retryMs = Math.min(retryMs * 2, 30_000);
  }
}

function onNative(message: Envelope): void {
  switch (message.type) {
    case "JOB_PROGRESS":
      applyProgress(message.payload as Progress);
      return;
    case "JOB_STATE":
      applyJobState((message.payload as { job: Job }).job);
      return;
    case "JOB_REMOVED":
      removeJob((message.payload as { jobId: string }).jobId);
      return;
    case "CORE_STATUS":
      applyCoreStatus(message);
      return;
  }
  const waiter = pending.get(message.id);
  if (!waiter) return;
  clearTimeout(waiter.timer);
  pending.delete(message.id);
  waiter.resolve(message);
}

// Progress arrives several times a second, so only the changed job is sent on.
function applyProgress(progress: Progress): void {
  const current = state.jobs.find((job) => job.id === progress.jobId);
  if (!current) return;
  const job = withProgress(current, progress);
  state.jobs = replaceJob(state.jobs, job);
  for (const port of popups) port.postMessage({ type: "JOB", job });
}

function applyJobState(job: Job): void {
  upsertJob(job);
  void persist("jobs");
  broadcast();
}

function removeJob(jobId: string): void {
  state.jobs = state.jobs.filter((job) => job.id !== jobId);
  state.hiddenJobIds = state.hiddenJobIds.filter((id) => id !== jobId);
  void persist("jobs", "hiddenJobIds");
  broadcast();
}

function applyCoreStatus(message: Envelope): void {
  const payload = message.payload as { updated?: boolean; ytDlpVersion?: string; warning?: string };
  state.updateWarning = payload.warning || null;
  if (state.handshake && payload.ytDlpVersion) {
    state.handshake = { ...state.handshake, ytDlpVersion: payload.ytDlpVersion, updateWarning: payload.warning };
  }
  if (payload.warning) notify(payload.warning);
  else if (payload.updated) notify(`yt-dlp updated to ${payload.ytDlpVersion}`);
  broadcast();
}

function upsertJob(job: Job): void {
  if (state.jobs.some((item) => item.id === job.id)) state.jobs = replaceJob(state.jobs, job);
  else state.jobs = [job, ...state.jobs];
}

async function onPopup(message: PopupMessage): Promise<void> {
  switch (message.type) {
    case "GET_STATE":
      broadcast();
      return;
    case "ACTIVE_TAB":
      watchedTabId = message.tabId ?? null;
      applyActiveTab(message.url, message.title);
      return;
    case "ENQUEUE":
      await enqueue(message);
      return;
    case "PAUSE":
    case "RESUME":
    case "CANCEL":
      await request(message.type, { jobId: message.jobId }).catch(() => undefined);
      return;
    case "CLEAR_DONE":
      state.hiddenJobIds = unique([
        ...state.hiddenJobIds,
        ...state.jobs.filter(isFinished).map((job) => job.id),
      ]);
      await persist("hiddenJobIds");
      broadcast();
      return;
    case "CLEAR_HISTORY":
      state.hiddenJobIds = unique([
        ...state.hiddenJobIds,
        ...state.jobs.filter((job) => job.state === "done").map((job) => job.id),
      ]);
      await persist("hiddenJobIds");
      broadcast();
      return;
    case "OPEN_FOLDER":
      await request("OPEN_FOLDER", {}).catch(() => undefined);
      return;
    case "SHOW_IN_FOLDER":
      await request("SHOW_IN_FOLDER", { path: message.path }).catch(() => undefined);
      return;
    case "RETRY_HOST":
      retryMs = 1000;
      connect();
      return;
    case "FOCUS_JOB":
      state.focusJobId = message.jobId;
      broadcast();
      return;
    case "UPDATE_SETTINGS":
      state.settings = { ...state.settings, ...message.settings };
      await persist("settings");
      broadcast();
      return;
    case "PICK_DIRECTORY":
      await pickDirectory();
      return;
    case "UPDATE_CORE":
      await updateCore(Boolean(message.retry));
      return;
    case "SET_AUTO_UPDATE":
      await request("SET_AUTO_UPDATE", { enabled: message.enabled }).catch(() => undefined);
      return;
    case "RETRY_MUX":
      await request("RETRY_MUX", { jobId: message.jobId, container: message.container ?? "" }).catch(() => undefined);
      return;
  }
}

async function enqueue(message: Extract<PopupMessage, { type: "ENQUEUE" }>): Promise<void> {
  if (state.host !== "ready" || !state.info) {
    notify("Connect the companion before downloading.");
    broadcast();
    return;
  }
  const audio = message.kind === "audio";
  const container = audio ? message.audio?.container : message.row?.container || state.settings.container;
  const height = audio ? 0 : message.row?.height ?? 0;
  if (!container || (audio && !message.audio) || (!audio && !message.row?.videoFormatId)) {
    notify("That format can't be downloaded.");
    broadcast();
    return;
  }
  const key = audio ? `${state.info.id}:audio:${container}` : `${state.info.id}:${height}:${container}`;
  const existing = state.jobs.find((job) => job.key === key && !isFinished(job));
  if (existing) {
    state.focusJobId = existing.id;
    notify("Already in queue");
    broadcast();
    return;
  }
  try {
    const response = await request("ENQUEUE", {
      url: state.info.url,
      videoId: state.info.id,
      title: state.info.title || state.page?.title || "YouTube video",
      thumbnail: state.info.thumbnail || state.page?.thumbnail || "",
      kind: audio ? "audio" : "video",
      height,
      container,
      videoFormatId: audio ? "" : message.row?.videoFormatId,
      audioFormatId: audio ? message.audio?.formatId : message.row?.audioFormatId,
      estimatedBytes: audio ? message.audio?.estimatedBytes : message.row?.estimatedBytes,
      recodeAudio: audio ? !message.audio?.copy : Boolean(message.row?.recodeAudio),
      sampleRate: message.audio?.sampleRate ?? 0,
    });
    if (!response.ok) {
      state.infoError = response.error ?? { code: "UNKNOWN", message: "The download didn't start." };
      notify(state.infoError.message);
      broadcast();
      return;
    }
    const payload = response.payload as { job: Job; already: boolean };
    upsertJob(payload.job);
    state.focusJobId = payload.job.id;
    notify(payload.already ? "Already in queue" : "Added to queue");
    await persist("jobs");
    broadcast();
  } catch {
    notify("The companion didn't answer.");
    broadcast();
  }
}

function applyActiveTab(url?: string, title?: string): void {
  const parsed = parse(url);
  if (!parsed) {
    clearPage();
    return;
  }
  applyPage({
    ...parsed,
    title:
      title?.replace(/ - YouTube$/, "") || (state.page?.videoId === parsed.videoId ? state.page.title : ""),
    channel: state.page?.videoId === parsed.videoId ? state.page.channel : "",
    thumbnail:
      state.page?.videoId === parsed.videoId && state.page.thumbnail
        ? state.page.thumbnail
        : `https://i.ytimg.com/vi/${parsed.videoId}/hqdefault.jpg`,
  });
}

function applyPage(page: PageVideo): void {
  const same = state.page?.videoId === page.videoId;
  state.page = same && state.page ? { ...state.page, ...page } : page;
  if (!same) {
    cancelLookup();
    state.info = null;
    state.infoError = null;
    const cached = cache.get(page.videoId);
    if (cached && isFresh(cached.fetchedAt) && cached.presets) state.info = cached;
  }
  ensureInfo(page);
  broadcast();
}

function clearPage(): void {
  cancelLookup();
  state.page = null;
  state.info = null;
  state.infoError = null;
  broadcast();
}

function cancelLookup(): void {
  lookup += 1;
  loadingId = null;
  state.infoLoading = false;
}

// A lookup starts a yt-dlp process, so it runs only while the popup is open and
// only once per video at a time.
function ensureInfo(page: PageVideo): void {
  if (state.info || popups.size === 0 || state.host !== "ready" || loadingId === page.videoId) return;
  void loadInfo(page);
}

async function loadInfo(page: PageVideo): Promise<void> {
  const token = ++lookup;
  loadingId = page.videoId;
  state.infoLoading = true;
  state.infoError = null;
  broadcast();
  try {
    const response = await request("GET_INFO", { url: page.url });
    if (token !== lookup) return;
    loadingId = null;
    state.infoLoading = false;
    if (!response.ok) {
      state.infoError = response.error ?? { code: "EXTRACTION_FAILED", message: "StreamGrab couldn't read this video." };
      broadcast();
      return;
    }
    const info = { ...(response.payload as Omit<VideoInfo, "fetchedAt">), fetchedAt: Date.now() };
    state.info = info;
    cache.set(info.id, info);
    broadcast();
    await persist("cache");
  } catch {
    if (token !== lookup) return;
    loadingId = null;
    state.infoLoading = false;
    state.infoError = { code: "NETWORK_ERROR", message: "The companion didn't answer." };
    broadcast();
  }
}

async function refreshJobs(): Promise<void> {
  const response = await request("LIST_JOBS", {});
  if (!response.ok) return;
  const payload = response.payload as { jobs?: Job[] };
  if (!payload.jobs) return;
  state.jobs = payload.jobs;
  const known = new Set(payload.jobs.map((job) => job.id));
  state.hiddenJobIds = state.hiddenJobIds.filter((id) => known.has(id));
  await persist("jobs", "hiddenJobIds");
}

async function refreshDisk(): Promise<void> {
  const response = await request("GET_DISK_INFO", {}).catch(() => null);
  if (!response?.ok) return;
  state.disk = response.payload as Snapshot["disk"];
}

function armDiskTimer(): void {
  if (diskTimer) clearInterval(diskTimer);
  diskTimer = setInterval(() => {
    if (state.jobs.some(isLive)) void refreshDisk().then(broadcast);
  }, 30_000);
}

async function updateCore(retry: boolean): Promise<void> {
  try {
    const response = await request("UPDATE_CORE", {}, 180_000);
    const payload = (response.payload ?? {}) as { updated?: boolean; ytDlpVersion?: string; warning?: string };
    state.updateWarning = payload.warning || null;
    if (state.handshake && payload.ytDlpVersion) {
      state.handshake = { ...state.handshake, ytDlpVersion: payload.ytDlpVersion, updateWarning: payload.warning };
    }
    if (!response.ok) notify(response.error?.message || "yt-dlp couldn't be updated. The installed version was kept.");
    else if (payload.warning) notify(payload.warning);
    else if (payload.updated) notify(`yt-dlp updated to ${payload.ytDlpVersion}`);
    else notify("yt-dlp is already current.");
    broadcast();
    if (state.page && (retry || !payload.warning)) {
      cache.delete(state.page.videoId);
      await loadInfo(state.page);
    }
  } catch {
    notify("yt-dlp couldn't be updated. The installed version was kept.");
    broadcast();
  }
}

async function pickDirectory(): Promise<void> {
  try {
    const response = await request("PICK_DIRECTORY", {}, 180_000);
    if (!response.ok) {
      notify(response.error?.message || "StreamGrab couldn't open the folder picker.");
      broadcast();
      return;
    }
    const payload = response.payload as { cancelled?: boolean; path?: string; freeBytes?: number };
    if (payload.cancelled || !payload.path) return;
    if (state.handshake) state.handshake = { ...state.handshake, downloadDir: payload.path };
    state.disk = { freeBytes: payload.freeBytes ?? state.disk?.freeBytes ?? 0, path: payload.path };
    notify("Download folder updated");
    broadcast();
  } catch {
    notify("The companion didn't answer.");
    broadcast();
  }
}

function request(type: string, payload: unknown, timeoutMs = 60_000): Promise<Envelope> {
  if (!native) return Promise.reject(new Error("disconnected"));
  const id = crypto.randomUUID();
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error("timeout"));
    }, timeoutMs);
    pending.set(id, { resolve, reject, timer });
    native?.postMessage({ id, type, payload });
  });
}

async function persist(...parts: Part[]): Promise<void> {
  for (const [id, entry] of cache) {
    if (Date.now() - entry.fetchedAt >= CACHE_MS) cache.delete(id);
  }
  const values = {
    cache: [...cache.values()],
    jobs: state.jobs,
    hiddenJobIds: state.hiddenJobIds,
    settings: state.settings,
  };
  await browser.storage.local.set(toStored(values, parts));
}

// postMessage copies the snapshot, so it isn't cloned first.
function broadcast(): void {
  for (const port of popups) port.postMessage({ type: "SNAPSHOT", snapshot: state });
}

function parse(url?: string): Pick<PageVideo, "videoId" | "url" | "kind"> | null {
  const parsed = parseYouTubeUrl(url);
  return parsed ? { videoId: parsed.videoId, url: parsed.url, kind: parsed.kind } : null;
}

function unique(values: string[]): string[] {
  return [...new Set(values)];
}
