/**
 * An upstream address: `http(s)://host[:port]`, optionally with a trailing
 * slash. RedApp serves from the root of its host, so no path, query,
 * fragment or credentials. The server verifies the address itself.
 */
const ORIGIN = /^https?:\/\/[^\s/?#@]+\/?$/i;

export function isUpstreamOrigin(value: string): boolean {
  return ORIGIN.test(value.trim());
}

/** The address as sent to the server: trimmed, without a trailing slash. */
export function normalizeOrigin(value: string): string {
  return value.trim().replace(/\/$/, "");
}
