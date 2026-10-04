<script setup lang="ts">
import { computed } from "vue";
import { bytes } from "../api";
import { appAPI } from "../bootstrap";
import { usePageStatus } from "../composables/usePageStatus";
import type { AutoCleanupStatus } from "../cachePolicy";
import { errorText, localDate, t } from "../i18n";
const props = defineProps<{ application: string }>();
const {
  status,
  loading: statusLoading,
  error: statusError,
  refresh: refreshStatus,
} = usePageStatus<AutoCleanupStatus>(
  computed(() => `${appAPI(props.application)}/cache/cleanup/status`),
);
</script>
<template>
  <section class="panel auto-cleanup-status">
    <div class="rule-heading">
      <h2>{{ t("Automatic cleanup service") }}</h2>
      <button
        type="button"
        class="secondary"
        :disabled="statusLoading"
        @click="refreshStatus"
      >
        {{ t("Refresh") }}
      </button>
    </div>
    <p class="muted">
      {{ t("Service-wide last pass; includes all configured applications.") }}
    </p>
    <p v-if="statusError" class="error" role="alert">
      {{ errorText(statusError) }}
    </p>
    <p v-if="statusLoading && !status" role="status">{{ t("Loading…") }}</p>
    <template v-if="status">
      <p>{{ status.running ? t("Cleanup running") : t("Cleanup idle") }}</p>
      <dl class="cleanup-service-times">
        <dt>{{ t("Last attempt") }}</dt>
        <dd>{{ localDate(status.last_attempt_at ?? undefined) }}</dd>
        <dt>{{ t("Last successful pass") }}</dt>
        <dd>{{ localDate(status.last_success_at ?? undefined) }}</dd>
        <dt>{{ t("Last failed pass") }}</dt>
        <dd>{{ localDate(status.last_error_at ?? undefined) }}</dd>
      </dl>
      <p>
        {{
          t(
            "{apps} configured applications · {scanned} files scanned · {retired} files retired · {size}",
            {
              apps: status.configured_apps,
              scanned: status.scanned_files,
              retired: status.retired_files,
              size: bytes(status.retired_bytes),
            },
          )
        }}
      </p>
      <p>
        {{
          t(
            "Skipped: {accessed} accessed since preview; {changed} changed generations.",
            {
              accessed: status.skipped_accessed,
              changed: status.skipped_changed,
            },
          )
        }}
      </p>
      <p>
        {{
          t("{passes} total passes · {failures} failed passes", {
            passes: status.passes_total,
            failures: status.failures_total,
          })
        }}
      </p>
      <details v-if="status.last_error">
        <summary>{{ t("Technical details") }}</summary>
        <pre>{{ status.last_error }}</pre>
      </details>
    </template>
  </section>
</template>
