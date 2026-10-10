import type { LocaleModule } from "@/shared/i18n";

export default {
  retention: {
    title: "Keep latest versions",
    description:
      "Deletes cached files of old versions. Configuration, version history and download statistics are kept; deleted files are downloaded again on demand.",
    templateMissing: "The template is no longer available; the last accepted defaults stay in use.",
    enabled: "Delete old cached versions automatically",
    enabledHint:
      "Checked on a schedule. Versions a channel points to, versions in use and versions that cannot be compared are always kept. Historical sources are not touched.",
    enableTitle: "Turn on automatic deletion?",
    enableDescription:
      "Cached files of versions beyond the newest ones you keep are deleted on the next scheduled check after you save.",
    enableConfirm: "Turn on",
    keepLatest: "Versions to keep",
    keepLatestHint: "The newest versions with a complete cached file in the current source.",
    keepRange: "Enter a whole number from 1 to 1000.",
    save: "Save retention",
    discard: "Discard changes",
    saved: "Retention policy saved.",
    execute: "Delete selected versions",
    executeTitle: "Delete the selected versions?",
    executeDescription:
      "Cached files of {count} versions ({size}) are deleted. They can be downloaded again later.",
    executed: "Retention run finished.",
    status: {
      title: "Automatic runs",
      never: "No automatic run yet.",
      outcomes: { success: "Succeeded", failure: "Failed", skip: "Skipped" },
      reasons: {
        retention_execution_failed: "Deleting failed",
        policy_source_or_channel_changed: "The policy, source or channels changed during the run",
        channel_unavailable: "A channel could not be verified",
      },
      removed: "{count} versions removed ({size})",
      lastSuccess: "Last success",
      next: "Next check",
      notScheduled: "Automatic checks are not scheduled.",
    },
    run: {
      title: "Run now",
      hint: "Uses the saved policy. Review the selection before anything is deleted; at most 100 versions per run.",
      preview: "Preview run",
      saveFirst: "Save or discard your changes first.",
      review: "Retention preview",
      versions: "Versions to delete",
      logical: "Logical size",
      reclaimable: "Reclaimable disk space",
      items: "Evaluated versions",
      noVersions: "No cached versions to evaluate.",
      delete: "Delete",
      keep: "Keep",
      receipt: "Run finished",
      receiptText:
        "{count} versions removed ({size}). Disk space is reclaimed after current downloads finish.",
      skipped: "{version} kept: {reason}",
    },
    columns: {
      version: "Version",
      decision: "Decision",
      reasons: "Why",
      size: "Size",
    },
    reasons: {
      latest_n: "Among the newest",
      channel: "A channel points to it",
      in_use: "In use",
      uncomparable: "Cannot be compared",
      outside_latest_n: "Older than the newest kept",
      cycle_limit: "Deferred to a later run",
    },
  },
} satisfies LocaleModule;
