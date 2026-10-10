/**
 * happy-dom has no canvas: a stand-in for uPlot that keeps the geometry the
 * chart component asks for (10 px per bucket). Use it in tests that open a
 * metric history: `vi.mock("uplot", () => import("@/test/uplot"));`
 */
export default class UPlotStub {
  cursor = { idx: null as number | null, left: -10, top: -10 };
  over = document.createElement("div");
  constructor(
    public options: unknown,
    public data: number[][],
    host: HTMLElement,
  ) {
    this.over.getBoundingClientRect = () => new DOMRect(0, 0, 1000, 200);
    host.append(this.over);
  }
  destroy() {
    this.over.remove();
  }
  setSize() {}
  valToPos(value: number) {
    return (this.data[0]?.indexOf(value) ?? 0) * 10;
  }
  posToIdx(left: number) {
    return Math.round(left / 10);
  }
  setCursor(position: { left: number; top: number }) {
    this.cursor = { ...position, idx: position.left < 0 ? null : this.posToIdx(position.left) };
  }
}
