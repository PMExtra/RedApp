<script setup lang="ts">
import { computed, ref, useId } from "vue";
import { useI18n } from "vue-i18n";
import { useFormat } from "@/shared/i18n";
import { useBucketText } from "./bucket";
import type { HistoryPoint, Metric } from "./catalog";
import type { ChartLine, Timeline } from "./chartData";
import { useMetricFormat } from "./format";

/** The buckets with data as a table, newest first: the chart without the chart. */
const props = defineProps<{
  timeline: Timeline;
  lines: ChartLine[];
  metric: Metric;
  /** Caption: "<metric> over <range>". */
  caption: string;
}>();
const { t } = useI18n();
const format = useFormat();
const metricFormat = useMetricFormat();
const bucket = useBucketText();
const captionId = useId();
const open = ref(false);
const counter = computed(() => props.metric.kind === "counter");
const unit = computed(() => props.metric.unit);
const rows = computed(() => {
  const result: { time: number; point: HistoryPoint }[] = [];
  props.timeline.points.forEach((point, index) => {
    const time = props.timeline.times[index];
    if (point && time !== undefined) result.push({ time, point });
  });
  return result.reverse();
});
</script>

<template>
  <details
    class="rounded-lg border border-border"
    @toggle="open = ($event.target as HTMLDetailsElement).open"
  >
    <summary class="cursor-pointer px-3 py-2 text-sm font-medium focus-ring">
      {{ t("metrics.history.table.toggle", { count: format.number(rows.length) }) }}
    </summary>
    <div
      v-if="open"
      class="max-h-80 overflow-auto border-t border-border"
      tabindex="0"
      :aria-labelledby="captionId"
    >
      <table class="w-full border-collapse text-sm">
        <caption :id="captionId" class="sr-only">
          {{
            caption
          }}
        </caption>
        <thead class="sticky top-0 bg-surface-sunken text-xs text-muted">
          <tr>
            <th scope="col" class="px-3 py-2 text-start font-medium">
              {{ t("metrics.history.table.time") }}
            </th>
            <template v-if="counter">
              <th scope="col" class="px-3 py-2 text-end font-medium">
                {{ t("metrics.history.lines.last") }}
              </th>
              <th scope="col" class="px-3 py-2 text-end font-medium">
                {{ t("metrics.history.lines.delta") }}
              </th>
            </template>
            <template v-else>
              <th scope="col" class="px-3 py-2 text-end font-medium">
                {{ t(lines[0]?.labelKey ?? "metrics.history.lines.sample") }}
              </th>
              <th scope="col" class="px-3 py-2 text-end font-medium">
                {{ t("metrics.history.lines.min") }}
              </th>
              <th scope="col" class="px-3 py-2 text-end font-medium">
                {{ t("metrics.history.lines.max") }}
              </th>
            </template>
            <th scope="col" class="px-3 py-2 text-end font-medium">
              {{ t("metrics.history.table.samples") }}
            </th>
            <th scope="col" class="px-3 py-2 text-start font-medium">
              {{ t("metrics.history.table.coverage") }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.time" class="border-t border-border">
            <th scope="row" class="px-3 py-1.5 text-start font-normal whitespace-nowrap">
              {{ bucket.time(row.time) }}
            </th>
            <template v-if="counter">
              <td class="px-3 py-1.5 text-end tabular-nums">
                {{ metricFormat.value(row.point.value, unit) }}
              </td>
              <td class="px-3 py-1.5 text-end tabular-nums">
                {{ metricFormat.value(row.point.delta, unit) }}
              </td>
            </template>
            <template v-else>
              <td class="px-3 py-1.5 text-end tabular-nums">
                {{ metricFormat.value(row.point.value, unit) }}
              </td>
              <td class="px-3 py-1.5 text-end tabular-nums">
                {{ metricFormat.value(row.point.min, unit) }}
              </td>
              <td class="px-3 py-1.5 text-end tabular-nums">
                {{ metricFormat.value(row.point.max, unit) }}
              </td>
            </template>
            <td class="px-3 py-1.5 text-end tabular-nums">{{ row.point.count }}</td>
            <td class="px-3 py-1.5">{{ bucket.coverage(row.point) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </details>
</template>
