import { screen, waitFor } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { DownloadChoicesSection } from "@/features/json-release";
import type { Schema } from "@/shared/api";
import { renderAppPage } from "@/test/appPage";
import { codexConfiguration } from "@/test/factories/runtime";
import { mockApi, useHandlers } from "@/test/msw";

type Patch = Schema<"AppConfigurationPatch">;

function configuration(choices: { default_artifact: string; hidden_artifacts: string[] }) {
  const spec: Schema<"AppSpec"> = {
    name: { en: "Visual Studio Code", "zh-CN": "Visual Studio Code" },
    description: { en: "", "zh-CN": "" },
    icon: "",
    provider: "json-release",
    proxy: { mode: "inherit" },
    categories: [],
    tags: [],
    instructions: { en: "", "zh-CN": "" },
    ...choices,
  };
  return codexConfiguration({
    template_ref: "microsoft/vscode",
    defaults: { ...spec, default_artifact: "win32-x64-user", hidden_artifacts: [] },
    effective: spec,
    fields: {},
  });
}

const known: Schema<"ReleaseArtifactChoice">[] = [
  { key: "win32-x64-user", os: "windows", arch: "x64", format: "exe", scope: "user" },
  { key: "linux-deb-x64", os: "linux", arch: "x64", format: "deb", scope: "system" },
];

describe("download choices section", () => {
  it("chooses the default and hides packages, keeping keys no longer released", async () => {
    let patch: Patch | undefined;
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () =>
        configuration({ default_artifact: "win32-x64-user", hidden_artifacts: ["gone-key"] }),
      ),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/artifacts", () => ({ items: known })),
      mockApi("patch", "/admin/api/apps/{vendor}/{app}/configuration", async ({ request }) => {
        patch = (await request.json()) as Patch;
        return configuration({ default_artifact: "", hidden_artifacts: ["linux-deb-x64"] });
      }),
    );
    await renderAppPage(DownloadChoicesSection, {
      key: "microsoft/vscode",
      props: { vendor: "microsoft", app: "vscode" },
    });
    const user = userEvent.setup();

    const deb = await screen.findByRole("checkbox", {
      name: "Linux · x64 · deb · system (linux-deb-x64)",
    });
    expect(deb).toBeChecked();
    // A hidden key the releases no longer have stays visible until cleared.
    const gone = screen.getByRole("checkbox", { name: "gone-key (not in the current releases)" });
    expect(gone).not.toBeChecked();
    await user.click(deb);
    await user.click(gone);
    await user.click(screen.getByRole("combobox", { name: "Default package" }));
    await user.click(await screen.findByRole("option", { name: "None (the first package)" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Download choices saved")).toBeInTheDocument();
    expect(patch).toEqual({
      set: { default_artifact: "", hidden_artifacts: ["linux-deb-x64"] },
    });
    await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeDisabled());
  });
});
