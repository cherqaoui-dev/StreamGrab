export function step(index: number, delta: number, count: number): number {
  if (count <= 0) return 0;
  return (index + delta + count) % count;
}

const containers = ["mp4", "webm", "mkv"] as const;

export function nextContainer(current: string, delta: number): (typeof containers)[number] {
  const index = containers.indexOf(current as (typeof containers)[number]);
  return containers[step(index < 0 ? 0 : index, delta, containers.length)];
}
