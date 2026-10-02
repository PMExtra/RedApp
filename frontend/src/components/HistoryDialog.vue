<script setup lang="ts">
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
  ApiError,
  formatMetric,
  getHistory,
  type HistorySeries,
  type Metric,
} from "../api";
const props = defineProps<{ metric: Metric }>();
const emit = defineEmits<{ close: []; error: [unknown] }>();
const dialog = ref<HTMLDialogElement>(),
  host = ref<HTMLElement>(),
  range = ref("7d"),
  series = ref<HistorySeries>(),
  loading = ref(false),
  error = ref<unknown>(),
  mode = ref("value");
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
const filled = computed(() => points.value.filter((point) => point.count > 0));
const current = computed(() => points.value.at(-1));
function destroy() {
  observer?.disconnect();
  observer = undefined;
  plot?.destroy();
  plot = undefined;
}
function chart() {
  destroy();
  if (disposed || !host.value || !series.value || !hasValues.value) return;
  const data: uPlot.AlignedData = [
    points.value.map((point) => point.time),
    points.value.map((point) =>
      mode.value === "delta" ? point.delta : point.value,
    ),
  ];
  const options: uPlot.Options = {
    width: Math.max(200, host.value.clientWidth),
    height: 280,
    tzDate: (timestamp) => uPlot.tzDate(new Date(timestamp * 1000), "UTC"),
    series: [
      {},
      {
        label:
          mode.value === "delta"
            ? t("Observed increment")
            : props.metric.kind === "counter"
              ? t("Last cumulative value")
              : t("Observed average / value"),
        stroke: "#146b56",
        width: 2,
        spanGaps: false,
        points: { show: true, size: 4 },
        value: (_plot, value) => formatMetric(value, props.metric.unit),
      },
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
    cursor: { show: true },
    select: { show: false, left: 0, top: 0, width: 0, height: 0 },
  };
  if (props.metric.kind !== "counter") {
    data.push(
      points.value.map((point) => point.min),
      points.value.map((point) => point.max),
    );
    options.series.push(
      {
        label: t("Observed minimum"),
        stroke: "#7598b4",
        width: 1,
        spanGaps: false,
      },
      {
        label: t("Observed maximum"),
        stroke: "#a5773e",
        width: 1,
        spanGaps: false,
      },
    );
  }
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
      }}<button class="secondary" @click="load">
        {{ t("Retry history") }}
      </button>
    </div>
    <template v-else-if="series"
      ><p class="muted small-text">
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
      <div
        v-else
        ref="host"
        class="history-chart"
        role="img"
        :aria-label="
          t(
            '{name} over {range}. Values and coverage are available in the table below.',
            { name: label(metric.label), range: windowName(range) },
          )
        "
      ></div>
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
        <summary>
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
