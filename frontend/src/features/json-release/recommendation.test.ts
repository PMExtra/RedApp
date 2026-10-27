import { describe, expect, it } from "vitest";
import { detectEnvironment, recommend, type Artifact } from "./recommendation";
const artifact = (
  key: string,
  os: Artifact["os"],
  arch: Artifact["arch"],
  format = "zip",
): Artifact => ({ key, os, arch, format, url: "/local", sha256: "a".repeat(64) });
describe("package recommendations", () => {
  it("does not guess architecture from Windows or Intel Mac compatibility tokens", () => {
    expect(detectEnvironment("Windows NT 10.0; Win64; x64")).toEqual({
      os: "windows",
      arch: undefined,
    });
    expect(detectEnvironment("Macintosh; Intel Mac OS X 10_15_7")).toEqual({
      os: "macos",
      arch: undefined,
    });
    expect(recommend([artifact("win", "windows", "x64")], { os: "windows" })).toBeUndefined();
  });
  it("uses optional browser hints and permits one universal package", () => {
    expect(
      detectEnvironment("", { platform: "Windows", architecture: "arm", bitness: "64" }),
    ).toEqual({ os: "windows", arch: "arm64" });
    expect(recommend([artifact("mac", "macos", "universal")], { os: "macos" })).toBe("mac");
  });
  it("does not choose Linux packaging or substitute another architecture", () => {
    expect(
      recommend([artifact("deb", "linux", "x64", "deb"), artifact("rpm", "linux", "x64", "rpm")], {
        os: "linux",
        arch: "x64",
      }),
    ).toBeUndefined();
    expect(
      recommend([artifact("win", "windows", "x64")], { os: "windows", arch: "arm64" }),
    ).toBeUndefined();
    expect(recommend([artifact("win", "windows", "x64")], {})).toBeUndefined();
  });
});
