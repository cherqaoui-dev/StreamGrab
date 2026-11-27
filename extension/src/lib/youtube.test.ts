import { describe, expect, it } from "vitest";
import { isFresh, parseYouTubeUrl } from "./youtube";

describe("parseYouTubeUrl", () => {
  it("accepts watch, shorts, live, and youtu.be links", () => {
    expect(parseYouTubeUrl("https://www.youtube.com/watch?v=abcdefghijk")?.kind).toBe("watch");
    expect(parseYouTubeUrl("https://www.youtube.com/shorts/abcdefghijk")?.videoId).toBe("abcdefghijk");
    expect(parseYouTubeUrl("https://www.youtube.com/live/abcdefghijk")?.kind).toBe("live");
    expect(parseYouTubeUrl("https://youtu.be/abcdefghijk")?.url).toBe(
      "https://www.youtube.com/watch?v=abcdefghijk",
    );
  });

  it("rejects other sites and non-video pages", () => {
    expect(parseYouTubeUrl("https://example.com/watch?v=abcdefghijk")).toBeNull();
    expect(parseYouTubeUrl("https://www.youtube.com/feed/subscriptions")).toBeNull();
    expect(parseYouTubeUrl("https://www.youtube.com/watch?v=short")).toBeNull();
  });
});

describe("cache", () => {
  it("expires metadata after 10 minutes", () => {
    const now = 1_000_000;
    expect(isFresh(now - 9 * 60 * 1000, now)).toBe(true);
    expect(isFresh(now - 11 * 60 * 1000, now)).toBe(false);
  });
});
