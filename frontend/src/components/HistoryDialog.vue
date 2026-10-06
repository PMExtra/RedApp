<script setup lang="ts">
import IconButton from "./IconButton.vue";
import DisclosureIcon from "./DisclosureIcon.vue";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  onUnmounted,
  ref,
  watch,
} from "vue";
import { errorText, label, language, t, utcDate } from "../i18n";
import Icon from "./Icon.vue";
import SelectMenu from "./SelectMenu.vue";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import {
  exactMetric,
  historyData,
  historyLines,
  localBucketTime,
} from "../historyChart";
import {
  ApiError,
  formatMetric,
  getHistory,
  type HistorySeries,
  type Metric,
} from "../api";
const props = defineProps<{ metric: Metric; application?: string }>();
const emit = defineEmits<{ close: []; error: [unknown] }>();
const dialog = ref<HTMLDialogElement>(),
  host = ref<HTMLElement>(),
  range = ref("7d"),
  series = ref<HistorySeries>(),
  loading = ref(false),
  error = ref<unknown>(),
  mode = ref("value");
const selectedIndex = ref<number | null>(null);
const inputMethod = ref<"pointer" | "keyboard" | "touch">("pointer");
let controller: AbortController | undefined,
  plot: uPlot | undefined,
  observer: ResizeObserver | undefined,
  disposed = false,
  sequence = 0;
const points = computed(() => series.value?.points || []);
const hasValues = computed(() =>
  points.value.some(
    (point) => (mode.value === "delta" ? point.delta : point.value) !== null,
  ),
);
const filled = computed(() =>
  points.value.filter((point) => point.count > 0),
);
const current = computed(() => points.value.at(-1));
const lines = computed(() =>
  series.value ? historyLines(series.value, mode.value) : [],
);
const selectedPoint = computed(() =>
  selectedIndex.value === null ? undefined : points.value[selectedIndex.value],
);
function clearPoint() {
  selectedIndex.value = null;
}
function selectPoint(index: number, method: "keyboard" | "touch") {
  if (!plot || !points.value.length) return;
  inputMethod.value = method;
  selectedIndex.value = Math.max(0, Math.min(points.value.length - 1, index));
  plot.setCursor(
    {
      left: plot.valToPos(points.value[selectedIndex.value]!.time, "x"),
      top: 0,
    },
    false,
  );
}
function keyboardPoint(event: KeyboardEvent) {
  if (event.key === "Escape" && selectedIndex.value !== null) {
    event.preventDefault();
    event.stopPropagation();
    clearPoint();
    plot?.setCursor({ left: -10, top: -10 }, false);
    return;
  }
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  event.stopPropagation();
  const last = points.value.length - 1;
  const index =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? last
        : (selectedIndex.value ?? last) + (event.key === "ArrowLeft" ? -1 : 1);
  selectPoint(index, "keyboard");
}
function touchPoint(event: PointerEvent) {
  if (!plot || (event.pointerType !== "touch" && event.pointerType !== "pen"))
    return;
  const rect = plot.over.getBoundingClientRect();
  if (
    event.clientX < rect.left ||
    event.clientX > rect.right ||
    event.clientY < rect.top ||
    event.clientY > rect.bottom
  )
    return;
  host.value?.focus({ preventScroll: true });
  selectPoint(plot.posToIdx(event.clientX - rect.left), "touch");
}
function pointerInput(event: PointerEvent) {
  if (event.pointerType === "mouse") inputMethod.value = "pointer";
}
function destroy() {
  clearPoint();
  observer?.disconnect();
  observer = undefined;
  plot?.destroy();
  plot = undefined;
}
function chart() {
  destroy();
  if (disposed || !host.value || !series.value || !hasValues.value) return;
  const data = historyData(points.value, lines.value) as uPlot.AlignedData;
  const options: uPlot.Options = {
    width: Math.max(200, host.value.clientWidth),
    height: 280,
    tzDate: (timestamp) => uPlot.tzDate(new Date(timestamp * 1000), "UTC"),
    series: [
      {},
      ...lines.value.map((line, index) => ({
        label: t(line.label),
        stroke: line.color,
        width: index === 0 ? 2 : 1,
        spanGaps: false,
        ...(index === 0 ? { points: { show: true, size: 4 } } : {}),
      })),
    ],
    axes: [
      {},
      {
        size: 95,
        values: (_plot, values) =>
          values.map((value) => formatMetric(value, props.metric.unit)),
      },
    ],
    legend: { show: false },
    cursor: {
      show: true,
      drag: { x: false, y: false },
      // Do not snap null values to an adjacent observed bucket.
      dataIdx: (_plot, _seriesIndex, index) => index,
    },
    hooks: {
      setCursor: [
        (chart) => {
          // Touch taps are pinned until another input. Compatibility mouse
          // events emitted after a tap must not replace or clear the readout.
          if (inputMethod.value === "touch") return;
          if (
            chart.cursor.idx == null ||
            (chart.cursor.left ?? -1) < 0 ||
            (chart.cursor.top ?? -1) < 0
          ) {
            if (inputMethod.value === "pointer") clearPoint();
            return;
          }
          inputMethod.value = "pointer";
          selectedIndex.value = chart.cursor.idx;
        },
      ],
    },
    select: { show: false, left: 0, top: 0, width: 0, height: 0 },
  };
  plot = new uPlot(options, data, host.value);
  observer = new ResizeObserver(() => {
    if (host.value)
      plot?.setSize({
        width: Math.max(200, host.value.clientWidth),
        height: 280,
      });
  });
  observer.observe(host.value);
}
async function load() {
  const ticket = ++sequence;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = undefined;
  series.value = undefined;
  destroy();
  try {
    const data = await getHistory(
      props.metric.key,
      range.value,
      controller.signal,
      props.application,
    );
    if (disposed || ticket !== sequence) return;
    series.value = data;
    loading.value = false;
    await nextTick();
    chart();
  } catch (reason) {
    if (
      disposed ||
      ticket !== sequence ||
      (reason instanceof Error && reason.name === "AbortError")
    )
      return;
    error.value = reason;
    if (reason instanceof ApiError && reason.status === 401)
      emit("error", reason);
  } finally {
    if (ticket === sequence) loading.value = false;
  }
}
async function selectMode() {
  await nextTick();
  chart();
}
const previous = document.activeElement as HTMLElement | null;
watch(language, async () => {
  await nextTick();
  chart();
});
watch(
  () => [props.metric.key, props.application],
  () => {
    mode.value = "value";
    void load();
  },
);
function windowName(value: string) {
  return value === "24h"
    ? t("24 hours")
    : value === "7d"
      ? t("7 days")
      : t("30 days");
}
onMounted(() => {
  dialog.value?.showModal();
  void load();
});
onBeforeUnmount(() => {
  dialog.value?.close();
  previous?.focus();
});
onUnmounted(() => {
  disposed = true;
  controller?.abort();
  destroy();
});
</script>
<template>
  <dialog
    ref="dialog"
    class="history-dialog"
    aria-labelledby="history-title"
    @cancel.prevent="emit('close')"
  >
    <div class="dialog-heading">
      <div>
        <span class="eyebrow">{{ t("Metric history · UTC") }}</span>
        <h2 id="history-title">{{ label(metric.label) }}</h2>
      </div>
      <button
        class="icon-button secondary"
        type="button"
        @click="emit('close')"
        :aria-label="t('Close history')"
        autofocus
      >
        <Icon name="close" />
      </button>
    </div>
    <p class="history-current">
      {{ t("Current value") }}
      <strong>{{ formatMetric(metric.value, metric.unit) }}</strong>
    </p>
    <div class="history-controls">
      <div class="segmented" role="group" :aria-label="t('History window')">
        <button
          v-for="option in ['24h', '7d', '30d']"
          :key="option"
          :aria-pressed="range === option"
          @click="
            range = option;
            load();
          "
        >
          {{ windowName(option) }}
        </button>
      </div>
      <div v-if="metric.kind === 'counter'" class="select-field">
        <span>{{ t("Counter view") }}</span>
        <SelectMenu
          v-model="mode"
          @update:model-value="selectMode"
          :label="t('Counter view')"
          :options="[
            { value: 'value', label: t('Cumulative last value') },
            { value: 'delta', label: t('Observed increment') },
          ]"
        />
      </div>
    </div>
    <p v-if="loading" class="empty" role="status">
      {{ t("Loading history…") }}
    </p>
    <div v-else-if="error" class="error" role="alert">
      {{ errorText(error)
      }}<IconButton
        class="secondary"
        @click="load"
        icon="refresh"
        :label="t('Retry history')"
      />
    </div>
    <template v-else-if="series"
      ><p
        v-if="metric.key === 'versions.total'"
        class="notice small-text version-scope-note"
      >
        {{
          application
            ? t("Includes only this application.")
            : t(
                "Includes all applications; matching version names count separately.",
              )
        }}
      </p>
      <p class="muted small-text">
        {{
          series.resolution_seconds === 60
            ? t("Minute observations · last 24 hours")
            : t("Hourly aggregates")
        }}
        · {{ t("UTC buckets. Missing observations remain gaps.") }}
      </p>
      <p v-if="!hasValues" class="empty" role="status">
        {{ t("No observations available for this window.") }}
      </p>
      <div v-else class="history-explorer">
        <p id="history-point-help" class="muted small-text">
          {{
            t(
              "Hover or tap to inspect a bucket. Focus the chart and use Left/Right, Home/End; Escape clears the selection.",
            )
          }}
        </p>
        <div
          ref="host"
          class="history-chart"
          role="group"
          tabindex="0"
          aria-describedby="history-point-help history-point-values"
          @keydown="keyboardPoint"
          @focus="selectPoint(points.length - 1, 'keyboard')"
          @blur="clearPoint"
          @pointermove.capture="pointerInput"
          @pointerdown.capture="pointerInput"
          @pointerdown="touchPoint"
          :aria-label="
            t(
              '{name} over {range}. Values and coverage are available in the table below.',
              { name: label(metric.label), range: windowName(range) },
            )
          "
        ></div>
        <div
          id="history-point-values"
          class="history-readout"
          role="status"
          :aria-live="inputMethod === 'pointer' ? 'off' : 'polite'"
          aria-atomic="true"
        >
          <template v-if="selectedPoint">
            <p class="small-text">
              <strong>{{ t("Bucket start · browser local time") }}</strong
              ><br />
              <time
                :datetime="new Date(selectedPoint.time * 1000).toISOString()"
                >{{ localBucketTime(selectedPoint.time) }}</time
              >
              ·
              {{
                series.resolution_seconds === 60
                  ? t("Minute observation")
                  : t("Hourly aggregate")
              }}
            </p>
            <dl class="history-point-values">
              <div
                v-for="(line, index) in lines"
                :key="line.field"
                :class="'history-line-' + index"
              >
                <dt>{{ t(line.label) }}</dt>
                <dd>
                  {{ exactMetric(selectedPoint[line.field], metric.unit) }}
                </dd>
              </div>
            </dl>
            <p class="muted small-text">
              {{ t("Samples") }}: {{ selectedPoint.count }} ·
              {{
                selectedPoint.partial
                  ? t("Partial current bucket")
                  : selectedPoint.incomplete
                    ? t("Incomplete observations")
                    : t("Complete observations")
              }}
              <template v-if="metric.kind !== 'gauge'">
                ·
                {{
                  t("Observed duration: {seconds} seconds.", {
                    seconds: selectedPoint.observed_seconds,
                  })
                }}</template
              >
              <template v-if="metric.kind === 'counter' && mode === 'delta'">
                ·
                {{
                  t("Valid intervals: {count}", {
                    count: selectedPoint.delta_count,
                  })
                }}</template
              >
            </p>
            <p v-if="selectedPoint.count === 0" class="muted small-text">
              {{
                t(
                  "No observation in this bucket. Missing values are not zero.",
                )
              }}
            </p>
            <p
              v-else-if="mode === 'delta' && selectedPoint.delta === null"
              class="muted small-text"
            >
              {{
                t(
                  "Increment unknown: no valid adjacent observation interval.",
                )
              }}
            </p>
          </template>
          <p v-else class="muted small-text">
            {{ t("Select a chart point to see exact values.") }}
          </p>
        </div>
      </div>
      <p class="muted small-text">
        {{
          metric.kind === "counter"
            ? t(
                "Cumulative counters are never averaged. Increments cover only adjacent valid samples; restart, reset and long gaps have unknown increments.",
              )
            : metric.kind === "rate"
              ? t(
                  "Each observation covers five seconds, sampled once per minute. Hourly average weights those observed windows; it is not the whole-hour transfer rate.",
                )
              : t(
                  "Green: observed average/value. Blue: minimum. Brown: maximum. Hourly values summarize available samples, not missing intervals.",
                )
        }}
      </p>
      <p v-if="current?.partial" class="notice small-text">
        {{
          t("Current bucket is partial: {count} observations.", {
            count: current.count,
            seconds: current.observed_seconds,
          })
        }}
        <span v-if="metric.kind === 'rate'">
          {{
            t("Observed duration: {seconds} seconds.", {
              seconds: current.observed_seconds,
            })
          }}</span
        >
      </p>
      <details class="history-table">
        <summary class="section-heading">
          <DisclosureIcon />
          {{
            t("Observation values and coverage ({count} buckets)", {
              count: filled.length,
            })
          }}
        </summary>
        <div
          class="table-wrap"
          tabindex="0"
          :aria-label="
            t('Observation values and coverage ({count} buckets)', {
              count: filled.length,
            })
          "
        >
          <table>
            <thead>
              <tr>
                <th>{{ t("UTC bucket") }}</th>
                <th>{{ t("Value / last") }}</th>
                <th>{{ t("Min / max / average") }}</th>
                <th>{{ t("Samples") }}</th>
                <th>{{ t("Increment / valid intervals") }}</th>
                <th>{{ t("Observed seconds / coverage") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="point in filled" :key="point.time">
                <td>{{ utcDate(point.time) }}</td>
                <td>
                  {{ formatMetric(point.value, metric.unit) }} /
                  {{ formatMetric(point.last, metric.unit) }}
                </td>
                <td>
                  {{ formatMetric(point.min, metric.unit) }} /
                  {{ formatMetric(point.max, metric.unit) }} /
                  {{ formatMetric(point.avg, metric.unit) }}
                </td>
                <td>{{ point.count }}</td>
                <td>
                  {{ formatMetric(point.delta, metric.unit) }} /
                  {{ point.delta_count }}
                </td>
                <td>
                  {{ point.observed_seconds }} s ·
                  {{
                    point.partial
                      ? t("Partial current bucket")
                      : point.incomplete
                        ? t("Incomplete observations")
                        : t("Complete observations")
                  }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </details>
    </template>
  </dialog>
</template>
