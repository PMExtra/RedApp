<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, shallowRef, useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import type uPlot from "uplot";
import { useFormat } from "@/shared/i18n";
import { usePreferencesStore } from "@/shared/lib";
import { AsyncState, Skeleton } from "@/shared/ui";
import {
  DEFAULT_HISTORY_RANGE,
  HISTORY_RANGES,
  useMetricLabels,
  type HistoryPoint,
  type HistoryRange,
  type Metric,
} from "./catalog";
import {
  alignedData,
  buildTimeline,
  chartLines,
  lastValueIndex,
  summarize,
  type CounterView,
} from "./chartData";
import { useMetricFormat } from "./format";
import { createPlot, indexAt, showCursor } from "./plot";
import { useMetricHistory, type MetricScope } from "./queries";

/**
 * History of one metric (global or per application) for 24 hours, 7 days or
 * 30 days. Missing samples stay gaps. The chart is focusable: Left/Right,
 * Page Up/Down, Home/End move the readout, Escape clears it; touch taps pin
 * it. A summary and a data table give the same information without the chart.
 */
const range = defineModel<HistoryRange>("range", { default: DEFAULT_HISTORY_RANGE });
const props = defineProps<{ metric: Metric; scope: MetricScope }>();

const { t, locale } = useI18n();
const format = useFormat();
const metricFormat = useMetricFormat();
const labels = useMetricLabels();
const preferences = usePreferencesStore();
const ids = { help: useId(), summary: useId(), readout: useId(), table: useId() };

const view = ref<CounterView>("value");
watch(
  () => props.metric.key,
  () => {
    view.value = "value";
  },
);

const history = useMetricHistory(
  () => props.scope,
  () => props.metric.key,
  range,
);
const series = computed(() => history.data.value);
const timeline = computed(() =>
  series.value ? buildTimeline(series.value) : { times: [], points: [] },
);
const lines = computed(() => (series.value ? chartLines(series.value, view.value) : []));
const mainLine = computed(() => lines.value[0]);
const summary = computed(() =>
  mainLine.value ? summarize(timeline.value, mainLine.value) : undefined,
);
const unit = computed(() => props.metric.unit);
const metricName = computed(() => labels.label(props.metric));
const hourly = computed(() => series.value?.resolution_seconds === 3600);
const latestPoint = computed(() => series.value?.points.at(-1));

const selected = ref<number | null>(null);
const input = ref<"pointer" | "keyboard" | "touch">("pointer");
const selectedTime = computed(() =>
  selected.value === null ? undefined : timeline.value.times[selected.value],
);
const selectedPoint = computed<HistoryPoint | null | undefined>(() =>
  selected.value === null ? undefined : timeline.value.points[selected.value],
);

const host = ref<HTMLElement>();
const plot = shallowRef<uPlot>();
let observer: ResizeObserver | undefined;

function destroyPlot() {
  observer?.disconnect();
  observer = undefined;
  plot.value?.destroy();
  plot.value = undefined;
}

function drawPlot() {
  destroyPlot();
  selected.value = null;
  const element = host.value;
  if (!element || !summary.value?.covered) return;
  plot.value = createPlot({
    host: element,
    data: alignedData(timeline.value, lines.value),
    lines: lines.value,
    lineLabels: lines.value.map((line) => t(line.labelKey)),
    formatAxis: (value) => metricFormat.value(value, unit.value),
    onHover: (index) => {
      // A touch tap pins the readout; emulated mouse events must not move it.
      if (input.value === "touch") return;
      input.value = "pointer";
      selected.value = index;
    },
  });
  if (typeof ResizeObserver !== "undefined") {
    observer = new ResizeObserver(() => {
      plot.value?.setSize({ width: Math.max(240, element.clientWidth), height: 260 });
    });
    observer.observe(element);
  }
}

watch(
  [timeline, lines, host, locale, () => preferences.resolvedTheme],
  async () => {
    await nextTick();
    drawPlot();
  },
  { flush: "post" },
);
onBeforeUnmount(destroyPlot);

function select(index: number | null, method: "keyboard" | "touch") {
  input.value = method;
  const last = timeline.value.times.length - 1;
  selected.value = index === null || last < 0 ? null : Math.max(0, Math.min(last, index));
  if (plot.value) showCursor(plot.value, selected.value);
}

function onKeydown(event: KeyboardEvent) {
  const last = timeline.value.times.length - 1;
  const current =
    selected.value ?? lastValueIndex(timeline.value, mainLine.value?.field ?? "value");
  // Page keys jump a day (hourly buckets) or an hour (minute samples).
  const page = hourly.value ? 24 : 60;
  const targets: Record<string, number> = {
    ArrowLeft: current - 1,
    ArrowRight: current + 1,
    PageUp: current - page,
    PageDown: current + page,
    Home: 0,
    End: last,
  };
  if (event.key === "Escape") {
    if (selected.value === null) return;
    event.preventDefault();
    event.stopPropagation();
    select(null, "keyboard");
    return;
  }
  const target = targets[event.key];
  if (target === undefined) return;
  event.preventDefault();
  select(target, "keyboard");
}

function onFocus() {
  if (selected.value === null) {
    select(lastValueIndex(timeline.value, mainLine.value?.field ?? "value"), "keyboard");
  }
}

function onPointerDown(event: PointerEvent) {
  if (event.pointerType === "mouse") {
    input.value = "pointer";
    return;
  }
  if (!plot.value) return;
  const index = indexAt(plot.value, event.clientX, event.clientY);
  if (index !== null) select(index, "touch");
}

function onBlur() {
  if (input.value === "keyboard") select(null, "keyboard");
}

function bucketTime(seconds: number | undefined) {
  if (seconds === undefined) return "—";
  return format.dateTime(new Date(seconds * 1000), { dateStyle: "medium", timeStyle: "short" });
}

function coverage(point: HistoryPoint) {
  if (point.partial) return t("metrics.history.coverage.partial");
  if (point.incomplete) return t("metrics.history.coverage.incomplete");
  return t("metrics.history.coverage.complete");
}

const kindNote = computed(() => t(`metrics.history.notes.${props.metric.kind}`));
const scopeNote = computed(() =>
  props.metric.key === "versions.total"
    ? t(props.scope.kind === "app" ? "metrics.versionsScope.app" : "metrics.versionsScope.global")
    : "",
);

const tableOpen = ref(false);
const tableRows = computed(() => {
  const rows: { time: number; point: HistoryPoint }[] = [];
  timeline.value.points.forEach((point, index) => {
    const time = timeline.value.times[index];
    if (point && time !== undefined) rows.push({ time, point });
  });
  return rows.reverse();
});
const counter = computed(() => props.metric.kind === "counter");
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div
        role="group"
        :aria-label="t('metrics.history.range')"
        class="inline-flex rounded-md border border-border-strong bg-surface p-0.5"
      >
        <button
          v-for="option in HISTORY_RANGES"
          :key="option"
          type="button"
          :aria-pressed="range === option"
          class="rounded-sm px-3 py-1 text-sm font-medium text-muted hover:text-fg focus-ring aria-pressed:bg-primary aria-pressed:text-primary-fg"
          @click="range = option"
        >
          {{ t(`metrics.history.ranges.${option}`) }}
        </button>
      </div>
      <div
        v-if="counter"
        role="group"
        :aria-label="t('metrics.history.view')"
        class="inline-flex rounded-md border border-border-strong bg-surface p-0.5"
      >
        <button
          v-for="option in ['value', 'delta'] as const"
          :key="option"
          type="button"
          :aria-pressed="view === option"
          class="rounded-sm px-3 py-1 text-sm font-medium text-muted hover:text-fg focus-ring aria-pressed:bg-surface-sunken aria-pressed:text-fg"
          @click="view = option"
        >
          {{ t(`metrics.history.views.${option}`) }}
        </button>
      </div>
    </div>

    <p v-if="scopeNote" class="text-sm text-muted">{{ scopeNote }}</p>

    <AsyncState
      :loading="history.isPending.value"
      :error="series ? undefined : history.error.value"
      @retry="history.refetch()"
    >
      <template #loading>
        <Skeleton class="h-[260px] w-full" />
      </template>
      <div v-if="series && mainLine && summary" class="flex flex-col gap-3">
        <p class="text-xs text-muted">
          {{ t(hourly ? "metrics.history.resolution.hour" : "metrics.history.resolution.minute") }}
        </p>
        <p
          v-if="summary.covered === 0"
          role="status"
          class="rounded-lg border border-dashed border-border py-10 text-center text-sm text-muted"
        >
          {{ t("metrics.history.noData") }}
        </p>
        <template v-else>
          <p :id="ids.summary" class="text-sm">
            {{
              t("metrics.history.summary", {
                latest: metricFormat.value(summary.latest, unit),
                min: metricFormat.value(summary.min, unit),
                max: metricFormat.value(summary.max, unit),
                covered: format.number(summary.covered),
                total: format.number(summary.total),
              })
            }}
          </p>
          <ul v-if="lines.length > 1" class="flex flex-wrap gap-4 text-xs text-muted">
            <li v-for="line in lines" :key="line.field" class="inline-flex items-center gap-2">
              <span
                aria-hidden="true"
                class="inline-block w-5"
                :class="
                  line.main ? 'border-t-2 border-primary' : 'border-t border-dashed border-subtle'
                "
              />
              {{ t(line.labelKey) }}
            </li>
          </ul>
          <div
            ref="host"
            role="group"
            tabindex="0"
            :aria-label="
              t('metrics.history.chartLabel', {
                name: metricName,
                range: t(`metrics.history.ranges.${range}`),
              })
            "
            :aria-describedby="`${ids.summary} ${ids.help} ${ids.readout}`"
            class="min-h-[260px] w-full touch-pan-y rounded-md focus-ring"
            @keydown="onKeydown"
            @focus="onFocus"
            @blur="onBlur"
            @pointerdown="onPointerDown"
          />
          <p :id="ids.help" class="text-xs text-muted">{{ t("metrics.history.help") }}</p>
          <div
            :id="ids.readout"
            role="status"
            :aria-label="t('metrics.history.readout')"
            :aria-live="input === 'pointer' ? 'off' : 'polite'"
            aria-atomic="true"
            class="min-h-24 rounded-lg border border-border bg-surface-sunken p-3 text-sm"
          >
            <template v-if="selectedTime !== undefined">
              <p class="font-medium">
                <time :datetime="new Date(selectedTime * 1000).toISOString()">
                  {{ bucketTime(selectedTime) }}
                </time>
                <span class="text-muted">
                  ·
                  {{ t(hourly ? "metrics.history.bucket.hour" : "metrics.history.bucket.minute") }}
                </span>
              </p>
              <p v-if="!selectedPoint" class="mt-1 text-muted">
                {{ t("metrics.history.gap") }}
              </p>
              <template v-else>
                <dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
                  <template v-for="line in lines" :key="line.field">
                    <dt class="text-muted">{{ t(line.labelKey) }}</dt>
                    <dd class="tabular-nums">
                      {{ metricFormat.value(selectedPoint[line.field], unit) }}
                      <span
                        v-if="
                          metricFormat.exact(selectedPoint[line.field], unit) !==
                          metricFormat.value(selectedPoint[line.field], unit)
                        "
                        class="text-xs text-muted"
                      >
                        ({{ metricFormat.exact(selectedPoint[line.field], unit) }})
                      </span>
                    </dd>
                  </template>
                </dl>
                <p class="mt-2 text-xs text-muted">
                  {{ t("metrics.history.samples", { count: selectedPoint.count }) }}
                  · {{ coverage(selectedPoint) }}
                  <template v-if="metric.kind !== 'gauge'">
                    ·
                    {{
                      t("metrics.history.observed", {
                        seconds: format.number(selectedPoint.observed_seconds),
                      })
                    }}
                  </template>
                  <template v-if="counter && view === 'delta'">
                    ·
                    {{ t("metrics.history.intervals", { count: selectedPoint.delta_count }) }}
                  </template>
                </p>
                <p
                  v-if="counter && view === 'delta' && selectedPoint.delta === null"
                  class="mt-1 text-xs text-muted"
                >
                  {{ t("metrics.history.deltaUnknown") }}
                </p>
              </template>
            </template>
            <p v-else class="text-muted">{{ t("metrics.history.prompt") }}</p>
          </div>
        </template>

        <p class="text-xs text-muted">{{ kindNote }}</p>
        <p v-if="latestPoint?.partial" class="text-xs text-muted">
          {{ t("metrics.history.partialNote", { count: latestPoint.count }) }}
        </p>

        <details
          v-if="summary.covered > 0"
          class="rounded-lg border border-border"
          @toggle="tableOpen = ($event.target as HTMLDetailsElement).open"
        >
          <summary class="cursor-pointer px-3 py-2 text-sm font-medium focus-ring">
            {{ t("metrics.history.table.toggle", { count: format.number(tableRows.length) }) }}
          </summary>
          <div
            v-if="tableOpen"
            class="max-h-80 overflow-auto border-t border-border"
            tabindex="0"
            :aria-labelledby="ids.table"
          >
            <table class="w-full border-collapse text-sm">
              <caption :id="ids.table" class="sr-only">
                {{
                  t("metrics.history.table.caption", {
                    name: metricName,
                    range: t(`metrics.history.ranges.${range}`),
                  })
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
                <tr v-for="row in tableRows" :key="row.time" class="border-t border-border">
                  <th scope="row" class="px-3 py-1.5 text-start font-normal whitespace-nowrap">
                    {{ bucketTime(row.time) }}
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
                  <td class="px-3 py-1.5">{{ coverage(row.point) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </details>
      </div>
    </AsyncState>
  </div>
</template>
