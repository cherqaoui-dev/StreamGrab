import type { Job, JobState } from "../protocol/types";

const LIVE: JobState[] = ["downloading_video", "downloading_audio", "muxing", "paused"];
const FINISHED: JobState[] = ["done", "failed", "cancelled"];

/** A job that holds the queue: downloading, merging, or paused. */
export function isLive(job: Job): boolean {
  return LIVE.includes(job.state);
}

export function isFinished(job: Job): boolean {
  return FINISHED.includes(job.state);
}

export function replaceJob(jobs: Job[], next: Job): Job[] {
  return jobs.map((job) => (job.id === next.id ? next : job));
}

export type Progress = {
  jobId: string;
  stage?: string;
  percent?: number;
  bytesDone?: number;
  bytesTotal?: number;
  speedBps?: number;
  etaSec?: number | null;
};

export function withProgress(job: Job, progress: Progress): Job {
  const stage = progress.stage ?? job.stage;
  return {
    ...job,
    stage,
    percent: progress.percent ?? job.percent,
    bytesDone: progress.bytesDone ?? job.bytesDone,
    bytesTotal: progress.bytesTotal ?? job.bytesTotal,
    speedBps: progress.speedBps ?? job.speedBps,
    etaSec: progress.etaSec === undefined ? job.etaSec : progress.etaSec,
    state: LIVE.includes(stage as JobState) ? (stage as JobState) : job.state,
  };
}
