import { fireEvent, screen, waitFor } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { appConfiguration } from "@/test/factories/directory";
import { mockApi, useHandlers } from "@/test/msw";
import { renderAppPage } from "@/test/appPage";
import JSONReleaseSection from "./JSONReleaseSection.vue";
const config: Schema<"JSONReleaseConfig"> = {
  default_channel: "stable",
  refresh_seconds: 300,
  channels: { stable: { query: ".body", requests: [{ url: "https://metadata.example/api" }] } },
  sources: {
    metadata: [{ origin: "https://metadata.example", prefix: "/" }],
    artifacts: [{ origin: "https://files.example", prefix: "/" }],
    checksums: [],
  },
  prewarm: [],
};
const configuration = () => {
  const data = appConfiguration();
  data.effective = {
    name: data.effective.name,
    description: data.effective.description,
    icon: "",
    provider: "json-release",
    proxy: { mode: "inherit" },
    categories: [],
    tags: [],
    instructions: { en: "", "zh-CN": "" },
    json_release: config,
  };
  return data;
};
describe("JSON release settings", () => {
  it("previews without saving and saves exact prewarm keys with revision protection", async () => {
    let saved: Schema<"AppConfigurationPatch"> | undefined;
    let revision: string | null = null;
    let previewBody: unknown;
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => configuration()),
      mockApi("post", "/admin/api/apps/{vendor}/{app}/release-preview", async ({ request }) => {
        previewBody = await request.json();
        return {
          version: "1.2.3",
          artifacts: [
            {
              key: "win-user",
              os: "windows" as const,
              arch: "x64" as const,
              format: "exe",
              url: "https://files.example/setup.exe",
              sha256: "a".repeat(64),
            },
          ],
        };
      }),
      mockApi("patch", "/admin/api/apps/{vendor}/{app}/configuration", async ({ request }) => {
        saved = (await request.json()) as Schema<"AppConfigurationPatch">;
        revision = request.headers.get("If-Match");
        const data = configuration();
        data.revision = 10;
        data.effective.json_release = saved.set?.json_release;
        return data;
      }),
    );
    await renderAppPage(JSONReleaseSection, {
      key: "example/editor",
      props: { vendor: "example", app: "editor" },
    });
    const user = userEvent.setup();
    await screen.findByRole("textbox", { name: "Request and query configuration (JSON)" });
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Preview" }));
    const option = await screen.findByRole("checkbox", { name: "windows · x64 · exe · win-user" });
    expect(saved).toBeUndefined();
    expect(previewBody).toEqual({ config, channel: "latest" });
    await user.click(option);
    await user.click(screen.getByRole("button", { name: "Use selected prewarm keys" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(saved?.set?.json_release?.prewarm).toEqual([
        { channel: "stable", keys: ["win-user"] },
      ]),
    );
    expect(revision).toBe('"9"');
    expect(saved?.set?.json_release?.channels).toEqual(config.channels);
  });
  it("rejects invalid JSON locally without claiming preview isolation", async () => {
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => configuration()),
    );
    await renderAppPage(JSONReleaseSection, { props: { vendor: "example", app: "editor" } });
    const editor = await screen.findByRole("textbox", {
      name: "Request and query configuration (JSON)",
    });
    await fireEvent.update(editor, "{");
    expect(screen.getByRole("button", { name: "Preview" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByText(/not a sandbox/)).toBeInTheDocument();
  });
});
