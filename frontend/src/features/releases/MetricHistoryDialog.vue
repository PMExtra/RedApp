<script setup lang="ts">
// Table-only history for the metrics stub; package D's uPlot chart replaces it.
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useFormat } from "@/shared/i18n";
import { AsyncState, Dialog, RadioGroup, type RadioOption } from "@/shared/ui";
import { useMetricFormat } from "./metricFormat";
import { useAppHistory, type HistoryRange, type Metric } from "./queries";

const open = defineModel<boolean>("open", { default: false });
const props = defineProps<{ vendor: string; app: string; metric: Metric }>();
const { t } = useI18n();
const format = useFormat();
const metricFormat = useMetricFormat();
const range = ref<HistoryRange>("24h");
const history = useAppHistory(
  () => props.vendor,
  () => props.app,
  () => props.metric.key,
  range,
);
const ranges = computed<RadioOption[]>(() =>
  (["24h", "7d", "30d"] as const).map((value) => ({
    value,
    label: t(`releases.history.ranges.${value}`),
  })),
);
const rangeModel = computed({
  get: () => range.value,
  set: (value: string | undefined) => {
    if (value === "24h" || value === "7d" || value === "30d") range.value = value;
  },
});
const points = computed(() => [...(history.data.value?.points ?? [])].reverse());
const counter = computed(() => props.metric.kind === "counter");
</script>

<template>
  <Dialog
    v-model:open="open"
    size="lg"
    :title="metricFormat.name(metric)"
    :description="
      t('releases.history.current', { value: metricFormat.value(metric.value, metric.unit) })
    "
  >
    <div class="flex flex-col gap-4">
      <RadioGroup
        v-model="rangeModel"
        :options="ranges"
        orientation="horizontal"
        :aria-label="t('releases.history.range')"
      />
      <AsyncState
        :loading="history.isPending.value"
        :error="history.error.value"
        :empty="points.length === 0"
        :empty-text="t('releases.history.empty')"
        @retry="history.refetch()"
      >
        <p class="mb-2 text-xs text-muted">
          {{
            history.data.value?.resolution_seconds === 60
              ? t("releases.history.minutes")
              : t("releases.history.hours")
          }}
        </p>
        <div class="max-h-80 overflow-y-auto rounded-lg border border-border">
          <table class="w-full text-sm">
            <caption class="sr-only">
              {{
                t("releases.history.caption", { name: metricFormat.name(metric) })
              }}
            </caption>
            <thead class="sticky top-0 bg-surface-sunken text-xs text-muted">
              <tr>
                <th scope="col" class="px-3 py-2 text-start font-medium">
                  {{ t("releases.history.time") }}
                </th>
                <th scope="col" class="px-3 py-2 text-end font-medium">
                  {{ t("releases.history.value") }}
                </th>
                <th v-if="counter" scope="col" class="px-3 py-2 text-end font-medium">
                  {{ t("releases.history.increment") }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="point in points" :key="point.time" class="border-t border-border">
                <td class="px-3 py-1.5">
                  {{ format.dateTime(point.time) }}
                  <span v-if="point.partial" class="text-xs text-muted">
                    · {{ t("releases.history.partial") }}
                  </span>
                </td>
                <td class="px-3 py-1.5 text-end tabular-nums">
                  {{ metricFormat.value(point.value, metric.unit) }}
                </td>
                <td v-if="counter" class="px-3 py-1.5 text-end tabular-nums">
                  {{ metricFormat.value(point.delta, metric.unit) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="mt-2 text-xs text-muted">{{ t("releases.history.gaps") }}</p>
      </AsyncState>
    </div>
  </Dialog>
</template>
