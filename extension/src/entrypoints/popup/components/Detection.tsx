import { formatDuration, formatViews } from "../../../lib/format";
import type { Snapshot } from "../../../protocol/types";

export function Detection({ snapshot }: { snapshot: Snapshot }) {
  const page = snapshot.page;
  const info = snapshot.info;
  if (!page) {
    return (
      <section class="detect empty-card">
        <p>Open a YouTube video to get started</p>
      </section>
    );
  }
  const title = info?.title || page.title || "YouTube video";
  const channel = info?.channel || page.channel;
  const views = info ? formatViews(info.viewCount) : "";
  const chips = info?.chips ?? [];
  return (
    <section class="detect">
      <div class="thumb">
        <img src={info?.thumbnail || page.thumbnail} alt="" />
        {info?.durationSec ? <span class="duration">{formatDuration(info.durationSec)}</span> : null}
        <span class={snapshot.infoLoading ? "pulse live" : "pulse"} title={snapshot.infoLoading ? "Detecting" : "Video detected"} />
        {page.kind === "shorts" && <span class="short">Short</span>}
      </div>
      <div class="detect-copy">
        <h2>{title}</h2>
        <p class="meta">{[channel, views].filter(Boolean).join(" · ")}</p>
        <div class="chips">
          {chips.map((chip) => (
            <span key={chip}>{chip}</span>
          ))}
        </div>
      </div>
    </section>
  );
}
