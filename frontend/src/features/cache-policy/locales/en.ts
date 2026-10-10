import type { LocaleModule } from "@/shared/i18n";

export default {
  cachePolicy: {
    title: "Cache rules",
    description:
      "How long files stay fresh, what happens when the origin fails, and when old files are removed automatically.",
    templateMissing: "The template is no longer available; the last accepted defaults stay in use.",
    staleFallback: "Serve a stale copy when the origin fails",
    staleFallbackHint:
      "Applies to all paths, including TTL 0. When off, origin failures (network, timeout, 5xx) return an error and stored files are kept.",
    save: "Save cache rules",
    discard: "Discard changes",
    saved: "Cache rules saved.",
    limit: "Up to {max} rules per list. Changes apply after saving.",
    match: {
      type: "Match type",
      glob: "Glob",
      re2: "RE2 regular expression",
      pattern: "Path pattern",
      globHint:
        "Paths start with / and are case-sensitive. A pattern matching a directory also matches everything below it: / matches all files, /releases/ the releases directory, /**/*.zip every zip file.",
      re2Hint: "The expression must match the whole decoded path, not a part of it.",
      tooLong: "Patterns may contain at most 1,024 UTF-8 bytes.",
      required: "Enter a pattern.",
      testTitle: "Test a path",
      sample: "Sample path",
      sampleHint:
        "The decoded path below this application, for example /releases/文件.zip, without query string.",
      test: "Test match",
      matches: "Matches {path}",
      noMatch: "Does not match {path}",
    },
    duration: {
      unit: "Unit of {label}",
      units: { "1": "Seconds", "60": "Minutes", "3600": "Hours", "86400": "Days" },
    },
    rules: {
      title: "Path TTL rules",
      hint: "Rules run from top to bottom; the first matching rule sets the TTL and overrides no-store and private from the origin. Without a matching rule, Cache-Control decides, then the application default TTL.",
      rule: "TTL rule {number}",
      remove: "Remove TTL rule {number}",
      ttl: "Fresh for",
      ttlHint:
        "0 checks the origin on every request and still keeps a complete copy. At most 1 day.",
      empty:
        "No rules: Cache-Control decides freshness; the application default TTL applies when that header is absent.",
      add: "Add TTL rule",
    },
    cleanup: {
      title: "Automatic cleanup rules",
      hint: "Every 15 minutes, files of the current source are checked; the first matching rule decides. Each pass scans up to 1,000 files and removes up to 100 per application.",
      rule: "Cleanup rule {number}",
      remove: "Remove cleanup rule {number}",
      age: "Remove when older than",
      ageHint: "At least 1 minute.",
      empty: "Automatic cleanup is off until you add and save a rule.",
      add: "Add cleanup rule",
    },
    basis: {
      label: "Age is measured from",
      last_access: "Last access",
      fetched_at: "Last download",
      lastAccessHint: "Files still in use are kept. Access times are recorded per minute.",
      fetchedAtHint: "Files can be removed even when they are still used often.",
    },
    ttl: {
      title: "Metadata freshness",
      description:
        "How long channel metadata (for example the latest version) is reused before the upstream is asked again.",
      label: "Channel TTL",
      hint: "Between 1 second and 1 day.",
      unit: "seconds",
      required: "Enter a TTL.",
      range: "Enter a whole number from {min} to {max}.",
      saved: "Channel TTL saved.",
    },
  },
} satisfies LocaleModule;
