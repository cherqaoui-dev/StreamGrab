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

type CacheEntry = VideoInfo;

const state: Snapshot = emptySnapshot();
const cache = new Map<string, CacheEntry>();
const pending = new Map<
  string,
  {
    resolve: (message: Envelope) => void;
    reject: (error: Error) => void;
    timer: ReturnType<typeof setTimeout>;
    generation: number;
  }
>();
const popups = new Set<Browser.runtime.Port>();
let native: Browser.runtime.Port | null = null;
let generation = 0;
let retryMs = 1000;
let retryTimer: ReturnType<typeof setTimeout> | undefined;
let diskTimer: ReturnType<typeof setInterval> | undefined;

export default defineBackground(() => {
  void restore().then(() => connect());
  browser.runtime.onConnect.addListener((port) => {
    if (port.name !== "popup") return;
    popups.add(port);
    port.postMessage({ type: "SNAPSHOT", snapshot: state });
    port.onMessage.addListener((message) => {
      void onPopup(message as PopupMessage);
    });
    port.onDisconnect.addListener(() => popups.delete(port));
  });
  browser.runtime.onMessage.addListener((message) => {
    if (message?.type === "PAGE_VIDEO") applyPage(message.payload as PageVideo);
    if (message?.type === "PAGE_CLEAR") clearPage();
  });
});

type PopupMessage =
  | { type: "GET_STATE" }
  | { type: "ACTIVE_TAB"; url?: string; title?: string }
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
  const stored = await browser.storage.local.get("streamgrab");
  const data = stored.streamgrab as
    | { cache?: CacheEntry[]; jobs?: Job[]; hiddenJobIds?: string[]; settings?: Settings }
    | undefined;
  for (const entry of data?.cache ?? []) {
    if (isFresh(entry.fetchedAt) && entry.presets) cache.set(entry.id, entry);
  }
  state.jobs = data?.jobs ?? [];
  state.hiddenJobIds = data?.hiddenJobIds ?? [];
  state.settings = { ...defaultSettings(), ...data?.settings };
}

function connect(): void {
  if (retryTimer) clearTimeout(retryTimer);
  state.host = "connecting";
  broadcast();
  try {
    native = browser.runtime.connectNative(HOST_NAME);
  } catch {
    failHost();
    return;
  }
  native.onMessage.addListener(onNative);
  native.onDisconnect.addListener(() => {
    native = null;
    failHost();
  });
  void handshake();
}

async function handshake(): Promise<void> {
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
    }
    retryMs = 1000;
    await refreshJobs();
    await refreshDisk();
    if (state.page && !state.info) void loadInfo(state.page);
    broadcast();
    armDiskTimer();
  } catch {
    failHost();
  }
}

function notify(text: string): void {
  state.notice = text;
  state.noticeId += 1;
}

function failHost(): void {
  native = null;
  if (state.host !== "incompatible") state.host = "disconnected";
  state.handshake = state.host === "incompatible" ? state.handshake : null;
  broadcast();
  if (state.host === "disconnected") {
    retryTimer = setTimeout(connect, retryMs);
    retryMs = Math.min(retryMs * 2, 30_000);
  }
}

function onNative(message: Envelope): void {
  const waiter = pending.get(message.id);
  if (waiter && message.type !== "JOB_PROGRESS" && message.type !== "JOB_STATE") {
    clearTimeout(waiter.timer);
    pending.delete(message.id);
    waiter.resolve(message);
    return;
  }
  if (message.type === "JOB_PROGRESS" || message.type === "JOB_STATE") applyJobEvent(message);
  if (message.type === "CORE_STATUS") applyCoreStatus(message);
}

function applyJobEvent(message: Envelope): void {
  const payload = message.payload as {
    job?: Job;
    jobId?: string;
    percent?: number;
    stage?: string;
    bytesDone?: number;
    bytesTotal?: number;
    speedBps?: number;
    etaSec?: number | null;
  };
  if (payload.job) {
    upsertJob(payload.job);
  } else if (payload.jobId) {
    state.jobs = state.jobs.map((job) =>
      job.id === payload.jobId
        ? {
            ...job,
            stage: payload.stage ?? job.stage,
            percent: payload.percent ?? job.percent,
            bytesDone: payload.bytesDone ?? job.bytesDone,
            bytesTotal: payload.bytesTotal ?? job.bytesTotal,
            speedBps: payload.speedBps ?? job.speedBps,
            etaSec: payload.etaSec === undefined ? job.etaSec : payload.etaSec,
            state: payload.stage && isStage(payload.stage) ? payload.stage : job.state,
          }
        : job,
    );
  }
  if (message.type === "JOB_STATE") void persist();
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
  const index = state.jobs.findIndex((item) => item.id === job.id);
  if (index === -1) state.jobs = [job, ...state.jobs];
  else state.jobs = state.jobs.map((item) => (item.id === job.id ? job : item));
}

async function onPopup(message: PopupMessage): Promise<void> {
  switch (message.type) {
    case "GET_STATE":
      broadcast();
      return;
    case "ACTIVE_TAB":
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
        ...state.jobs.filter((job) => ["done", "failed", "cancelled"].includes(job.state)).map((job) => job.id),
      ]);
      await persist();
      broadcast();
      return;
    case "CLEAR_HISTORY":
      state.hiddenJobIds = unique([
        ...state.hiddenJobIds,
        ...state.jobs.filter((job) => job.state === "done").map((job) => job.id),
      ]);
      await persist();
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
      await persist();
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
  const existing = state.jobs.find((job) => job.key === key && !["done", "failed", "cancelled"].includes(job.state));
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
    await persist();
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
    title: title?.replace(/ - YouTube$/, "") || state.page?.title || "",
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
    state.info = null;
    state.infoError = null;
    const cached = cache.get(page.videoId);
    if (cached && isFresh(cached.fetchedAt) && cached.presets) state.info = cached;
  }
  if (!state.info && state.host === "ready") void loadInfo(page);
  broadcast();
}

function clearPage(): void {
  generation += 1;
  state.page = null;
  state.info = null;
  state.infoLoading = false;
  state.infoError = null;
  broadcast();
}

async function loadInfo(page: PageVideo): Promise<void> {
  const token = ++generation;
  state.infoLoading = true;
  state.infoError = null;
  broadcast();
  try {
    const response = await request("GET_INFO", { url: page.url });
    if (token !== generation) return;
    if (!response.ok) {
      state.infoError = response.error ?? { code: "EXTRACTION_FAILED", message: "StreamGrab couldn't read this video." };
      state.infoLoading = false;
      broadcast();
      return;
    }
    const info = { ...(response.payload as Omit<VideoInfo, "fetchedAt">), fetchedAt: Date.now() };
    state.info = info;
    state.infoLoading = false;
    cache.set(info.id, info);
    await persist();
    broadcast();
  } catch {
    if (token !== generation) return;
    state.infoLoading = false;
    state.infoError = { code: "NETWORK_ERROR", message: "The companion didn't answer." };
    broadcast();
  }
}

async function refreshJobs(): Promise<void> {
  const response = await request("LIST_JOBS", {});
  if (!response.ok) return;
  const payload = response.payload as { jobs?: Job[] };
  if (payload.jobs) state.jobs = payload.jobs;
  await persist();
}

async function refreshDisk(): Promise<void> {
  const response = await request("GET_DISK_INFO", {}).catch(() => null);
  if (!response?.ok) return;
  state.disk = response.payload as Snapshot["disk"];
}

function armDiskTimer(): void {
  if (diskTimer) clearInterval(diskTimer);
  diskTimer = setInterval(() => {
    if (state.jobs.some((job) => activeState(job.state))) void refreshDisk().then(broadcast);
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
  const generationNow = generation;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error("timeout"));
    }, timeoutMs);
    pending.set(id, { resolve, reject, timer, generation: generationNow });
    native?.postMessage({ id, type, payload });
  });
}

async function persist(): Promise<void> {
  const fresh = [...cache.values()].filter((entry) => Date.now() - entry.fetchedAt < CACHE_MS);
  await browser.storage.local.set({
    streamgrab: { cache: fresh, jobs: state.jobs, hiddenJobIds: state.hiddenJobIds, settings: state.settings },
  });
}

function broadcast(): void {
  const snapshot = structuredClone(state);
  for (const port of popups) port.postMessage({ type: "SNAPSHOT", snapshot });
}

function parse(url?: string): Pick<PageVideo, "videoId" | "url" | "kind"> | null {
  const parsed = parseYouTubeUrl(url);
  return parsed ? { videoId: parsed.videoId, url: parsed.url, kind: parsed.kind } : null;
}

function activeState(stateName: string): boolean {
  return ["downloading_video", "downloading_audio", "muxing", "paused"].includes(stateName);
}

function isStage(value: string): value is Job["state"] {
  return ["queued", "downloading_video", "downloading_audio", "muxing", "paused"].includes(value);
}

function unique(values: string[]): string[] {
  return [...new Set(values)];
}
