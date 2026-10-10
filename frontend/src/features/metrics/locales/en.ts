import type { LocaleModule } from "@/shared/i18n";

export default {
  metrics: {
    sampled: "Sampled",
    stale: "Refresh failed. Showing the last successful snapshot.",
    sections: {
      common: "Common metrics",
      commonDescription:
        "Capacity, downloads and active transfers. Select a value to see its history.",
      diagnostic: "Diagnostic metrics",
      diagnosticDescription:
        "Detailed troubleshooting metrics. Their history is recorded like the common ones.",
    },
    count: "{count} metrics",
    viewHistory: "{name}: {value}. View history",
    about: "About {name}",
    currentValue: "Current value: {value}.",
    kinds: {
      gauge: "Current value",
      counter: "Total since the service started",
      rate: "Last 5 seconds",
    },
    versionsScope: {
      global: "All applications; the same version in two applications counts twice.",
      app: "This application only.",
    },
    groups: {
      disk: "Disk",
      traffic: "Requests and traffic",
      speed: "Transfer speed",
      runtime: "Runtime",
      resources: "Resources and versions",
    },
    labels: {
      disk: {
        used_bytes: "Disk used",
        allocated_cache_bytes: "Cached files",
        allocated_temporary_bytes: "Partial downloads",
        allocated_pending_bytes: "Pending deletion",
        other_bytes: "Other data",
        free_bytes: "Free space",
      },
      counters: {
        requests: "Public requests",
        artifact_requests: "File requests",
        cache_hit_requests: "Cache hits",
        shared_follower_requests: "Shared downloads",
        miss_requests: "Cache misses",
        download_success: "Downloads served",
        download_errors: "Downloads failed",
        upstream_errors: "Upstream errors",
        upstream_bytes: "Traffic in",
        downstream_bytes: "Traffic out",
        cleanup_freed_bytes: "Space reclaimed",
      },
      rates: {
        upstream_bytes_per_second: "Speed in",
        downstream_bytes_per_second: "Speed out",
      },
      runtime: {
        memory_bytes: "Memory",
      },
      resources: {
        current: "Current files",
        retired: "Retired files",
        readers: "Active downloads",
        active_writers: "Active fetches",
        failed: "Failed files",
        invalid: "Invalid files",
      },
      versions: {
        total: "Versions",
      },
    },
    descriptions: {
      disk: {
        used_bytes: "Disk space allocated by the data directory.",
        allocated_cache_bytes: "Disk space allocated by complete cached files.",
        allocated_temporary_bytes: "Disk space allocated by downloads still in progress.",
        allocated_pending_bytes: "Disk space allocated by files waiting to be removed.",
        other_bytes: "Everything else in the data directory: database, hosted files and icons.",
        free_bytes: "Free space available on the file system.",
      },
      counters: {
        requests: "Public requests for application files, installers and metadata.",
        artifact_requests: "Requests for downloadable files.",
        cache_hit_requests: "File requests served from a complete cached copy.",
        shared_follower_requests: "File requests that joined a download already in progress.",
        miss_requests: "File requests that started a new upstream download.",
        download_success: "File downloads sent to a client completely.",
        download_errors: "File downloads to a client that failed or were cut off.",
        upstream_errors: "Upstream connection failures and error responses.",
        upstream_bytes: "File bytes received from upstream sources.",
        downstream_bytes: "File bytes sent to clients.",
        cleanup_freed_bytes: "Size of cached files removed by cleanup.",
      },
      rates: {
        upstream_bytes_per_second:
          "Bytes per second received from upstream, over the last 5 seconds.",
        downstream_bytes_per_second: "Bytes per second sent to clients, over the last 5 seconds.",
      },
      runtime: {
        memory_bytes: "Memory currently allocated by the RedApp process.",
      },
      resources: {
        current: "Stored copies that are the current version of their file.",
        retired: "Stored copies replaced or removed but not yet deleted.",
        readers: "Client downloads currently in progress.",
        active_writers: "Files currently being downloaded from upstream.",
        failed: "Stored copies whose download failed.",
        invalid: "Stored copies that failed verification.",
      },
      versions: {
        total: "Release versions seen.",
      },
    },
    history: {
      range: "Time range",
      ranges: {
        "24h": "24 hours",
        "7d": "7 days",
        "30d": "30 days",
      },
      view: "Counter view",
      views: {
        value: "Running total",
        delta: "Increase per bucket",
      },
      resolution: {
        minute: "One sample per minute. Times are in your local time zone; gaps mean no sample.",
        hour: "Hourly summaries. Times are in your local time zone; gaps mean no sample.",
      },
      noData: "No samples in this time range.",
      summary:
        "Latest {latest}, lowest {min}, highest {max}. {covered} of {total} time buckets have data.",
      chartLabel: "Chart of {name} over {range}",
      help: "Hover or tap the chart to see a time bucket. When the chart has focus, use Left and Right, Page Up and Page Down, Home and End; Escape clears the selection.",
      readout: "Selected time bucket",
      prompt: "Select a point in the chart to see its exact values.",
      bucket: {
        minute: "1-minute sample",
        hour: "1-hour bucket",
      },
      gap: "No sample in this time bucket. Missing data is not zero.",
      lines: {
        sample: "Value",
        average: "Average",
        min: "Lowest",
        max: "Highest",
        last: "Running total",
        delta: "Increase",
      },
      samples: "{count} samples",
      observed: "observed for {seconds} s",
      intervals: "{count} valid intervals",
      deltaUnknown:
        "Increase unknown: no two consecutive samples in this bucket (for example after a restart).",
      coverage: {
        partial: "Bucket in progress",
        incomplete: "Fewer samples than expected",
        complete: "Complete",
      },
      notes: {
        gauge: "Hourly values summarize the samples taken; missing minutes are not counted.",
        counter:
          "Running totals are never averaged. Increases count only consecutive samples; restarts and long gaps leave them unknown.",
        rate: "Each sample covers 5 seconds once per minute. The hourly average weights those windows; it is not the transfer rate over the whole hour.",
      },
      partialNote: "The latest bucket is still in progress ({count} samples so far).",
      table: {
        toggle: "Data table ({count} buckets with data)",
        caption: "{name} over {range}, newest first",
        time: "Time bucket",
        samples: "Samples",
        coverage: "Coverage",
      },
    },
  },
} satisfies LocaleModule;
