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
import HistoryReadout from "./HistoryReadout.vue";
import HistoryTable from "./HistoryTable.vue";
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
const ids = { help: useId(), summary: useId(), readout: useId() };

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
const rangeName = computed(() => t(`metrics.history.ranges.${range.value}`));
const hourly = computed(() => series.value?.resolution_seconds === 3600);
const latestPoint = computed(() => series.value?.points.at(-1));

// The selection is a bucket time, not an index, so it survives background
// refetches that add or drop buckets; it clears when its bucket is gone.
const selectedTime = ref<number | null>(null);
const input = ref<"pointer" | "keyboard" | "touch">("pointer");
const selected = computed(() => {
  if (selectedTime.value === null) return null;
  const index = timeline.value.times.indexOf(selectedTime.value);
  return index < 0 ? null : index;
});
const selectedPoint = computed<HistoryPoint | null | undefined>(() =>
  selected.value === null ? undefined : timeline.value.points[selected.value],
);
watch([() => props.metric.key, range, view], () => {
  selectedTime.value = null;
});

function selectIndex(index: number | null) {
  selectedTime.value = index === null ? null : (timeline.value.times[index] ?? null);
}

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
  // A hover readout follows the mouse, which the new chart has not seen yet.
  if (input.value === "pointer") selectedTime.value = null;
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
      selectIndex(index);
    },
  });
  // A keyboard or touch selection stays visible across redraws.
  if (selected.value !== null) showCursor(plot.value, selected.value);
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
  selectIndex(index === null || last < 0 ? null : Math.max(0, Math.min(last, index)));
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

// A real mouse over the chart takes over from a pinned touch readout (touch
// produces emulated mouse events, but never pointer events of type mouse).
function onPointerMove(event: PointerEvent) {
  if (event.pointerType === "mouse" && input.value === "touch") input.value = "pointer";
}

function onBlur() {
  if (input.value === "keyboard") select(null, "keyboard");
}

const kindNote = computed(() => t(`metrics.history.notes.${props.metric.kind}`));
const scopeNote = computed(() =>
  props.metric.key === "versions.total"
    ? t(props.scope.kind === "app" ? "metrics.versionsScope.app" : "metrics.versionsScope.global")
    : "",
);
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
            :aria-label="t('metrics.history.chartLabel', { name: metricName, range: rangeName })"
            :aria-describedby="`${ids.summary} ${ids.help} ${ids.readout}`"
            class="min-h-[260px] w-full touch-pan-y rounded-md focus-ring"
            @keydown="onKeydown"
            @focus="onFocus"
            @blur="onBlur"
            @pointerdown="onPointerDown"
            @pointermove="onPointerMove"
          />
          <p :id="ids.help" class="text-xs text-muted">{{ t("metrics.history.help") }}</p>
          <HistoryReadout
            :id="ids.readout"
            :live="input !== 'pointer'"
            :time="selected === null ? undefined : timeline.times[selected]"
            :point="selectedPoint"
            :lines="lines"
            :metric="metric"
            :hourly="hourly"
            :view="view"
          />
        </template>

        <p class="text-xs text-muted">{{ kindNote }}</p>
        <p v-if="latestPoint?.partial" class="text-xs text-muted">
          {{ t("metrics.history.partialNote", { count: latestPoint.count }) }}
        </p>

        <HistoryTable
          v-if="summary.covered > 0"
          :timeline="timeline"
          :lines="lines"
          :metric="metric"
          :caption="t('metrics.history.table.caption', { name: metricName, range: rangeName })"
        />
      </div>
    </AsyncState>
  </div>
</template>
