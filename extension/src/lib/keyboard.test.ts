import { describe, expect, it } from "vitest";
import { nextContainer, step } from "./keyboard";

describe("keyboard movement", () => {
  it("wraps quality rows in both directions", () => {
    expect(step(0, -1, 4)).toBe(3);
    expect(step(3, 1, 4)).toBe(0);
    expect(step(1, 1, 4)).toBe(2);
  });

  it("moves the container radios", () => {
    expect(nextContainer("mp4", 1)).toBe("webm");
    expect(nextContainer("mkv", 1)).toBe("mp4");
    expect(nextContainer("webm", -1)).toBe("mp4");
  });
});
