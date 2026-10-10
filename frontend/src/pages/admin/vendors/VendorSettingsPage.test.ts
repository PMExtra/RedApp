import { screen, waitFor, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { recorder, renderAdminPage } from "@/test/directory";
import { linkedVendorConfiguration, vendor, vendorConfiguration } from "@/test/factories/directory";
import { apiError, mockApi, noContent, useHandlers } from "@/test/msw";

const builtin = () =>
  vendor({ id: "openai", name: { en: "OpenAI", "zh-CN": "开放人工智能" }, has_template: true });

describe("vendor settings", () => {
  it("saves only edited and reset fields, and keeps the draft on a conflict", async () => {
    let revision = 3;
    let conflict = true;
    const patches = recorder<Schema<"VendorConfigurationPatch">>();
    useHandlers(
      mockApi("get", "/admin/api/vendors/{vendor}", () => builtin()),
      mockApi("get", "/admin/api/vendors/{vendor}/configuration", () =>
        linkedVendorConfiguration({ revision }),
      ),
      mockApi("patch", "/admin/api/vendors/{vendor}/configuration", async ({ request }) => {
        await patches.record(request);
        if (conflict) {
          conflict = false;
          revision = 4;
          return apiError("REVISION_CONFLICT");
        }
        return linkedVendorConfiguration({ revision: 5 });
      }),
    );
    const { user } = await renderAdminPage("/admin/vendors/openai/settings");
    const english = await screen.findByLabelText(/^Name \(English\)/);
    expect(english).toHaveValue("OpenAI");

    // The overridden Chinese name can follow the template again.
    const chinese = screen.getByRole("group", { name: "简体中文" });
    expect(within(chinese).getByLabelText(/^Name/)).toHaveValue("开放人工智能");
    await user.click(
      within(chinese).getByRole("button", { name: "Restore the template value: OpenAI" }),
    );
    expect(within(chinese).getByLabelText(/^Name/)).toHaveValue("OpenAI");
    // Inherited and unchanged: no reset offered.
    expect(
      within(screen.getByRole("group", { name: "English" })).queryByRole("button", {
        name: /Restore the template value/,
      }),
    ).toBeNull();

    await user.type(english, " Labs");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Changed by someone else")).toBeInTheDocument();
    expect(english).toHaveValue("OpenAI Labs");
    expect(patches.calls[0]).toMatchObject({
      ifMatch: '"3"',
      body: { set: { "name.en": "OpenAI Labs" }, unset: ["name.zh-CN"] },
    });

    await user.click(screen.getByRole("button", { name: "Reload latest version" }));
    await waitFor(() => {
      expect(screen.queryByText("Changed by someone else")).toBeNull();
    });
    expect(english).toHaveValue("OpenAI Labs");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Changes saved.")).toBeInTheDocument();
    expect(patches.calls[1]).toMatchObject({
      ifMatch: '"4"',
      body: { set: { "name.en": "OpenAI Labs" }, unset: ["name.zh-CN"] },
    });
    expect(screen.getByText(/Built-in vendors cannot be deleted/)).toBeInTheDocument();
  });

  it("validates names and a proxy URL before saving", async () => {
    const patches = recorder();
    useHandlers(
      mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
      mockApi("get", "/admin/api/vendors/{vendor}/configuration", () => vendorConfiguration()),
      mockApi("patch", "/admin/api/vendors/{vendor}/configuration", async ({ request }) => {
        await patches.record(request);
        return vendorConfiguration({ revision: 4 });
      }),
    );
    const { user } = await renderAdminPage("/admin/vendors/example/settings");
    const english = await screen.findByLabelText(/^Name \(English\)/);
    await user.clear(english);
    await user.click(screen.getByRole("radio", { name: "Proxy URL" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Enter a name.")).toBeInTheDocument();
    expect(screen.getByText("Enter the proxy URL.")).toBeInTheDocument();
    expect(patches.calls).toHaveLength(0);

    await user.type(english, "Example Corp");
    await user.type(
      screen.getByRole("textbox", { name: /^Proxy URL/ }),
      "http://proxy.internal:3128",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(patches.calls).toHaveLength(1);
    });
    expect(patches.calls[0]?.body).toEqual({
      set: { "name.en": "Example Corp", proxy: { mode: "url", url: "http://proxy.internal:3128" } },
      unset: [],
    });
  });

  it("switches availability at once and deletes a custom vendor after confirmation", async () => {
    const updates = recorder<Schema<"EntityStateUpdate">>();
    const deletes = recorder();
    useHandlers(
      mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
      mockApi("get", "/admin/api/vendors/{vendor}/configuration", () => vendorConfiguration()),
      mockApi("patch", "/admin/api/vendors/{vendor}", async ({ request }) => {
        const body = await updates.record(request);
        return vendor({ enabled: body.enabled, revision: 4 });
      }),
      mockApi("delete", "/admin/api/vendors/{vendor}", async ({ request }) => {
        await deletes.record(request);
        return noContent();
      }),
      mockApi("get", "/admin/api/vendors", () => ({
        items: [],
        page: 1,
        limit: 12,
        total: 0,
        total_pages: 1,
      })),
    );
    const { router, user } = await renderAdminPage("/admin/vendors/example/settings");
    const enabled = await screen.findByRole("switch", { name: "Enabled" });
    expect(enabled).toHaveAttribute("aria-checked", "true");
    await user.click(enabled);
    await waitFor(() => {
      expect(enabled).toHaveAttribute("aria-checked", "false");
    });
    expect(updates.calls[0]).toMatchObject({ ifMatch: '"3"', body: { enabled: false } });
    expect(await screen.findAllByText("Disabled")).not.toHaveLength(0);

    await user.click(screen.getByRole("button", { name: "Delete vendor" }));
    const dialog = await screen.findByRole("alertdialog", { name: "Delete vendor example?" });
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => {
      expect(router.currentRoute.value.name).toBe("admin-vendors");
    });
    expect(deletes.calls[0]?.ifMatch).toBe('"4"');
  });
});
