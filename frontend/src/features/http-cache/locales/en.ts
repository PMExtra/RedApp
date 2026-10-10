import type { LocaleModule } from "@/shared/i18n";

export default {
  httpCache: {
    sections: {
      label: "Cache sections",
      files: "Cached files",
      maintenance: "Refresh and cleanup",
      prewarm: "Prewarm",
    },
    entries: {
      title: "Cached files",
      description:
        "Freshness decides when a file is checked with the origin again; cleanup removes stored files.",
      empty: "No cached files yet. Files are fetched on demand.",
      hint: "Times are in your local time zone. Last access is recorded per minute.",
      path: "Path",
      size: "Size",
      fetched: "Downloaded",
      accessed: "Last access",
      freshness: "Fresh until",
      actions: "Actions",
      never: "Never",
      fresh: "Fresh",
      stale: "Expired",
      source: "Source",
      validated: "Checked",
      generation: "Stored copy",
      refresh: "Refresh {path}",
      results: {
        refreshed: "{path} refreshed.",
        not_modified: "{path} is unchanged.",
        stale_fallback: "The origin failed; the cached copy of {path} is kept.",
        skipped: "{path} was not updated.",
        failed: "Refreshing {path} failed.",
      },
    },
    refresh: {
      title: "Refresh files",
      description:
        "Check matching cached files with the current origin now, even when they are still fresh.",
      unavailable:
        "Refreshing works only on the current source of an enabled application. Historical sources can still be cleaned up.",
      preview: "Preview refresh",
    },
    cleanup: {
      title: "Clean up files",
      description:
        "Remove matching cached files older than a cutoff from the selected source. Removed files are downloaded again on demand.",
      before: "Older than (local time, {zone})",
      beforeHint: "Files whose time is before this moment are selected.",
      beforeInvalid: "Enter a valid local date and time.",
      utc: "UTC cutoff: {time}",
      lastAccessHint: "Files accessed after the preview are kept when you execute it.",
      preview: "Preview cleanup",
    },
    review: {
      states: {
        building: "Building",
        ready: "Ready",
        running: "Running",
        done: "Done",
        failed: "Failed",
      },
      cutoff: "{basis} before {time} ({utc})",
      source: "source {epoch}",
      scanned: "Files scanned",
      selected: "Files selected",
      size: "Selected size",
      active: "In use now",
      building: "Collecting matching files…",
      progress: "Progress",
      running: "{completed} done, {failed} failed of {total}",
      failed: "The job failed. Create a new preview to try again.",
      files: "Selected files",
      noFiles: "No files on this page.",
      pageHint: "Execution applies to the whole frozen selection, not only this page.",
      columns: { path: "Path", size: "Size", result: "Result" },
      items: {
        pending: "Pending",
        retired: "Removed",
        skipped_accessed: "Kept: accessed since preview",
        skipped_changed: "Kept: replaced since preview",
        refreshed: "Refreshed",
        not_modified: "Not modified",
        stale_fallback: "Cached copy kept",
        failed: "Failed",
        skipped: "Skipped",
      },
      refresh: {
        title: "Refresh preview",
        hint: "Only these files are checked. Leaving the page does not stop a running refresh.",
        execute: "Refresh selected files",
        confirmTitle: "Refresh the selected files?",
        confirmDescription: "{count} files ({size}) are checked with the origin in the background.",
        background: "Refreshing in the background. Leaving this page does not cancel it.",
        done: "Refresh finished",
        result:
          "{refreshed} refreshed · {unchanged} not modified · {stale} kept after an origin failure · {failed} failed · {skipped} skipped",
      },
      cleanup: {
        title: "Cleanup preview",
        hint: "Only these files are removed. Files used meanwhile are kept, and files being downloaded are removed when their transfers finish.",
        execute: "Remove selected files",
        confirmTitle: "Remove the selected files?",
        confirmDescription:
          "{count} cached files ({size}) are removed. They are downloaded again on demand.",
        done: "Cleanup finished",
        result:
          "Removed {retired} of {selected} files ({size}). Kept {accessed} accessed and {changed} replaced files.",
      },
    },
    auto: {
      title: "Automatic cleanup service",
      description:
        "Runs every 15 minutes for all HTTP cache applications with cleanup rules. Counts are for the last pass; totals since the service started.",
      reload: "Reload status",
      running: "Running",
      idle: "Idle",
      never: "Never",
      lastAttempt: "Last pass",
      lastSuccess: "Last successful pass",
      lastError: "Last failed pass",
      lastPass:
        "{apps} applications · {scanned} files scanned · {retired} removed ({size}) · kept {accessed} accessed and {changed} replaced",
      totals: "{passes} passes · {failures} failed",
    },
  },
} satisfies LocaleModule;
