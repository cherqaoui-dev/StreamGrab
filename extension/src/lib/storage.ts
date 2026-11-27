import type { Job, Settings, VideoInfo } from "../protocol/types";

/** Each part has its own key, so saving one doesn't rewrite the others. */
export const KEYS = {
  cache: "streamgrab.cache",
  jobs: "streamgrab.jobs",
  hiddenJobIds: "streamgrab.hidden",
  settings: "streamgrab.settings",
} as const;

/** The single key that earlier builds kept everything under. */
export const LEGACY_KEY = "streamgrab";

export type Stored = {
  cache?: VideoInfo[];
  jobs?: Job[];
  hiddenJobIds?: string[];
  settings?: Settings;
};

export type Part = keyof typeof KEYS;

export function readStored(raw: Record<string, unknown>): Stored {
  const legacy = (raw[LEGACY_KEY] ?? {}) as Stored;
  return {
    cache: (raw[KEYS.cache] as Stored["cache"]) ?? legacy.cache,
    jobs: (raw[KEYS.jobs] as Stored["jobs"]) ?? legacy.jobs,
    hiddenJobIds: (raw[KEYS.hiddenJobIds] as Stored["hiddenJobIds"]) ?? legacy.hiddenJobIds,
    settings: (raw[KEYS.settings] as Stored["settings"]) ?? legacy.settings,
  };
}

export function toStored(values: Required<Stored>, parts: Part[]): Record<string, unknown> {
  return Object.fromEntries(parts.map((part) => [KEYS[part], values[part]]));
}
