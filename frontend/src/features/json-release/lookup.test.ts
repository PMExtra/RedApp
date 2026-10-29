import { screen } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { ReleaseLookupForm } from "@/features/json-release";
import type { Schema } from "@/shared/api";
import { renderAppPage } from "@/test/appPage";
import { codexConfiguration } from "@/test/factories/runtime";
import { mockApi, useHandlers } from "@/test/msw";

function configuration(lookup: boolean) {
  const spec: Schema<"AppSpec"> = {
    name: { en: "Editor", "zh-CN": "编辑器" },
    description: { en: "", "zh-CN": "" },
    icon: "",
    provider: "json-release",
    proxy: { mode: "inherit" },
    categories: [],
    tags: [],
    instructions: { en: "", "zh-CN": "" },
    json_release: {
      default_channel: "stable",
      refresh_seconds: 3600,
      channels: { stable: { requests: [{ url: "https://u.example/latest" }], query: ".body" } },
      ...(lookup
        ? { lookup: { requests: [{ url: "https://u.example/v/{version}" }], query: ".body" } }
        : {}),
      sources: {
        metadata: [{ origin: "https://u.example", prefix: "/" }],
        artifacts: [{ origin: "https://u.example", prefix: "/files/" }],
        checksums: [],
      },
    },
  };
  return codexConfiguration({ template_ref: null, defaults: null, effective: spec, fields: {} });
}

describe("release lookup form", () => {
  it("discovers a version the administrator enters", async () => {
    let requested: unknown;
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => configuration(true)),
      mockApi("post", "/admin/api/apps/{vendor}/{app}/releases/lookup", async ({ request }) => {
        requested = await request.json();
        return {
          version: "1.0.0",
          build: "",
          discovered_at: "2026-10-01T00:00:00Z",
          channels: [],
        };
      }),
    );
    await renderAppPage(ReleaseLookupForm, {
      key: "acme/editor",
      props: { vendor: "acme", app: "editor" },
    });
    const user = userEvent.setup();
    await user.type(await screen.findByRole("textbox", { name: "Version" }), " 1.0.0 ");
    await user.click(screen.getByRole("button", { name: "Discover" }));
    expect(await screen.findByText("Version 1.0.0 discovered")).toBeInTheDocument();
    expect(requested).toEqual({ version: "1.0.0" });
  });

  it("explains a configuration without lookup", async () => {
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => configuration(false)),
    );
    await renderAppPage(ReleaseLookupForm, {
      key: "acme/editor",
      props: { vendor: "acme", app: "editor" },
    });
    expect(await screen.findByText(/has no lookup/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Discover" })).not.toBeInTheDocument();
  });
});
