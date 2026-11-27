type IconProps = { name: string };

const paths: Record<string, string> = {
  download: "M12 4v10M8 10l4 4 4-4M5 19h14",
  back: "M19 12H5M11 6l-6 6 6 6",
  link: "M10 13a5 5 0 0 0 7.1.1l2-2a5 5 0 0 0-7.1-7.1l-1.2 1.2M14 11a5 5 0 0 0-7.1-.1l-2 2a5 5 0 0 0 7.1 7.1l1.2-1.2",
  tune: "M4 7h16M4 17h16M8 7v0M16 17v0",
  video: "M4 7h11v10H4zM15 10l5-2v8l-5-2z",
  audio: "M5 10v4M9 7v10M13 9v6M17 5v14",
  queue: "M4 7h16M4 12h16M4 17h10",
  history: "M4 12a8 8 0 1 0 2-5.3M4 4v5h5M12 8v5l3 2",
  folder: "M3 7h6l2 2h10v9H3z",
  pause: "M8 5v14M16 5v14",
  close: "M6 6l12 12M18 6 6 18",
  play: "M8 5l12 7-12 7z",
  check: "M5 12l5 5L20 7",
  music: "M9 18V7l10-2v9",
  warning: "M12 4l9 16H3zM12 10v4M12 17v1",
};

export function Icon({ name }: IconProps) {
  return (
    <svg class="icon" viewBox="0 0 24 24" aria-hidden="true">
      <path d={paths[name] ?? paths.download} />
    </svg>
  );
}
