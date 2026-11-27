import { describe, expect, it } from "vitest";
import type { Job } from "../protocol/types";
import { replaceJob, withProgress } from "./jobs";

const job = { id: "a", state: "downloading_video", stage: "downloading_video", percent: 1, etaSec: 9 } as Job;

describe("jobs", () => {
  it("applies progress without losing fields", () => {
    const next = withProgress(job, { jobId: "a", stage: "muxing", percent: 100, etaSec: null });
    expect(next).toMatchObject({ state: "muxing", stage: "muxing", percent: 100, etaSec: null });
    expect(withProgress(job, { jobId: "a" })).toEqual(job);
  });

  it("replaces only the matching job", () => {
    const other = { ...job, id: "b" };
    expect(replaceJob([job, other], { ...job, percent: 50 }).map((item) => item.percent)).toEqual([50, 1]);
  });
});
