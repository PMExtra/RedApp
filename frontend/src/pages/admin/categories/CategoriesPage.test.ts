import { screen, waitFor, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { recorder, renderAdminPage } from "@/test/directory";
import { category, page } from "@/test/factories/directory";
import { mockApi, useHandlers } from "@/test/msw";

/** A built-in category whose Chinese name is overridden. */
function builtin(revision = 4): Schema<"Category"> {
  return category({
    id: "ai",
    builtin: true,
    revision,
    name: { en: "AI", "zh-CN": "智能" },
    template_ref: "ai",
    defaults: { name: { en: "AI", "zh-CN": "人工智能" } },
    effective: { name: { en: "AI", "zh-CN": "智能" } },
    fields: {
      "name.en": { source: "inherited", differs_from_template: false },
      "name.zh-CN": { source: "custom", differs_from_template: true },
    },
  });
}

describe("categories", () => {
  it("searches and renames a category, restoring a built-in name", async () => {
    const queries: (string | null)[] = [];
    const patches = recorder<Schema<"CategoryPatch">>();
    useHandlers(
      mockApi("get", "/admin/api/categories", ({ request }) => {
        const q = new URL(request.url).searchParams.get("q");
        queries.push(q);
        return page(q ? [builtin()] : [category(), builtin()], { limit: 25 });
      }),
      mockApi("get", "/admin/api/categories/{category}", () => builtin()),
      mockApi("patch", "/admin/api/categories/{category}", async ({ request }) => {
        await patches.record(request);
        return builtin(5);
      }),
    );
    const { router, user } = await renderAdminPage("/admin/categories");
    const table = await screen.findByRole("table", { name: "Categories" });
    expect(await within(table).findByText("Developer tools")).toBeInTheDocument();

    await user.type(screen.getByRole("searchbox", { name: "Search categories" }), "ai");
    await waitFor(() => {
      expect(router.currentRoute.value.query.q).toBe("ai");
    });
    await waitFor(() => {
      expect(within(table).queryByText("Developer tools")).toBeNull();
    });
    expect(queries.at(-1)).toBe("ai");

    await user.click(within(table).getByRole("button", { name: "Rename AI" }));
    const dialog = await screen.findByRole("dialog", { name: "Rename category ai" });
    const english = within(dialog).getByLabelText(/^Name \(English\)/);
    await waitFor(() => {
      expect(english).toHaveValue("AI");
    });
    await user.type(english, " & ML");
    await user.click(
      within(dialog).getByRole("button", { name: "Restore the template value: 人工智能" }),
    );
    expect(within(dialog).getByLabelText(/^Name \(简体中文\)/)).toHaveValue("人工智能");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Rename category ai" })).toBeNull();
    });
    expect(patches.calls[0]).toMatchObject({
      ifMatch: '"4"',
      body: { set: { "name.en": "AI & ML" }, unset: ["name.zh-CN"] },
    });
  });
});
