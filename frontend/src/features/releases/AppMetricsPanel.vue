<script setup lang="ts">
// Minimal stand-in for package D's metric cards and history chart (see the
// package C report for the swap plan). Reads `getAppStatus` directly.
import { computed, ref } from "vue";
import { ChevronRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { AsyncState, Card, RelativeTime } from "@/shared/ui";
import MetricHistoryDialog from "./MetricHistoryDialog.vue";
import { useMetricFormat } from "./metricFormat";
import { useAppStatus, type Metric } from "./queries";

const props = defineProps<{ vendor: string; app: string; autoRefresh: boolean }>();
const { t } = useI18n();
const metricFormat = useMetricFormat();
const interval = computed(() => (props.autoRefresh ? 5_000 : false));
const status = useAppStatus(
  () => props.vendor,
  () => props.app,
  interval,
);

const SUMMARY = [
  "versions.total",
  "resources.complete",
  "resources.active_writers",
  "counters.download_success",
  "counters.download_errors",
  "counters.downstream_bytes",
];
const metrics = computed(() => status.data.value?.metrics ?? []);
const summary = computed(() =>
  SUMMARY.map((key) => metrics.value.find((metric) => metric.key === key)).filter(
    (metric): metric is Metric => metric !== undefined,
  ),
);
const groups = computed(() => {
  const rest = metrics.value.filter((metric) => !SUMMARY.includes(metric.key));
  return [...new Set(rest.map((metric) => metric.group))].map((group) => ({
    group,
    items: rest.filter((metric) => metric.group === group),
  }));
});
const selected = ref<Metric>();
const open = computed({
  get: () => selected.value !== undefined,
  set: (value) => {
    if (!value) selected.value = undefined;
  },
});
</script>

<template>
  <Card :title="t('releases.metrics.title')" :description="t('releases.metrics.description')">
    <template #actions>
      <span v-if="status.data.value" class="text-xs text-muted">
        {{ t("releases.metrics.sampled") }}
        <RelativeTime :value="status.data.value.sampled_at" />
      </span>
    </template>
    <AsyncState
      :loading="status.isPending.value"
      :error="status.data.value ? undefined : status.error.value"
      @retry="status.refetch()"
    >
      <p v-if="status.error.value" class="mb-3 text-sm text-danger" role="alert">
        {{ t("releases.metrics.stale") }}
      </p>
      <ul class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <li v-for="metric in summary" :key="metric.key">
          <button
            type="button"
            class="flex h-full w-full flex-col gap-1 rounded-lg border border-border bg-surface-sunken p-3 text-start hover:bg-surface-hover focus-ring"
            :aria-label="
              t('releases.metrics.viewHistory', {
                name: metricFormat.name(metric),
                value: metricFormat.value(metric.value, metric.unit),
              })
            "
            @click="selected = metric"
          >
            <span class="text-xs text-muted">{{ metricFormat.name(metric) }}</span>
            <strong class="text-lg tabular-nums">
              {{ metricFormat.value(metric.value, metric.unit) }}
            </strong>
          </button>
        </li>
      </ul>
      <details v-if="groups.length" class="group mt-4">
        <summary
          class="flex cursor-pointer items-center gap-1 rounded-sm text-sm font-medium focus-ring"
        >
          <ChevronRight
            class="size-4 transition-transform group-open:rotate-90"
            aria-hidden="true"
          />
          {{ t("releases.metrics.all") }}
        </summary>
        <div class="mt-3 flex flex-col gap-4">
          <section v-for="entry in groups" :key="entry.group">
            <h3 class="mb-2 text-xs font-semibold text-muted uppercase">
              {{ t(`releases.metrics.groups.${entry.group}`) }}
            </h3>
            <ul class="grid grid-cols-1 gap-x-6 sm:grid-cols-2 lg:grid-cols-3">
              <li v-for="metric in entry.items" :key="metric.key">
                <button
                  type="button"
                  class="flex w-full items-center justify-between gap-3 rounded-sm border-b border-border py-1.5 text-start text-sm hover:bg-surface-hover focus-ring"
                  @click="selected = metric"
                >
                  <span>{{ metricFormat.name(metric) }}</span>
                  <span class="tabular-nums">{{
                    metricFormat.value(metric.value, metric.unit)
                  }}</span>
                </button>
              </li>
            </ul>
          </section>
        </div>
      </details>
    </AsyncState>
    <MetricHistoryDialog
      v-if="selected"
      v-model:open="open"
      :vendor="vendor"
      :app="app"
      :metric="selected"
    />
  </Card>
</template>
