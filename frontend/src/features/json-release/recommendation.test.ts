import { describe, expect, it } from "vitest";
import { detectEnvironment, recommend, type Artifact } from "./recommendation";
const artifact = (
  key: string,
  os: Artifact["os"],
  arch: Artifact["arch"],
  format = "zip",
  hidden = false,
): Artifact => ({
  key,
  os,
  arch,
  format,
  url: "/local",
  path: "releases/r/" + key,
  sha256: "a".repeat(64),
  hidden,
});
// The VS Code preset's packages, in release order.
const vscode = [
  artifact("win32-x64-user", "windows", "x64", "exe"),
  artifact("win32-arm64-user", "windows", "arm64", "exe"),
  artifact("win32-x64", "windows", "x64", "exe"),
  artifact("darwin-universal", "macos", "universal"),
  artifact("linux-x64", "linux", "x64", "tar.gz"),
  artifact("linux-deb-x64", "linux", "x64", "deb"),
];
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
    expect(
      detectEnvironment("", { platform: "Windows", architecture: "arm", bitness: "64" }),
    ).toEqual({ os: "windows", arch: "arm64" });
  });
  it("prefers the default among the packages for the environment", () => {
    // Windows without architecture hints, as in Firefox.
    expect(recommend(vscode, { os: "windows" }, "win32-x64-user")).toBe("win32-x64-user");
    expect(recommend(vscode, { os: "windows", arch: "x64" }, "win32-x64")).toBe("win32-x64");
    expect(recommend(vscode, { os: "windows", arch: "arm64" }, "win32-x64-user")).toBe(
      "win32-arm64-user",
    );
    expect(recommend(vscode, { os: "macos" }, "win32-x64-user")).toBe("darwin-universal");
    // Several Linux packages: the first in release order.
    expect(recommend(vscode, { os: "linux", arch: "x64" }, "win32-x64-user")).toBe("linux-x64");
  });
  it("falls back to the default, then the first visible package", () => {
    expect(recommend(vscode, {}, "win32-x64-user")).toBe("win32-x64-user");
    expect(recommend(vscode, { os: "linux", arch: "arm64" }, "win32-x64-user")).toBe(
      "win32-x64-user",
    );
    expect(recommend(vscode, {}, null)).toBe("win32-x64-user");
    expect(recommend(vscode, {}, "gone")).toBe("win32-x64-user");
  });
  it("prefers a package that also runs emulated when the architecture is unknown", () => {
    // Safari and Firefox on any Mac report "Intel": x64 runs on Apple silicon too.
    const jetbrains = [
      artifact("macos-arm64-dmg", "macos", "arm64", "dmg"),
      artifact("macos-x64-dmg", "macos", "x64", "dmg"),
      artifact("windows-x64-exe", "windows", "x64", "exe"),
    ];
    expect(recommend(jetbrains, { os: "macos" }, "windows-x64-exe")).toBe("macos-x64-dmg");
    expect(recommend(jetbrains, { os: "macos", arch: "arm64" }, "windows-x64-exe")).toBe(
      "macos-arm64-dmg",
    );
  });
  it("never offers hidden packages", () => {
    const hidden = vscode.map((a) => ({ ...a, hidden: a.key !== "linux-deb-x64" }));
    expect(recommend(hidden, { os: "windows" }, "win32-x64-user")).toBe("linux-deb-x64");
    expect(recommend(hidden, { os: "linux", arch: "x64" }, null)).toBe("linux-deb-x64");
    expect(recommend([artifact("a", "windows", "x64", "exe", true)], {}, "a")).toBeUndefined();
  });
});
