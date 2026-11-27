import { formatSpeed } from "../../../lib/format";
import { isFinished, isLive } from "../../../lib/jobs";
import type { Job } from "../../../protocol/types";
import { Icon } from "../icons";
import { jobSummary } from "../labels";
import { Empty } from "./common";

export function QueuePanel({
  jobs: visibleJobs,
  focusJobId,
  send: portSend,
}: {
  jobs: Job[];
  focusJobId: string | null;
  send: (message: Record<string, unknown>) => void;
}) {
  const queuedCount = visibleJobs.filter((job) => !isFinished(job)).length;
  const clearable = visibleJobs.some(isFinished);
  return (
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
            focused={focusJobId === job.id}
            onPause={() => portSend({ type: job.state === "paused" ? "RESUME" : "PAUSE", jobId: job.id })}
            onRemove={() => portSend({ type: "CANCEL", jobId: job.id })}
            onOpen={() => job.outputPath && portSend({ type: "SHOW_IN_FOLDER", path: job.outputPath })}
            onRetry={(container) => portSend({ type: "RETRY_MUX", jobId: job.id, container })}
          />
        ))}
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
  const live = isLive(job);
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
            {job.kind === "audio" ? (
              <button type="button" onClick={() => onRetry("")}>Retry</button>
            ) : (
              <>
                <button type="button" onClick={() => onRetry(job.container || "mp4")}>Retry mux</button>
                <button type="button" onClick={() => onRetry("mkv")}>Retry as MKV</button>
              </>
            )}
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
