import { useState } from "preact/hooks";
import { formatBytes, formatEta, formatSpeed } from "../../../lib/format";
import type { Job } from "../../../protocol/types";
import { Icon } from "../icons";
import { jobStage } from "../labels";

export function ActiveTask({ job: running, send: portSend }: { job: Job; send: (message: Record<string, unknown>) => void }) {
  const [confirmId, setConfirmId] = useState<string | null>(null);
  return (
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
  );
}
