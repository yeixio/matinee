import { describe, expect, it } from "vitest";
import { includesTV, offersTV, withTV } from "./Collections";

const libs = [
  { id: 1, kind: "movies" },
  { id: 2, kind: "tv" },
  { id: 3, kind: "music" },
];

describe("Include TV shows", () => {
  it("is offered when the rules cover movies and TV", () => {
    expect(offersTV({}, libs)).toBe(true);
    expect(offersTV({ library: [1, 2] }, libs)).toBe(true);
    expect(offersTV({ kind: ["movie"] }, libs)).toBe(true);
  });
  it("is not offered for one kind of library, or other kinds of titles", () => {
    expect(offersTV({ library: [1] }, libs)).toBe(false);
    expect(offersTV({ library: [2] }, libs)).toBe(false);
    expect(offersTV({}, [libs[0]])).toBe(false);
    expect(offersTV({ kind: ["album"] }, libs)).toBe(false);
    expect(offersTV({}, undefined)).toBe(false);
  });
  it("keeps shows out unless asked", () => {
    expect(withTV({ genre: ["Horror"] }, false)).toEqual({
      genre: ["Horror"],
      kind: ["movie"],
    });
    expect(withTV({ genre: ["Horror"] }, true)).toEqual({
      genre: ["Horror"],
      kind: ["movie", "show"],
    });
  });
  it("reads collections made before as including shows", () => {
    expect(includesTV({})).toBe(true);
    expect(includesTV({ kind: ["movie", "show"] })).toBe(true);
    expect(includesTV({ kind: ["movie"] })).toBe(false);
  });
});
