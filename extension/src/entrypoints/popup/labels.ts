import { formatBytes, stageLabel } from "../../lib/format";
import type { Job } from "../../protocol/types";

export function jobSummary(job: Job): string {
  const size = formatBytes(job.bytesTotal || job.estimatedBytes);
  if (job.kind === "audio") return `${job.container.toUpperCase()} · ${size}`;
  const container = (job.container || "mp4").toUpperCase();
  return `${job.height}p ${container} · ${size}`;
}

export function jobStage(job: Job): string {
  if (job.kind === "audio" && (job.stage === "muxing" || job.state === "muxing")) return "Tagging";
  return stageLabel(job.stage) || job.state;
}
