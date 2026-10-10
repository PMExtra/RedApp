import { useI18n } from "vue-i18n";
import type { Locale } from "./index";

const UNKNOWN = "—";
const IEC_UNITS = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];

type DateInput = string | Date | null | undefined;

function finite(value: number | null | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function toDate(value: DateInput): Date | undefined {
  if (value === null || value === undefined || value === "") return undefined;
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

export function formatNumber(locale: Locale, value: number | null | undefined): string {
  return finite(value) ? new Intl.NumberFormat(locale).format(value) : UNKNOWN;
}

/** IEC units with 1024 steps and two decimals; unknown or negative values show "—". */
export function formatBytes(locale: Locale, value: number | null | undefined): string {
  if (!finite(value) || value < 0) return UNKNOWN;
  let unit = 0;
  let scaled = value;
  while (scaled >= 1024 && unit < IEC_UNITS.length - 1) {
    scaled /= 1024;
    unit++;
  }
  const number = new Intl.NumberFormat(locale, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(scaled);
  return `${number} ${IEC_UNITS[unit] ?? "B"}`;
}

/** Local date and time of an RFC 3339 timestamp from the API. */
export function formatDateTime(
  locale: Locale,
  value: DateInput,
  options: Intl.DateTimeFormatOptions = { dateStyle: "medium", timeStyle: "short" },
): string {
  const date = toDate(value);
  return date ? new Intl.DateTimeFormat(locale, options).format(date) : UNKNOWN;
}

const RELATIVE_STEPS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["second", 60],
  ["minute", 60],
  ["hour", 24],
  ["day", 30],
  ["month", 12],
  ["year", Number.POSITIVE_INFINITY],
];

/** "3 minutes ago" / "in 2 hours", relative to `now`. */
export function formatRelativeTime(locale: Locale, value: DateInput, now = new Date()): string {
  const date = toDate(value);
  if (!date) return UNKNOWN;
  let delta = (date.getTime() - now.getTime()) / 1000;
  const format = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  for (const [unit, size] of RELATIVE_STEPS) {
    if (Math.abs(delta) < size) return format.format(Math.round(delta), unit);
    delta /= size;
  }
  return UNKNOWN;
}

const DURATION_UNITS: [string, number][] = [
  ["day", 86400],
  ["hour", 3600],
  ["minute", 60],
];

/** Seconds in the largest unit that divides them exactly: 7200 → "2 hr", 90 → "90 sec". */
export function formatDuration(locale: Locale, seconds: number | null | undefined): string {
  if (!finite(seconds) || seconds < 0) return UNKNOWN;
  const [unit, size] = DURATION_UNITS.find(([, s]) => seconds >= s && seconds % s === 0) ?? [
    "second",
    1,
  ];
  return new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay: "short" }).format(
    seconds / size,
  );
}

/** Formatters bound to the current UI locale. */
export function useFormat() {
  const { locale } = useI18n();
  const current = () => locale.value as Locale;
  return {
    number: (value: number | null | undefined) => formatNumber(current(), value),
    bytes: (value: number | null | undefined) => formatBytes(current(), value),
    dateTime: (value: DateInput, options?: Intl.DateTimeFormatOptions) =>
      formatDateTime(current(), value, options),
    relativeTime: (value: DateInput, now?: Date) => formatRelativeTime(current(), value, now),
    duration: (seconds: number | null | undefined) => formatDuration(current(), seconds),
  };
}

export interface LocalizedValue {
  en: string;
  "zh-CN": string;
}

/** The text in `locale`, falling back to the other language when empty. */
export function pickLocalized(text: LocalizedValue | null | undefined, locale: Locale): string {
  if (!text) return "";
  return text[locale] || text.en || text["zh-CN"];
}

/** `localized(app.name)` in the current UI locale. */
export function useLocalized() {
  const { locale } = useI18n();
  return (text: LocalizedValue | null | undefined) => pickLocalized(text, locale.value as Locale);
}

/** A vendor logo for a UI language: the language's logo, else the default `icon`. */
export function vendorLogo(
  vendor: { icon: string; localized_icons: Record<Locale, string> },
  locale: string,
): string {
  return vendor.localized_icons[locale === "zh-CN" ? "zh-CN" : "en"] || vendor.icon;
}
