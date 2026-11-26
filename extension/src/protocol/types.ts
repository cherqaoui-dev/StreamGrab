export const PROTOCOL_VERSION = 1;
export const HOST_NAME = "com.streamgrab.host";
export const CACHE_MS = 10 * 60 * 1000;

export type ApiError = { code: string; message: string };

export type Handshake = {
  protocolVersion: number;
  hostVersion: string;
  ytDlpVersion: string;
  ffmpegVersion: string;
  deno: boolean;
  denoVersion: string;
  downloadDir: string;
  autoUpdate?: boolean;
  updateWarning?: string;
};

export type FormatRow = {
  height: number;
  label: string;
  fps: number;
  hdr: boolean;
  videoCodec: string;
  audioCodec: string;
  estimatedBytes: number;
  exact: boolean;
  fastest: boolean;
  videoFormatId: string;
  audioFormatId: string;
  videoUrl?: string;
  audioUrl?: string;
  container?: Container;
  available?: boolean;
  reason?: string;
  recodeAudio?: boolean;
  transcoded?: boolean;
};

export type Container = "mp4" | "webm" | "mkv";
export type AudioPreset = "flac" | "mp3" | "opus" | "m4a";
export type CodecOrder = "av1-vp9-h264" | "vp9-av1-h264" | "h264-vp9-av1";

export type AudioRow = {
  id: AudioPreset;
  label: string;
  spec: string;
  tag?: string;
  transcoded: boolean;
  copy: boolean;
  source: string;
  estimatedBytes: number;
  exact: boolean;
  formatId: string;
  container: AudioPreset;
  sampleRate?: number;
  url?: string;
};

export type Settings = {
  container: Container;
  codecOrder: CodecOrder;
  maxHeight: 0 | 720 | 1080 | 1440 | 2160;
  audioFormat: AudioPreset;
  autoUpdate: boolean;
};

export const defaultSettings = (): Settings => ({
  container: "mp4",
  codecOrder: "av1-vp9-h264",
  maxHeight: 0,
  audioFormat: "mp3",
  autoUpdate: true,
});

export type VideoInfo = {
  id: string;
  url: string;
  title: string;
  channel: string;
  thumbnail: string;
  durationSec: number;
  viewCount: number;
  kind: "watch" | "shorts" | "live";
  rows: FormatRow[];
  chips: string[];
  audio?: AudioRow[];
  presets?: Record<string, Record<Container, FormatRow[]>>;
  fetchedAt: number;
};

export type PageVideo = {
  videoId: string;
  url: string;
  kind: "watch" | "shorts" | "live";
  title: string;
  channel: string;
  thumbnail: string;
};

export type JobState =
  | "queued"
  | "downloading_video"
  | "downloading_audio"
  | "muxing"
  | "done"
  | "paused"
  | "failed"
  | "cancelled";

export type Job = {
  id: string;
  key: string;
  videoId: string;
  url: string;
  title: string;
  thumbnail: string;
  height: number;
  container: string;
  kind?: "video" | "audio";
  recodeAudio?: boolean;
  sampleRate?: number;
  videoFormatId: string;
  audioFormatId: string;
  estimatedBytes: number;
  state: JobState;
  stage: string;
  percent: number;
  bytesDone: number;
  bytesTotal: number;
  speedBps: number;
  etaSec: number | null;
  outputPath?: string;
  error?: ApiError;
  createdAt: string;
  resumeDownload?: boolean;
};

export type DiskInfo = { freeBytes: number; path: string };

export type HostStatus = "disconnected" | "connecting" | "ready" | "incompatible";

export type Snapshot = {
  host: HostStatus;
  handshake: Handshake | null;
  page: PageVideo | null;
  info: VideoInfo | null;
  infoLoading: boolean;
  infoError: ApiError | null;
  jobs: Job[];
  disk: DiskInfo | null;
  hiddenJobIds: string[];
  focusJobId: string | null;
  notice: string | null;
  noticeId: number;
  settings: Settings;
  updateWarning: string | null;
};

export type Envelope = {
  id: string;
  type: string;
  ok?: boolean;
  payload?: unknown;
  error?: ApiError;
};

export const emptySnapshot = (): Snapshot => ({
  host: "disconnected",
  handshake: null,
  page: null,
  info: null,
  infoLoading: false,
  infoError: null,
  jobs: [],
  disk: null,
  hiddenJobIds: [],
  focusJobId: null,
  notice: null,
  noticeId: 0,
  settings: defaultSettings(),
  updateWarning: null,
});
