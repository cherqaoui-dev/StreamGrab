import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { formatBytes, formatDuration, formatRelative, formatSize } from "./format";

describe("format", () => {
  it("formats sizes and durations", () => {
    expect(formatBytes(345_000_000)).toBe("329 MB");
    expect(formatSize(1000, false)).toBe("~1000 B");
    expect(formatDuration(38 * 60 + 42)).toBe("38:42");
    expect(formatDuration(3723)).toBe("1:02:03");
  });

  it("formats relative timestamps", () => {
    const now = Date.parse("2026-11-25T12:00:00Z");
    expect(formatRelative("2026-11-25T10:00:00Z", now)).toBe("2h ago");
    expect(formatRelative("2026-11-24T12:00:00Z", now)).toBe("Yesterday");
  });

  it("reads the shared handshake fixture", () => {
    const raw = JSON.parse(
      readFileSync(new URL("../../../protocol/fixtures/handshake.json", import.meta.url), "utf8"),
    );
    expect(raw.payload.protocolVersion).toBe(1);
    expect(raw.payload.ytDlpVersion).toBeTruthy();
  });
});
