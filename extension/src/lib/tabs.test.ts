import { describe, expect, it } from "vitest";
import { isFromWatchedTab } from "./tabs";

describe("isFromWatchedTab", () => {
  it("accepts only the watched tab", () => {
    expect(isFromWatchedTab(7, 7)).toBe(true);
    expect(isFromWatchedTab(8, 7)).toBe(false);
  });

  it("rejects messages before a tab is watched or without a tab", () => {
    expect(isFromWatchedTab(7, null)).toBe(false);
    expect(isFromWatchedTab(undefined, null)).toBe(false);
    expect(isFromWatchedTab(undefined, 7)).toBe(false);
  });
});
