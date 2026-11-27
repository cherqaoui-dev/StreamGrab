import { formatSize } from "../../../lib/format";
import type { FormatRow, Settings, Snapshot } from "../../../protocol/types";
import { Icon } from "../icons";
import { moveContainer, moveRow } from "../navigation";
import { Blocked, Empty, Skeleton } from "./common";

const containers = [
  { id: "mp4", label: "MP4" },
  { id: "webm", label: "WebM" },
  { id: "mkv", label: "MKV" },
] as const;

export function VideoPanel({
  snapshot,
  settings,
  rows,
  selected,
  setSelected,
  disabled,
  onGet,
  onChange,
  onUpdate,
}: {
  snapshot: Snapshot;
  settings: Settings;
  rows: FormatRow[];
  selected: number;
  setSelected: (index: number) => void;
  disabled: boolean;
  onGet: (row: FormatRow) => void;
  onChange: (patch: Partial<Settings>) => void;
  onUpdate: () => void;
}) {
  const blocked = snapshot.infoError;
  return (
    <section class="panel" role="tabpanel">
      <div class="panel-head">
        <span>Available stream qualities</span>
        <div class="segment" role="radiogroup" aria-label="Container">
          {containers.map((item) => (
            <button
              key={item.id}
              type="button"
              role="radio"
              data-container={item.id}
              aria-checked={settings.container === item.id}
              tabIndex={settings.container === item.id ? 0 : -1}
              class={settings.container === item.id ? "on" : ""}
              onClick={() => onChange({ container: item.id })}
              onKeyDown={(event) => moveContainer(event, item.id, onChange)}
            >
              {item.label}
            </button>
          ))}
        </div>
      </div>
      {snapshot.infoLoading && <Skeleton />}
      {!snapshot.page && <Empty text="Open a YouTube video to get started" />}
      {snapshot.page && blocked && <Blocked error={blocked} onUpdate={onUpdate} />}
      {rows.map((row, index) => (
        <article key={row.height} class={index === selected ? "row selected" : "row"}>
          <div>
            <div class="row-title">
              <strong>{row.label}</strong>
              {row.fps >= 50 && <em>{row.fps}fps</em>}
              {row.hdr && <em>HDR</em>}
              {row.fastest && <em class="fast">Fastest</em>}
            </div>
            <p>
              {row.videoCodec}
              {row.audioCodec ? ` · ${row.audioCodec}` : ""} · {formatSize(row.estimatedBytes, row.exact)}
              {row.transcoded ? " · transcoded audio" : ""}
            </p>
          </div>
          <button
            type="button"
            data-get={index}
            class={index === 0 ? "get primary" : "get"}
            aria-disabled={disabled || row.available === false}
            title={row.available === false ? row.reason : "Download this tier"}
            onClick={() => {
              setSelected(index);
              onGet(row);
            }}
            onFocus={() => setSelected(index)}
            onKeyDown={(event) => moveRow(event, index, rows.length, "data-get", setSelected)}
          >
            <Icon name="download" />
            Get
          </button>
        </article>
      ))}
      {snapshot.page && !snapshot.infoLoading && !blocked && rows.length === 0 && snapshot.host === "ready" && (
        <Empty text="No tiers were found for this container" />
      )}
    </section>
  );
}
