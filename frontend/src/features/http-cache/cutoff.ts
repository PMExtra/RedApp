const LOCAL_MINUTE = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/;

/**
 * Converts a `datetime-local` value (browser time zone, minute precision) to
 * an RFC 3339 UTC instant. Returns `null` for malformed values and for local
 * times that do not exist (skipped by a daylight saving change).
 */
export function localToUtc(value: string): string | null {
  const match = LOCAL_MINUTE.exec(value);
  if (!match) return null;
  const [, year, month, day, hour, minute] = match.map(Number) as [
    number,
    number,
    number,
    number,
    number,
    number,
  ];
  const date = new Date(year, month - 1, day, hour, minute);
  const roundTrip =
    date.getFullYear() === year &&
    date.getMonth() === month - 1 &&
    date.getDate() === day &&
    date.getHours() === hour &&
    date.getMinutes() === minute;
  return roundTrip ? date.toISOString() : null;
}

/** The browser's IANA time zone, for labelling local inputs. */
export function localTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}
