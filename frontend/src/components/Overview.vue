<script setup lang="ts">
import { computed } from "vue";
import { formatMetric, type Metric, type Status } from "../api";
import { label, t } from "../i18n";
import Icon from "./Icon.vue";
const props = defineProps<{ status: Status }>();
const emit = defineEmits<{ history: [Metric] }>();
const metrics = computed<Metric[]>(() => props.status.metrics || []);
const groups = computed(() => [
  ...new Set(metrics.value.map((metric) => metric.group)),
]);
const featured = computed(() =>
  [
    "disk.used_bytes",
    "rates.upstream_bytes_per_second",
    "rates.downstream_bytes_per_second",
    "counters.download_success",
  ].flatMap((key) => metrics.value.find((item) => item.key === key) || []),
);
</script>
<template>
  <div class="overview-stack">
    <div class="summary-metrics">
      <button
        v-for="metric in featured"
        :key="metric.key"
        class="metric-card featured"
        :aria-label="t('View {name} history', { name: label(metric.label) })"
        @click="emit('history', metric)"
      >
        <span>{{ label(metric.label) }}</span
        ><strong>{{ formatMetric(metric.value, metric.unit) }}</strong
        ><small>{{ t("View history") }}<Icon name="arrow" :size="14" /></small>
      </button>
    </div>
    <section class="panel metric-catalog">
      <div class="section-heading">
        <div>
          <h2>{{ t("All metrics") }}</h2>
          <p class="muted">
            {{ t("Select a metric to explore its history.") }}
          </p>
        </div>
        <span class="count-badge">{{
          t("{count} metrics", { count: metrics.length })
        }}</span>
      </div>
      <details
        v-for="group in groups"
        :key="group"
        class="metric-group"
        :open="group === 'Disk' || group === 'Speed'"
      >
        <summary>
          <span>{{ label(group) }}</span
          ><span class="muted">{{
            metrics.filter((item) => item.group === group).length
          }}</span>
        </summary>
        <div class="metrics">
          <button
            v-for="metric in metrics.filter((item) => item.group === group)"
            :key="metric.key"
            class="metric-card"
            @click="emit('history', metric)"
            :aria-label="
              t('View {name} history', { name: label(metric.label) })
            "
          >
            <span>{{ label(metric.label) }}</span
            ><strong>{{ formatMetric(metric.value, metric.unit) }}</strong
            ><small
              >{{
                metric.kind === "counter"
                  ? t("Cumulative total")
                  : t("Current value")
              }}
              · {{ t("View history") }}</small
            >
          </button>
        </div>
      </details>
    </section>
  </div>
</template>
