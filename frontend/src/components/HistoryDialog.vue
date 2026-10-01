<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  onUnmounted,
  ref,
} from "vue";
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
  error = ref(""),
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
  if (!host.value || !series.value || !hasValues.value) return;
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
            ? "Observed increment"
            : props.metric.kind === "counter"
              ? "Last cumulative value"
              : "Observed average / value",
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
        label: "Observed minimum",
        stroke: "#7598b4",
        width: 1,
        spanGaps: false,
      },
      {
        label: "Observed maximum",
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
  error.value = "";
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
    error.value =
      reason instanceof Error ? reason.message : "History request failed";
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
onMounted(() => {
  dialog.value?.showModal();
  void load();
});
onBeforeUnmount(() => {
  dialog.value?.close();
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
    <div class="section-heading">
      <div>
        <span class="eyebrow">Metric history · UTC</span>
        <h2 id="history-title">{{ metric.label }}</h2>
      </div>
      <button class="secondary" type="button" @click="emit('close')" autofocus>
        Close history
      </button>
    </div>
    <p>
      Current value:
      <strong>{{ formatMetric(metric.value, metric.unit) }}</strong>
    </p>
    <div class="history-controls">
      <div role="group" aria-label="History window">
        <button
          v-for="option in ['24h', '7d', '30d']"
          :key="option"
          :aria-pressed="range === option"
          @click="
            range = option;
            load();
          "
        >
          {{
            option === "24h"
              ? "24 hours"
              : option === "7d"
                ? "7 days"
                : "30 days"
          }}
        </button>
      </div>
      <label v-if="metric.kind === 'counter'"
        >Counter view<select v-model="mode" @change="selectMode">
          <option value="value">Cumulative last value</option>
          <option value="delta">Observed increment</option>
        </select></label
      >
    </div>
    <p v-if="loading" role="status">Loading history…</p>
    <div v-else-if="error" class="error" role="alert">
      {{ error }} <button @click="load">Retry history</button>
    </div>
    <template v-else-if="series"
      ><p class="muted">
        {{
          series.resolution_seconds === 60
            ? "Minute observations · last 24 hours"
            : "Hourly aggregates"
        }}
        · UTC buckets. Missing observations remain gaps.
      </p>
      <p v-if="!hasValues" class="empty" role="status">
        No observations available for this window.
      </p>
      <div
        v-else
        ref="host"
        class="history-chart"
        role="img"
        :aria-label="`${metric.label} over ${range}. Values and coverage are also available in the table below.`"
      ></div>
      <p v-if="metric.kind === 'counter'" class="muted">
        Cumulative counters are never averaged. Increments cover only adjacent
        valid samples; restart, reset and long gaps have unknown increments.
      </p>
      <p v-else-if="metric.kind === 'rate'" class="muted">
        Each observation covers five seconds, sampled once per minute. Hourly
        average weights those observed windows; it is not the whole-hour
        transfer rate.
      </p>
      <p v-else class="muted">
        Green: observed average/value. Blue: minimum. Brown: maximum. Hourly
        values summarize available samples, not missing intervals.
      </p>
      <p v-if="current?.partial" class="muted">
        Current {{ series.resolution_seconds === 60 ? "minute" : "hour" }} is
        partial: {{ current.count }} observations<span
          v-if="metric.kind === 'rate'"
        >
          / {{ current.observed_seconds }} seconds observed</span
        >.
      </p>
      <details>
        <summary>
          Observation values and coverage ({{ filled.length }} buckets)
        </summary>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>UTC bucket</th>
                <th>Value / last</th>
                <th>Min / max / average</th>
                <th>Samples</th>
                <th>Increment / valid intervals</th>
                <th>Observed seconds / coverage</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="point in filled" :key="point.time">
                <td>{{ new Date(point.time * 1000).toISOString() }}</td>
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
                      ? "Partial current bucket"
                      : point.incomplete
                        ? "Incomplete observations"
                        : "Complete observations"
                  }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </details></template
    >
  </dialog>
</template>
