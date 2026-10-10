import { screen } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import { usePreferencesStore } from "@/shared/lib";
import { renderWithApp } from "@/test/render";
import InstructionsDocument from "./InstructionsDocument.vue";

function post(source: unknown, data: unknown) {
  window.dispatchEvent(new MessageEvent("message", { data, source: source as Window }));
}

async function renderFrame() {
  const rendered = await renderWithApp(InstructionsDocument, {
    props: { vendor: "openai", app: "codex", revision: "1" },
  });
  const frame = screen.getByTitle<HTMLIFrameElement>("Usage instructions");
  // happy-dom does not load the document; stand in for its window.
  const child = {};
  Object.defineProperty(frame, "contentWindow", { value: child, configurable: true });
  return { ...rendered, frame, child };
}

describe("InstructionsDocument", () => {
  it("isolates the document in an opaque-origin sandbox", async () => {
    const { frame } = await renderFrame();
    expect(frame.getAttribute("sandbox")?.split(" ").sort()).toEqual([
      "allow-popups",
      "allow-popups-to-escape-sandbox",
      "allow-scripts",
      "allow-top-navigation-by-user-activation",
    ]);
    expect(frame.getAttribute("sandbox")).not.toContain("allow-same-origin");
    expect(frame).toHaveAttribute("allow", "clipboard-write");
    expect(frame).toHaveAttribute("src", "/api/apps/openai/codex/instructions/document?lang=en");
  });

  it("sizes itself only from numeric height messages of its own window", async () => {
    const { frame, child } = await renderFrame();
    const type = "redapp-instructions-height";
    const height = () => frame.style.height;

    post(child, { type, height: 480 });
    expect(height()).toBe("500px");

    // Other windows, other message types and invalid heights are ignored.
    post(window, { type, height: 900 });
    post({}, { type, height: 900 });
    post(null, { type, height: 900 });
    post(child, { type: "other", height: 900 });
    for (const invalid of ["900", Number.NaN, Number.POSITIVE_INFINITY, null]) {
      post(child, { type, height: invalid });
    }
    post(child, "900");
    expect(height()).toBe("500px");

    post(child, { type, height: 0 });
    expect(height()).toBe("120px");
    post(child, { type, height: 10_000_000 });
    expect(height()).toBe("50000px");
  });

  it("stops listening when unmounted", async () => {
    const rendered = await renderFrame();
    post(rendered.child, { type: "redapp-instructions-height", height: 480 });
    rendered.unmount();
    post(rendered.child, { type: "redapp-instructions-height", height: 1000 });
    expect(rendered.frame.style.height).toBe("500px");
  });

  it("follows the UI language and re-creates the frame for a new language", async () => {
    const { frame } = await renderFrame();
    usePreferencesStore().setLocale("zh-CN");
    const next = await screen.findByTitle<HTMLIFrameElement>("使用说明");
    expect(next).toHaveAttribute("src", "/api/apps/openai/codex/instructions/document?lang=zh-CN");
    expect(next).not.toBe(frame);
  });
});
