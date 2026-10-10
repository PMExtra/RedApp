import { mount } from "@vue/test-utils";
import { it, expect, vi } from "vitest";
import InstructionsDocument from "./InstructionsDocument.vue";

function post(source: unknown, data: unknown) {
  window.dispatchEvent(
    new MessageEvent("message", { data, source: source as Window }),
  );
}

it("isolates the document in an opaque-origin sandbox and sizes it only from its own messages", () => {
  const remove = vi.spyOn(window, "removeEventListener");
  const wrapper = mount(InstructionsDocument, {
    props: { application: "openai/codex", revision: "1" },
    attachTo: document.body,
  });
  const iframe = wrapper.get("iframe");
  const sandbox = iframe.attributes("sandbox")!.split(" ");
  expect(sandbox.sort()).toEqual(
    [
      "allow-popups",
      "allow-popups-to-escape-sandbox",
      "allow-scripts",
      "allow-top-navigation-by-user-activation",
    ].sort(),
  );
  expect(iframe.attributes("allow")).toBe("clipboard-write");
  expect(iframe.attributes("src")).toBe(
    "/api/apps/openai/codex/instructions/document?lang=en",
  );
  const element = iframe.element as HTMLIFrameElement;
  const child = {};
  Object.defineProperty(element, "contentWindow", { value: child });
  const height = () => element.style.height;
  const type = "redapp-instructions-height";
  post(child, { type, height: 480 });
  expect(height()).toBe("500px");
  // Other windows, other message types and invalid numbers are ignored.
  post(window, { type, height: 900 });
  post({}, { type, height: 900 });
  post(null, { type, height: 900 });
  post(child, { type: "other", height: 900 });
  for (const invalid of ["900", Number.NaN, Number.POSITIVE_INFINITY, null])
    post(child, { type, height: invalid });
  post(child, "900");
  expect(height()).toBe("500px");
  post(child, { type, height: 0 });
  expect(height()).toBe("120px");
  post(child, { type, height: 10_000_000 });
  expect(height()).toBe("50000px");
  wrapper.unmount();
  expect(remove).toHaveBeenCalledWith("message", expect.any(Function));
  post(child, { type, height: 480 });
  expect(height()).toBe("50000px");
  remove.mockRestore();
});
