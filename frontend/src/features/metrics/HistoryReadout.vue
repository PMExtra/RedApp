<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { useFormat } from "@/shared/i18n";
import { useBucketText } from "./bucket";
import type { HistoryPoint, Metric } from "./catalog";
import type { ChartLine, CounterView } from "./chartData";
import { useMetricFormat } from "./format";

/**
 * The selected bucket of a history chart. Announced (`live`) when the
 * keyboard or a touch tap selected it, silent while the mouse hovers.
 */
defineProps<{
  id: string;
  live: boolean;
  /** Start of the selected bucket (seconds); `undefined` when nothing is selected. */
  time: number | undefined;
  /** `null`: no sample in the bucket. */
  point: HistoryPoint | null | undefined;
  lines: ChartLine[];
  metric: Metric;
  hourly: boolean;
  view: CounterView;
}>();
const { t } = useI18n();
const format = useFormat();
const metricFormat = useMetricFormat();
const bucket = useBucketText();
</script>

<template>
  <div
    :id="id"
    role="status"
    :aria-label="t('metrics.history.readout')"
    :aria-live="live ? 'polite' : 'off'"
    aria-atomic="true"
    class="min-h-24 rounded-lg border border-border bg-surface-sunken p-3 text-sm"
  >
    <template v-if="time !== undefined">
      <p class="font-medium">
        <time :datetime="new Date(time * 1000).toISOString()">
          {{ bucket.time(time) }}
        </time>
        <span class="text-muted">
          ·
          {{ t(hourly ? "metrics.history.bucket.hour" : "metrics.history.bucket.minute") }}
        </span>
      </p>
      <p v-if="!point" class="mt-1 text-muted">
        {{ t("metrics.history.gap") }}
      </p>
      <template v-else>
        <dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
          <template v-for="line in lines" :key="line.field">
            <dt class="text-muted">{{ t(line.labelKey) }}</dt>
            <dd class="tabular-nums">
              {{ metricFormat.value(point[line.field], metric.unit) }}
              <span
                v-if="
                  metricFormat.exact(point[line.field], metric.unit) !==
                  metricFormat.value(point[line.field], metric.unit)
                "
                class="text-xs text-muted"
              >
                ({{ metricFormat.exact(point[line.field], metric.unit) }})
              </span>
            </dd>
          </template>
        </dl>
        <p class="mt-2 text-xs text-muted">
          {{ t("metrics.history.samples", { count: point.count }) }}
          · {{ bucket.coverage(point) }}
          <template v-if="metric.kind !== 'gauge'">
            ·
            {{
              t("metrics.history.observed", {
                seconds: format.number(point.observed_seconds),
              })
            }}
          </template>
          <template v-if="metric.kind === 'counter' && view === 'delta'">
            ·
            {{ t("metrics.history.intervals", { count: point.delta_count }) }}
          </template>
        </p>
        <p
          v-if="metric.kind === 'counter' && view === 'delta' && point.delta === null"
          class="mt-1 text-xs text-muted"
        >
          {{ t("metrics.history.deltaUnknown") }}
        </p>
      </template>
    </template>
    <p v-else class="text-muted">{{ t("metrics.history.prompt") }}</p>
  </div>
</template>
