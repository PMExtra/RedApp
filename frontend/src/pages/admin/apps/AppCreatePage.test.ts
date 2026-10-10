import { screen, waitFor } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { recorder, renderAdminPage } from "@/test/directory";
import { app, appConfiguration, vendor } from "@/test/factories/directory";
import { mockApi, useHandlers } from "@/test/msw";

function createServer() {
  const created = recorder<Schema<"AppCreate">>();
  useHandlers(
    mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
    mockApi("post", "/admin/api/apps", async ({ request }) => {
      const body = await created.record(request);
      return app({ id: body.id, provider: body.provider });
    }),
    mockApi("get", "/admin/api/apps/{vendor}/{app}", () => app({ id: "mirror" })),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => appConfiguration()),
    mockApi("get", "/admin/api/categories", () => ({
      items: [],
      page: 1,
      limit: 100,
      total: 0,
      total_pages: 1,
    })),
  );
  return created;
}

describe("application creation", () => {
  it("fills in the provider defaults", async () => {
    createServer();
    const { user } = await renderAdminPage("/admin/vendors/example/apps/new");
    await user.click(await screen.findByRole("radio", { name: /^Codex/ }));
    expect(await screen.findByLabelText(/^Base URL/)).toHaveValue(
      "https://releases.openai.com/codex",
    );
    await user.click(screen.getByRole("radio", { name: /^Information/ }));
    expect(screen.queryByLabelText(/^Base URL/)).toBeNull();
  });

  it("validates and submits the ordered sources of an HTTP cache", async () => {
    const created = createServer();
    const { router, user } = await renderAdminPage("/admin/vendors/example/apps/new");
    await user.click(await screen.findByRole("radio", { name: /^HTTP cache/ }));
    await user.type(screen.getByLabelText(/^Application ID/), "mirror");
    await user.type(screen.getByLabelText(/^Name \(English\)/), "Mirror");
    await user.type(screen.getByLabelText(/^Name \(简体中文\)/), "镜像");
    expect(screen.getByRole("spinbutton", { name: /^Default freshness/ })).toHaveValue("300");

    await user.type(screen.getByRole("textbox", { name: "Source URL 1" }), "ftp://old");
    await user.click(screen.getByRole("button", { name: "Add source" }));
    await user.type(
      screen.getByRole("textbox", { name: "Source URL 2" }),
      "https://b.example/files",
    );
    await user.click(screen.getByRole("button", { name: "Create application" }));
    expect(
      await screen.findByText("Enter an http or https URL without credentials, query or fragment."),
    ).toBeInTheDocument();
    expect(created.calls).toHaveLength(0);

    const first = screen.getByRole("textbox", { name: "Source URL 1" });
    await user.clear(first);
    await user.type(first, "https://a.example/files");
    // Move the second source to the front with the keyboard handle.
    screen.getByRole("button", { name: "Reorder https://b.example/files" }).focus();
    await user.keyboard("{ArrowUp}");
    await user.click(screen.getByRole("button", { name: "Create application" }));
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe(
        "/admin/vendors/example/apps/mirror/settings",
      );
    });
    expect(created.calls[0]?.body).toEqual({
      vendor: "example",
      id: "mirror",
      provider: "http-cache",
      name: { en: "Mirror", "zh-CN": "镜像" },
      description: { en: "", "zh-CN": "" },
      icon: "",
      enabled: true,
      base_urls: ["https://b.example/files", "https://a.example/files"],
      source_strategy: "ordered",
      cache_ttl_seconds: 300,
    });
  });
});
