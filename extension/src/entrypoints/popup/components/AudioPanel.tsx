import { useState } from "preact/hooks";
import { formatSize } from "../../../lib/format";
import type { AudioRow, Settings, Snapshot } from "../../../protocol/types";
import { Icon } from "../icons";
import { moveRow } from "../navigation";
import { Blocked, Empty, Skeleton } from "./common";

export function AudioPanel({
  snapshot,
  settings,
  selected: audioIndex,
  setSelected: setAudioIndex,
  disabled,
  onGet,
  onUpdate,
}: {
  snapshot: Snapshot;
  settings: Settings;
  selected: number;
  setSelected: (index: number) => void;
  disabled: boolean;
  onGet: (row: AudioRow) => void;
  onUpdate: () => void;
}) {
  const [tip, setTip] = useState<string | null>(null);
  const blocked = snapshot.infoError;
  const audioRows = snapshot.info?.audio ?? [];
  return (
    <section class="panel" role="tabpanel">
      <div class="panel-head">
        <span>Audio extraction</span>
        <span class="accent">Tags + cover</span>
      </div>
      {snapshot.infoLoading && <Skeleton />}
      {!snapshot.page && <Empty text="Open a YouTube video to get started" />}
      {snapshot.page && blocked && <Blocked error={blocked} onUpdate={onUpdate} />}
      {snapshot.page && !snapshot.infoLoading && !blocked && audioRows.length === 0 && snapshot.host === "ready" && (
        <Empty text="No audio stream was found for this video" />
      )}
      {audioRows.map((row, index) => (
        <article key={row.id} class={index === audioIndex ? "row selected" : "row"}>
          <div class="grow">
            <div class="row-title">
              <strong>{row.label}</strong>
              {row.tag && <em class={row.tag === "Direct" ? "fast" : ""}>{row.tag}</em>}
              {row.transcoded && <em>Transcoded</em>}
              {settings.audioFormat === row.id && <em>Default</em>}
              {row.id !== "opus" && (
                <button type="button" class="info" aria-label="About this conversion" onClick={() => setTip(tip === row.id ? null : row.id)}>
                  i
                </button>
              )}
            </div>
            <p>{row.spec} · {formatSize(row.estimatedBytes, row.exact)}</p>
            {tip === row.id && (
              <p class="tip">YouTube audio is lossy. This format is converted from the best available source ({row.source}). It won't add detail but preserves it in your chosen format.</p>
            )}
          </div>
          <button
            type="button"
            class="get"
            data-audio={row.id}
            data-audio-index={index}
            aria-disabled={disabled || !row.formatId}
            onClick={() => {
              setAudioIndex(index);
              onGet(row);
            }}
            onFocus={() => setAudioIndex(index)}
            onKeyDown={(event) => moveRow(event, index, audioRows.length, "data-audio-index", setAudioIndex)}
          >
            <Icon name="download" />
            Get
          </button>
        </article>
      ))}
    </section>
  );
}
