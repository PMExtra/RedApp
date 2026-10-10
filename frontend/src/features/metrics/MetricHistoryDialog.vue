<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Dialog } from "@/shared/ui";
import { DEFAULT_HISTORY_RANGE, useMetricLabels, type HistoryRange, type Metric } from "./catalog";
import { useMetricFormat } from "./format";
import HistoryChart from "./HistoryChart.vue";
import type { MetricScope } from "./queries";

/**
 * The history of a metric in a dialog: `<MetricHistoryDialog v-model:metric="selected" :scope />`.
 * Setting `metric` opens it; closing resets it to `undefined`. The chosen
 * range is kept while the dialog switches between metrics.
 */
const metric = defineModel<Metric | undefined>("metric");
const range = defineModel<HistoryRange>("range", { default: DEFAULT_HISTORY_RANGE });
defineProps<{ scope: MetricScope }>();
const { t } = useI18n();
const labels = useMetricLabels();
const format = useMetricFormat();

const open = computed({
  get: () => metric.value !== undefined,
  set: (value: boolean) => {
    if (!value) metric.value = undefined;
  },
});
const description = computed(() => {
  const current = metric.value;
  if (!current) return undefined;
  const text = labels.description(current.key);
  const value = t("metrics.currentValue", { value: format.value(current.value, current.unit) });
  return text ? `${text} ${value}` : value;
});
</script>

<template>
  <Dialog
    v-model:open="open"
    size="xl"
    :title="metric ? labels.label(metric) : ''"
    :description="description"
  >
    <HistoryChart v-if="metric" v-model:range="range" :metric="metric" :scope="scope" />
  </Dialog>
</template>
