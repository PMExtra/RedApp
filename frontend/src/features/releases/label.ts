/**
 * A version as a short label: a JSON release ID (`r-` and 64 hex digits)
 * keeps its first 8 digits; other versions are their own label.
 */
export function versionLabel(version: string): string {
  return /^r-[0-9a-f]{64}$/.test(version) ? `${version.slice(0, 10)}…` : version;
}
