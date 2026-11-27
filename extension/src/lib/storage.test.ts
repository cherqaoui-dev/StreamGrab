import { describe, expect, it } from "vitest";
import { defaultSettings } from "../protocol/types";
import { KEYS, LEGACY_KEY, readStored, toStored } from "./storage";

describe("storage", () => {
  it("reads the old single key", () => {
    expect(readStored({ [LEGACY_KEY]: { hiddenJobIds: ["a"] } }).hiddenJobIds).toEqual(["a"]);
  });

  it("prefers the new keys", () => {
    const raw = { [LEGACY_KEY]: { hiddenJobIds: ["old"] }, [KEYS.hiddenJobIds]: ["new"] };
    expect(readStored(raw).hiddenJobIds).toEqual(["new"]);
  });

  it("writes only the requested parts", () => {
    const values = { cache: [], jobs: [], hiddenJobIds: [], settings: defaultSettings() };
    expect(Object.keys(toStored(values, ["settings"]))).toEqual([KEYS.settings]);
  });
});
