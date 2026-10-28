import { screen, waitFor } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { AutoIndexSection } from "@/features/file-index";
import type { Schema } from "@/shared/api";
import { renderAppPage } from "@/test/appPage";
import { httpCacheConfiguration } from "@/test/factories/runtime";
import { mockApi, useHandlers } from "@/test/msw";

type Patch = Schema<"AppConfigurationPatch">;

describe("directory listing section", () => {
  it("turns directory listings on and saves only auto_index", async () => {
    let patch: { body: Patch; ifMatch: string | null } | undefined;
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () =>
        httpCacheConfiguration(),
      ),
      mockApi("patch", "/admin/api/apps/{vendor}/{app}/configuration", async ({ request }) => {
        const body = (await request.json()) as Patch;
        patch = { body, ifMatch: request.headers.get("If-Match") };
        const next = httpCacheConfiguration({}, { revision: 8 });
        next.effective.auto_index = true;
        return next;
      }),
    );
    await renderAppPage(AutoIndexSection, {
      key: "example/mirror",
      props: { vendor: "example", app: "mirror" },
    });
    const user = userEvent.setup();

    const toggle = await screen.findByRole("switch", { name: "List directories" });
    expect(toggle).not.toBeChecked();
    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();
    await user.click(toggle);
    await user.click(save);
    expect(await screen.findByText("Directory listings saved.")).toBeInTheDocument();
    expect(patch).toEqual({ ifMatch: '"7"', body: { set: { auto_index: true } } });
    await waitFor(() => {
      expect(save).toBeDisabled();
    });
  });
});
