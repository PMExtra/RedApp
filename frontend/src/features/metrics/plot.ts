import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import type { ChartLine } from "./chartData";

export const CHART_HEIGHT = 260;

/** Design token colors; read at draw time so the chart follows the theme. */
function token(name: string): string | undefined {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return value || undefined;
}

/** Indices of values with no neighbour, which a line alone would not show. */
function isolated(values: ArrayLike<number | null | undefined>): number[] {
  const result: number[] = [];
  for (let index = 0; index < values.length; index++) {
    if (values[index] == null) continue;
    if (values[index - 1] == null && values[index + 1] == null) result.push(index);
  }
  return result;
}

export interface PlotOptions {
  host: HTMLElement;
  data: (number | null)[][];
  lines: ChartLine[];
  lineLabels: string[];
  formatAxis: (value: number) => string;
  /** Mouse hover moved to bucket `index` (`null` when the pointer left). */
  onHover: (index: number | null) => void;
}

/**
 * One uPlot instance. Missing buckets are `null` and stay gaps
 * (`spanGaps: false`); the cursor never snaps to a neighbouring bucket.
 */
export function createPlot(options: PlotOptions): uPlot {
  const { host } = options;
  const main = token("--rd-primary");
  const bound = token("--rd-text-subtle");
  const axis = token("--rd-text-muted");
  const grid = token("--rd-border");
  const axisStyle = {
    stroke: axis,
    grid: { stroke: grid, width: 1 },
    ticks: { stroke: grid, width: 1 },
  };
  const plot = new uPlot(
    {
      width: Math.max(240, host.clientWidth),
      height: CHART_HEIGHT,
      legend: { show: false },
      select: { show: false, left: 0, top: 0, width: 0, height: 0 },
      cursor: {
        drag: { x: false, y: false },
        dataIdx: (_self, _series, index) => index,
        points: { size: 8 },
      },
      series: [
        {},
        ...options.lines.map((line, index) => ({
          label: options.lineLabels[index] ?? line.field,
          stroke: line.main ? main : bound,
          width: line.main ? 2 : 1,
          dash: line.main ? undefined : [4, 4],
          spanGaps: false,
          points: {
            show: true,
            size: line.main ? 6 : 4,
            filter: (self: uPlot, seriesIndex: number) => isolated(self.data[seriesIndex] ?? []),
          },
        })),
      ],
      axes: [
        axisStyle,
        {
          ...axisStyle,
          size: 80,
          values: (_self, splits) => splits.map((value) => options.formatAxis(value)),
        },
      ],
      hooks: {
        setCursor: [
          (self) => {
            const { idx, left } = self.cursor;
            options.onHover(idx == null || (left ?? -1) < 0 ? null : idx);
          },
        ],
      },
    },
    options.data as uPlot.AlignedData,
    host,
  );
  return plot;
}

/** Moves the visual cursor to bucket `index` without firing hover hooks. */
export function showCursor(plot: uPlot, index: number | null): void {
  if (index === null) {
    plot.setCursor({ left: -10, top: -10 }, false);
    return;
  }
  const time = plot.data[0][index];
  if (time === undefined) return;
  plot.setCursor({ left: plot.valToPos(time, "x"), top: CHART_HEIGHT / 3 }, false);
}

/** Bucket index under a client x coordinate, or null outside the plot area. */
export function indexAt(plot: uPlot, clientX: number, clientY: number): number | null {
  const rect = plot.over.getBoundingClientRect();
  if (clientX < rect.left || clientX > rect.right || clientY < rect.top || clientY > rect.bottom) {
    return null;
  }
  return plot.posToIdx(clientX - rect.left);
}
