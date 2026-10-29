import type { Schema } from "@/shared/api";
export type Artifact = Schema<"ReleaseDownloadChoice">;
export interface Environment {
  os?: Artifact["os"];
  arch?: Artifact["arch"];
}
export function detectEnvironment(
  userAgent: string,
  hints?: { platform?: string; architecture?: string; bitness?: string },
): Environment {
  const platform = hints?.platform ?? userAgent;
  const os = /Windows/i.test(platform)
    ? "windows"
    : /macOS|Macintosh|Mac OS/i.test(platform)
      ? "macos"
      : /Linux/i.test(platform)
        ? "linux"
        : undefined;
  let arch: Environment["arch"];
  if (hints?.architecture === "arm" && hints.bitness === "64") arch = "arm64";
  else if (hints?.architecture === "x86" && hints.bitness === "64") arch = "x64";
  else if (hints?.architecture === "x86" && hints.bitness === "32") arch = "x86";
  else if (/aarch64|arm64/i.test(userAgent)) arch = "arm64";
  else if (os === "linux" && /x86_64/.test(userAgent)) arch = "x64";
  // macOS Intel and Windows Win64 UA tokens may describe an emulated browser.
  return { os, arch };
}
/**
 * The package offered first. Candidates are the visible packages for the
 * detected OS and, when known, architecture (universal packages always fit):
 * the configured default when it is one, else the first in release order
 * (stored releases list their packages by key).
 * Without a detected OS or any candidate, the default, else the first visible
 * package. Undefined only when no package is visible.
 */
export function recommend(
  artifacts: Artifact[],
  environment: Environment,
  defaultKey?: string | null,
): string | undefined {
  const visible = artifacts.filter((a) => !a.hidden);
  const fallback = visible.find((a) => a.key === defaultKey) ?? visible[0];
  if (!environment.os) return fallback?.key;
  const candidates = visible.filter(
    (a) =>
      a.os === environment.os &&
      (environment.arch === undefined || a.arch === "universal" || a.arch === environment.arch),
  );
  return (candidates.find((a) => a.key === defaultKey) ?? candidates[0] ?? fallback)?.key;
}
