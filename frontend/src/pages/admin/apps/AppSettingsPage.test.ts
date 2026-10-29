import { screen, waitFor, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { recorder, renderAdminPage } from "@/test/directory";
import { app, appConfiguration, category, page, vendor } from "@/test/factories/directory";
import { mockApi, useHandlers } from "@/test/msw";

type Patch = Schema<"AppConfigurationPatch">;

/** A built-in application whose English instructions are overridden. */
function linkedConfiguration(revision: number): Schema<"AppConfiguration"> {
  const base = appConfiguration();
  return appConfiguration({
    revision,
    template_ref: "example/tools",
    template_hash: "b".repeat(64),
    defaults: { ...base.effective, instructions: { en: "Template text", "zh-CN": "模板说明" } },
    effective: { ...base.effective, instructions: { en: "Custom text", "zh-CN": "模板说明" } },
    fields: {
      "instructions.en": { source: "custom", differs_from_template: true },
      "instructions.zh-CN": { source: "inherited", differs_from_template: false },
    },
  });
}

function settingsServer() {
  let revision = 9;
  const patches = recorder<Patch>();
  useHandlers(
    mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
    mockApi("get", "/admin/api/apps/{vendor}/{app}", () =>
      app({ revision, builtin_template: true }),
    ),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () =>
      linkedConfiguration(revision),
    ),
    mockApi("patch", "/admin/api/apps/{vendor}/{app}/configuration", async ({ request }) => {
      await patches.record(request);
      revision += 1;
      return linkedConfiguration(revision);
    }),
    mockApi("get", "/admin/api/categories", () => page([category()], { limit: 100 })),
  );
  return patches;
}

function container(title: string, selector = "section"): HTMLElement {
  const heading = screen.getByRole("heading", { name: title });
  const element = heading.closest<HTMLElement>(selector);
  if (!element) throw new Error(`no section for ${title}`);
  return element;
}

describe("application settings", () => {
  it("saves sections independently against the shared revision", async () => {
    const patches = settingsServer();
    const { user } = await renderAdminPage("/admin/vendors/example/apps/tools/settings");

    // Upstream: only the changed strategy is sent, by the upstream card's own save.
    await user.click(await screen.findByRole("radio", { name: "Round robin" }));
    await user.click(within(container("Upstream")).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(patches.calls).toHaveLength(1);
    });
    expect(patches.calls[0]).toMatchObject({
      ifMatch: '"9"',
      body: { set: { source_strategy: "round_robin" }, unset: [] },
    });

    // Taxonomy: an existing category, a new one (created on save) and a tag.
    const taxonomy = container("Categories and tags");
    const picker = within(taxonomy).getByRole("combobox", { name: "Categories" });
    await user.type(picker, "Agents{Enter}");
    expect(within(taxonomy).getByText("Agents (new)")).toBeInTheDocument();
    await user.type(picker, "develop");
    await user.click(await screen.findByRole("option", { name: "Developer tools" }));
    await user.type(within(taxonomy).getByRole("textbox", { name: "New tag" }), "#cli{Enter}");
    expect(within(taxonomy).getByText("#cli")).toBeInTheDocument();
    await user.click(within(taxonomy).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(patches.calls).toHaveLength(2);
    });
    expect(patches.calls[1]).toMatchObject({
      ifMatch: '"10"',
      body: {
        set: { categories: ["developer-tools"], tags: ["cli"] },
        unset: [],
        new_categories: ["Agents"],
      },
    });

    // Instructions: the override goes back to the template.
    const instructions = container("Usage instructions");
    const english = within(instructions).getByRole("textbox", { name: /Instructions \(English\)/ });
    expect(english).toHaveValue("Custom text");
    expect(
      within(instructions).getAllByRole("button", { name: /Restore the template value/ }),
    ).toHaveLength(1);
    await user.click(
      within(instructions).getByRole("button", {
        name: "Restore the template value: Template text",
      }),
    );
    expect(english).toHaveValue("Template text");
    await user.click(within(instructions).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(patches.calls).toHaveLength(3);
    });
    expect(patches.calls[2]).toMatchObject({
      ifMatch: '"11"',
      body: { set: {}, unset: ["instructions.en"] },
    });
    expect(screen.getByText(/Built-in applications cannot be deleted/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete application" })).toBeDisabled();
  });

  it("marks an icon that includes its background", async () => {
    const patches = settingsServer();
    const { user } = await renderAdminPage("/admin/vendors/example/apps/tools/settings");
    const toggle = await screen.findByRole("switch", { name: "Icon includes background" });
    expect(toggle).not.toBeChecked();
    await user.click(toggle);
    const details = toggle.closest("section");
    if (!details) throw new Error("no details section");
    await user.click(within(details).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(patches.calls).toHaveLength(1);
    });
    expect(patches.calls[0]).toMatchObject({
      body: { set: { icon_has_background: true }, unset: [] },
    });
  });

  it("saves the redirect protection and allowlist of an HTTP cache", async () => {
    const patches = settingsServer();
    const { user } = await renderAdminPage("/admin/vendors/example/apps/tools/settings");
    const network = await waitFor(() => container("Network"));
    const protection = within(network).getByRole("group", { name: "Redirect protection" });
    expect(within(protection).getByRole("radio", { name: "Inherit" })).toBeChecked();
    expect(within(protection).getByText("Effective: Strict (from global setting)")).toBeVisible();

    await user.click(within(protection).getByRole("radio", { name: "Basic" }));
    await user.click(within(network).getByRole("button", { name: "Add redirect target" }));
    const origin = within(network).getByRole("textbox", { name: "Allowed origin 1" });
    await user.type(origin, "https://cdn.example.com/path");
    await user.click(within(network).getByRole("button", { name: "Save" }));
    expect(
      await within(network).findByText(
        "Enter a unique origin such as https://cdn.example.com, without a path.",
      ),
    ).toBeVisible();
    expect(patches.calls).toHaveLength(0);

    await user.clear(origin);
    await user.type(origin, "https://cdn.example.com");
    const prefix = within(network).getByRole("textbox", { name: "Path prefix 1" });
    await user.clear(prefix);
    await user.type(prefix, "/files/");
    await user.click(within(network).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(patches.calls).toHaveLength(1);
    });
    expect(patches.calls[0]).toMatchObject({
      ifMatch: '"9"',
      body: {
        set: {
          redirect_protection: "basic",
          redirect_allowlist: [{ origin: "https://cdn.example.com", prefix: "/files/" }],
        },
        unset: [],
      },
    });
  });

  it.each([
    ["http-cache", "Cache rules", "Metadata freshness"],
    ["codex", "Metadata freshness", "Cache rules"],
  ] as const)("adds the runtime sections of %s applications", async (provider, shown, hidden) => {
    settingsServer();
    useHandlers(mockApi("get", "/admin/api/apps/{vendor}/{app}", () => app({ provider })));
    await renderAdminPage("/admin/vendors/example/apps/tools/settings");
    expect(await screen.findByRole("heading", { name: shown })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: hidden })).toBeNull();
  });
});
