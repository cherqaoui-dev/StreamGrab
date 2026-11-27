import { formatRelative } from "../../../lib/format";
import type { Job } from "../../../protocol/types";
import { Icon } from "../icons";
import { jobSummary } from "../labels";
import { Empty } from "./common";

export function HistoryPanel({
  jobs: history,
  send: portSend,
}: {
  jobs: Job[];
  send: (message: Record<string, unknown>) => void;
}) {
  return (
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
  );
}
