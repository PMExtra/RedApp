<script setup lang="ts">
import { computed } from "vue";
import { formatMetric, type Metric, type Status } from "../api";
const props = defineProps<{ status: Status }>();
const emit = defineEmits<{ history: [Metric] }>();
// Keep the previous status interface usable during server/UI version skew.
const metrics = computed<Metric[]>(
  () =>
    props.status.metrics || [
      ...Object.entries(props.status.disk).map(([key, value]) => ({
        key: "disk." + key,
        label: key.replaceAll("_", " "),
        kind: "gauge" as const,
        unit: "bytes" as const,
        group: "Disk",
        value,
        observed_seconds: 0,
      })),
      ...Object.entries(props.status.counters)
        .filter(([key]) => !key.includes(":"))
        .map(([key, value]) => ({
          key: "counters." + key,
          label: key.replaceAll("_", " "),
          kind: "counter" as const,
          unit: (key.includes("bytes") ? "bytes" : "count") as Metric["unit"],
          group: "Traffic and requests",
          value,
          observed_seconds: 0,
        })),
    ],
);
const groups = computed(() => [
  ...new Set(metrics.value.map((metric) => metric.group)),
]);
</script>
<template>
  <section>
    <div class="section-heading">
      <h2>Overview</h2>
      <span class="muted">Snapshot {{ status.sampled_at }}</span>
    </div>
    <p class="muted">
      Select any current metric to view its history. Versions and individual
      resources retain current details only.
    </p>
    <div v-for="group in groups" :key="group" class="metric-group">
      <h3>{{ group }}</h3>
      <div class="metrics">
        <button
          v-for="metric in metrics.filter((item) => item.group === group)"
          :key="metric.key"
          type="button"
          class="metric-card"
          @click="emit('history', metric)"
          :aria-label="`View ${metric.label} history`"
        >
          <span>{{ metric.label }}</span
          ><strong>{{ formatMetric(metric.value, metric.unit) }}</strong
          ><small
            >{{
              metric.kind === "counter" ? "Cumulative total" : "Current value"
            }}
            · View history</small
          >
        </button>
      </div>
    </div>
  </section>
</template>
