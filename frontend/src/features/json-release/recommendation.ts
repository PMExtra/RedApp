import type { Schema } from "@/shared/api";
export type Artifact = Schema<"ReleaseArtifact">;
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
export function recommend(artifacts: Artifact[], environment: Environment): string | undefined {
  if (!environment.os) return;
  const matches = artifacts.filter(
    (a) =>
      a.os === environment.os &&
      (a.arch === "universal" || (environment.arch !== undefined && a.arch === environment.arch)),
  );
  // Never guess a package manager, installation scope or competing package.
  return matches.length === 1 ? matches[0]?.key : undefined;
}
