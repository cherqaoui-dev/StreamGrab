export type YouTubeKind = "watch" | "shorts" | "live";

export type ParsedYouTube = {
  videoId: string;
  url: string;
  kind: YouTubeKind;
};

const ID = /^[A-Za-z0-9_-]{11}$/;

export function parseYouTubeUrl(raw: string | undefined | null): ParsedYouTube | null {
  if (!raw) return null;
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    return null;
  }
  if (parsed.protocol !== "https:" && parsed.protocol !== "http:") return null;
  const host = parsed.hostname.toLowerCase();
  if (host === "youtu.be") {
    const id = parsed.pathname.split("/").filter(Boolean)[0] ?? "";
    return ID.test(id) ? canonical(id, "watch") : null;
  }
  if (!["youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com"].includes(host)) {
    return null;
  }
  const parts = parsed.pathname.split("/").filter(Boolean);
  if (parts[0] === "watch") {
    const id = parsed.searchParams.get("v") ?? "";
    return ID.test(id) ? canonical(id, "watch") : null;
  }
  if (parts[0] === "shorts" && ID.test(parts[1] ?? "")) return canonical(parts[1], "shorts");
  if (parts[0] === "live" && ID.test(parts[1] ?? "")) return canonical(parts[1], "live");
  return null;
}

function canonical(videoId: string, kind: YouTubeKind): ParsedYouTube {
  return { videoId, kind, url: `https://www.youtube.com/watch?v=${videoId}` };
}

export function isFresh(fetchedAt: number, now = Date.now()): boolean {
  return now - fetchedAt < 10 * 60 * 1000;
}

export function jobKey(videoId: string, height: number): string {
  return `${videoId}:${height}:mp4`;
}
