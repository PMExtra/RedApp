<script setup lang="ts">
import { computed } from "vue";
import { formatMetric, type Metric, type Status } from "../api";
import { label, t } from "../i18n";
const props = defineProps<{
  status: Status;
  application?: string;
  compact?: boolean;
}>();
const emit = defineEmits<{ history: [Metric] }>();
const commonKeys = new Set([
  "disk.used_bytes",
  "disk.free_bytes",
  "disk.allocated_cache_bytes",
  "disk.allocated_temporary_bytes",
  "disk.allocated_pending_bytes",
  "counters.artifact_requests",
  "counters.download_success",
  "counters.download_errors",
  "counters.upstream_errors",
  "counters.upstream_bytes",
  "counters.downstream_bytes",
  "rates.upstream_bytes_per_second",
  "rates.downstream_bytes_per_second",
  "runtime.memory_bytes",
  "resources.readers",
  "resources.active_writers",
]);
const metrics = computed(() =>
  (props.status.metrics || []).filter(
    (metric) =>
      metric.key !== "counters.reuse_requests" &&
      metric.key !== "events.recent_total",
  ),
);
const applicationSummary = new Set([
  "versions.total",
  "disk.cache_bytes",
  "resources.active_writers",
  "counters.download_success",
  "counters.download_errors",
  "rates.downstream_bytes_per_second",
]);
const sections = computed(() =>
  [false, true].map((diagnostic) => {
    const items = metrics.value.filter(
      (metric) =>
        (props.compact ? applicationSummary : commonKeys).has(metric.key) !==
        diagnostic,
    );
    return {
      diagnostic,
      title: diagnostic ? t("Diagnostic metrics") : t("Common metrics"),
      items,
      groups: [...new Set(items.map((metric) => metric.group))],
    };
  }),
);
</script>
<template>
  <div class="overview-stack">
    <template v-for="section in sections" :key="String(section.diagnostic)">
      <slot v-if="section.diagnostic" name="content" />
      <component
        :is="section.diagnostic ? 'details' : 'section'"
        class="panel metric-catalog"
        :class="section.diagnostic ? 'diagnostic-metrics' : 'common-metrics'"
      >
        <component
          :is="section.diagnostic ? 'summary' : 'div'"
          class="section-heading"
        >
          <h2>{{ section.title }}</h2>
          <span class="count-badge">{{
            t("{count} metrics", { count: section.items.length })
          }}</span>
        </component>
        <p class="muted small-text">
          {{
            section.diagnostic
              ? t(
                  "Detailed troubleshooting metrics. Collection and history remain available.",
                )
              : t(
                  "Capacity, downloads and active transfers. Select a value to view history.",
                )
          }}
        </p>
        <section
          v-for="group in section.groups"
          :key="group"
          class="metric-group"
        >
          <h3>{{ label(group) }}</h3>
          <div class="metrics compact-metrics">
            <button
              v-for="metric in section.items.filter(
                (item) => item.group === group,
              )"
              :key="metric.key"
              class="metric-card"
              :data-metric="metric.key"
              @click="emit('history', metric)"
              :aria-label="
                t('View {name} history', { name: label(metric.label) })
              "
            >
              <span>{{ label(metric.label) }}</span>
              <strong>{{ formatMetric(metric.value, metric.unit) }}</strong>
              <small
                >{{
                  metric.kind === "counter"
                    ? t("Cumulative total")
                    : t("Current value")
                }}
                · {{ t("View history") }}</small
              >
              <small v-if="metric.key === 'versions.total'">{{
                t(
                  application
                    ? "Includes only this application."
                    : "Includes all applications; matching version names count separately.",
                )
              }}</small>
            </button>
          </div>
        </section>
      </component>
    </template>
  </div>
</template>
