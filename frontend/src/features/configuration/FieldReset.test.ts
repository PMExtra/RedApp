import { screen } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import { renderWithApp } from "@/test/render";
import FieldReset from "./FieldReset.vue";

describe("provider-scoped template resets", () => {
  it("hides reset for a foreign provider even with unsaved edits", async () => {
    await renderWithApp(FieldReset, {
      props: {
        linked: true,
        modified: true,
        origin: { source: "custom", differs_from_template: null, resettable: false },
      },
    });
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
  it("offers reset for a compatible common field", async () => {
    await renderWithApp(FieldReset, {
      props: {
        linked: true,
        origin: { source: "custom", differs_from_template: true, resettable: true },
        templateValue: "Template name",
      },
    });
    expect(screen.getByRole("button", { name: /Restore the template value/ })).toBeInTheDocument();
  });
});
