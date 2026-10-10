import { screen, waitFor, within } from "@testing-library/vue";
import { HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schema } from "@/shared/api";
import { recorder, renderAdminPage } from "@/test/directory";
import {
  app,
  appConfiguration,
  importItem,
  importPreview,
  notes,
  page,
  vendor,
  vendorListItem,
} from "@/test/factories/directory";
import { mockApi, useHandlers } from "@/test/msw";

const SCRIPT = '<script>fetch("https://must-not-load.invalid")</script>';

afterEach(() => {
  vi.restoreAllMocks();
});

describe("configuration import", () => {
  it("requires a fresh preview after changed decisions and explicit instruction trust", async () => {
    const choices: unknown[] = [];
    const executed = recorder<Schema<"ImportExecuteRequest">>();
    useHandlers(
      mockApi("get", "/admin/api/vendors", () => page([vendorListItem()], { limit: 12 })),
      mockApi("post", "/admin/api/configuration/import/preview", async ({ request }) => {
        const form = await request.formData();
        const sent = JSON.parse(await (form.get("choices") as Blob).text()) as unknown[];
        choices.push(sent);
        const resolved = sent.length > 0;
        return HttpResponse.json(
          importPreview({
            id: resolved ? "2".repeat(32) : "1".repeat(32),
            ready: resolved,
            needs_instructions_trust: true,
            items: [
              importItem({
                omitted_fields: ["proxy"],
                requirements: resolved ? [] : ["resolve_omitted_proxy"],
                differences: [{ field: "instructions.en", before: "Old", after: SCRIPT }],
              }),
            ],
          }),
        );
      }),
      mockApi(
        "post",
        "/admin/api/configuration/import/{preview_id}/execute",
        async ({ request }) => {
          await executed.record(request);
          return {
            applied: true,
            items: [{ kind: "app" as const, key: "example/tools", revision: 10 }],
          };
        },
        { status: 200 },
      ),
    );
    const { user } = await renderAdminPage("/admin/vendors");
    await user.click(await screen.findByRole("button", { name: "Import configuration" }));
    const dialog = await screen.findByRole("dialog", { name: "Import configuration" });
    const input = dialog.querySelector<HTMLInputElement>('input[type="file"]');
    if (!input) throw new Error("no file input");
    await user.upload(input, new File(["kind: app"], "package.yaml", { type: "application/yaml" }));
    await user.click(within(dialog).getByRole("button", { name: "Preview import" }));

    // Instructions are shown as inert text.
    await user.click(await within(dialog).findByText("instructions.en"));
    expect(within(dialog).getByText(SCRIPT)).toBeInTheDocument();
    expect(dialog.querySelector("script")).toBeNull();
    expect(choices[0]).toEqual([]);
    const execute = within(dialog).getByRole("button", { name: "Execute import" });
    expect(execute).toBeDisabled();
    expect(within(dialog).getByText("Choose a proxy setting.")).toBeInTheDocument();

    await user.click(within(dialog).getByRole("radio", { name: "Direct connection" }));
    expect(within(dialog).getByText("Decisions changed")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Update preview" }));
    await waitFor(() => {
      expect(choices).toHaveLength(2);
    });
    expect(choices[1]).toEqual([{ kind: "app", key: "example/tools", proxy: { mode: "direct" } }]);
    await waitFor(() => {
      expect(within(dialog).queryByText("Decisions changed")).toBeNull();
    });

    // Ready, but the instructions are not trusted yet.
    expect(within(dialog).getByRole("button", { name: "Execute import" })).toBeDisabled();
    await user.click(within(dialog).getByRole("checkbox", { name: /I trust the instructions/ }));
    const ready = within(dialog).getByRole("button", { name: "Execute import" });
    expect(ready).toBeEnabled();
    await user.click(ready);
    expect(await within(dialog).findByText("1 item imported.")).toBeInTheDocument();
    expect(executed.calls[0]?.url.pathname).toBe(
      `/admin/api/configuration/import/${"2".repeat(32)}/execute`,
    );
    expect(executed.calls[0]?.body).toEqual({ trust_instructions: true });
  });
});

function appServer() {
  useHandlers(
    mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
    mockApi("get", "/admin/api/apps/{vendor}/{app}", () => app()),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/admin-notes", () => notes()),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => appConfiguration()),
    mockApi("get", "/admin/api/vendors", () => page([vendorListItem()], { limit: 20 })),
  );
}

describe("configuration export", () => {
  it("leaves sensitive content out unless chosen, every time it opens", async () => {
    appServer();
    const exported = recorder<Schema<"ExportRequest">>();
    useHandlers(
      mockApi("post", "/admin/api/configuration/export", async ({ request }) => {
        await exported.record(request);
        return new HttpResponse(new Blob(["PK"]), {
          headers: {
            "Content-Type": "application/zip",
            "Content-Disposition": 'attachment; filename="redapp-configuration.zip"',
          },
        });
      }),
    );
    const createObjectURL = vi.fn(() => "blob:package");
    Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() });
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);

    const { user } = await renderAdminPage("/admin/vendors/example/apps/tools/admin-notes");
    await user.click(await screen.findByRole("button", { name: "Export configuration" }));
    let dialog = await screen.findByRole("dialog", { name: "Export example/tools" });
    expect(within(dialog).getByRole("checkbox", { name: "Include admin notes" })).not.toBeChecked();
    await user.click(within(dialog).getByRole("checkbox", { name: "Include admin notes" }));
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await user.click(screen.getByRole("button", { name: "Export configuration" }));
    dialog = await screen.findByRole("dialog", { name: "Export example/tools" });
    expect(within(dialog).getByRole("checkbox", { name: "Include admin notes" })).not.toBeChecked();
    await user.click(within(dialog).getByRole("button", { name: "Download ZIP" }));
    await waitFor(() => {
      expect(createObjectURL).toHaveBeenCalledOnce();
    });
    expect(exported.calls[0]?.body).toEqual({
      selection: [{ kind: "app", key: "example/tools" }],
      mode: "linked",
      include_notes: false,
      include_proxy_credentials: false,
    });
  });
});

describe("application copy", () => {
  it("copies with the source revision and opens the new application", async () => {
    appServer();
    const copies = recorder<Schema<"CopyRequest">>();
    useHandlers(
      mockApi("post", "/admin/api/apps/{vendor}/{app}/copy", async ({ request }) => {
        const body = await copies.record(request);
        return app({ id: body.target_id, enabled: false });
      }),
    );
    const { router, user } = await renderAdminPage("/admin/vendors/example/apps/tools/admin-notes");
    await user.click(await screen.findByRole("button", { name: "Copy application" }));
    const dialog = await screen.findByRole("dialog", { name: "Copy example/tools" });
    // Without a template only an independent copy is possible.
    expect(within(dialog).getByRole("radio", { name: /Keep template link/ })).toBeDisabled();
    await user.click(within(dialog).getByRole("button", { name: "Copy application" }));
    expect(
      await within(dialog).findByText("Use lowercase letters, digits and single hyphens."),
    ).toBeInTheDocument();

    await user.type(within(dialog).getByLabelText(/^New application ID/), "tools-canary");
    await user.click(within(dialog).getByRole("checkbox", { name: "Copy admin notes" }));
    await user.click(within(dialog).getByRole("button", { name: "Copy application" }));
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe(
        "/admin/vendors/example/apps/tools-canary/settings",
      );
    });
    expect(copies.calls[0]).toMatchObject({
      ifMatch: '"9"',
      body: {
        source_uid: "ffeeddccbbaa99887766554433221100",
        target_vendor: "example",
        target_id: "tools-canary",
        mode: "independent",
        include_notes: true,
        notes_revision: 2,
      },
    });
  });
});
