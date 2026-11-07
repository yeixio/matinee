import { describe, expect, it } from "vitest";
import type { Version } from "../api";
import {
  pickVersion,
  resolutionLabel,
  versionDetail,
  versionLabel,
} from "./versions";

const uhd: Version = {
  id: 1,
  height: 2160,
  width: 3840,
  mediaTags: ["4k", "hdr", "dolby-vision"],
};
const hd: Version = {
  id: 2,
  height: 1080,
  width: 1920,
  edition: "Director's Cut",
};
const sd: Version = { id: 3, height: 480, width: 720 };

describe("versions", () => {
  it("labels them", () => {
    expect(versionLabel(uhd)).toBe("4K · Dolby Vision");
    expect(versionLabel(hd)).toBe("1080p · Director's Cut");
    expect(resolutionLabel(800)).toBe("720p");
  });
  it("says how long a cut runs", () => {
    expect(
      versionDetail({ ...hd, durationMs: 201 * 60000, container: "mkv" }),
    ).toMatch(/3.*21.*MKV/);
    expect(
      versionDetail({ ...sd, durationMs: 201 * 60000, container: "mkv" }),
    ).toBe("MKV");
  });
  it("picks the biggest picture that fits the screen", () => {
    expect(pickVersion([uhd, hd, sd], { maxHeight: 1080 })?.id).toBe(2);
    expect(pickVersion([uhd, hd, sd], { maxHeight: 2160 })?.id).toBe(1);
    expect(pickVersion([uhd, hd], { maxHeight: 720 })?.id).toBe(2);
  });
  it("prefers one that plays as it is among the same size", () => {
    const other: Version = { id: 4, height: 1080 };
    expect(
      pickVersion([hd, other], { maxHeight: 1080, plays: (v) => v.id === 4 })
        ?.id,
    ).toBe(4);
    expect(pickVersion([hd, other], { maxHeight: 1080 })?.id).toBe(2);
  });
});
