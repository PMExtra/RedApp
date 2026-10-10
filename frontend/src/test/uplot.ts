interface StubOptions {
  hooks?: { setCursor?: ((plot: UPlotStub) => void)[] };
}

/**
 * happy-dom has no canvas: a stand-in for uPlot that keeps the geometry the
 * chart component asks for (10 px per bucket). Like uPlot, a mouse moving
 * over the plot area (`over`) moves the cursor and runs the setCursor hooks.
 * Use it in tests that open a metric history:
 * `vi.mock("uplot", () => import("@/test/uplot"));`
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
    this.over.addEventListener("mousemove", (event) => {
      this.cursor = { left: event.clientX, top: event.clientY, idx: this.posToIdx(event.clientX) };
      for (const hook of (this.options as StubOptions).hooks?.setCursor ?? []) hook(this);
    });
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
