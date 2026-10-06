import { readFileSync } from "node:fs";
import { expect, it } from "vitest";

it("centers section disclosure contents, keeps expanded heading spacing, and leaves compact disclosures alone", () => {
  const style = document.createElement("style");
  style.textContent = readFileSync("src/style.css", "utf8");
  document.head.append(style);
  const root = document.createElement("div");
  root.innerHTML = '<div class="section-heading">Regular heading</div><details class="diagnostic-metrics"><summary class="section-heading"><span class="disclosure-icon"></span><h2>Diagnostics</h2></summary><p>Content</p></details><details><summary>Compact error</summary><pre>Error</pre></details>';
  document.body.append(root);
  try {
    const section = root.querySelector("details")!;
    const heading = section.querySelector("summary")!;
    const regular = root.querySelector(".section-heading")!;
    expect(getComputedStyle(heading).alignItems).toBe("center");
    expect(getComputedStyle(heading).marginBottom).toBe("0px");
    for (let cycle = 0; cycle < 3; cycle++) {
      section.open = true;
      expect(getComputedStyle(heading).marginBottom).toBe(getComputedStyle(regular).marginBottom);
      section.open = false;
      expect(getComputedStyle(heading).marginBottom).toBe("0px");
    }
    const compact = root.querySelectorAll("details")[1]!;
    const before = getComputedStyle(compact.querySelector("summary")!).marginBottom;
    compact.open = true;
    expect(getComputedStyle(compact.querySelector("summary")!).marginBottom).toBe(before);
  } finally { root.remove(); style.remove(); }
});
