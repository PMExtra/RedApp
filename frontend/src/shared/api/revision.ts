/** Quoted revision for `If-Match`, the format the spec requires (`"7"`). */
export function ifMatch(revision: number): string {
  if (!Number.isSafeInteger(revision) || revision < 1) {
    throw new RangeError(`invalid revision ${revision}`);
  }
  return `"${revision}"`;
}

/** `{ "If-Match": "\"7\"" }` for the `params.header` of a conditional write. */
export function ifMatchHeader(resource: { revision: number }): { "If-Match": string } {
  return { "If-Match": ifMatch(resource.revision) };
}

/** Parses an `ETag: "7"` header; weak or malformed validators return undefined. */
export function revisionFromEtag(etag: string | null | undefined): number | undefined {
  const match = /^"([1-9][0-9]{0,17})"$/.exec(etag ?? "");
  if (!match?.[1]) return undefined;
  const revision = Number(match[1]);
  return Number.isSafeInteger(revision) ? revision : undefined;
}
