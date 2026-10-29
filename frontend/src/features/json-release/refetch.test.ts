import { screen, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { ReleaseRefetchButton } from "@/features/json-release";
import { mockApi, useHandlers } from "@/test/msw";
import { renderWithApp } from "@/test/render";

describe("release re-fetch", () => {
  it("re-fetches a version after confirmation", async () => {
    const requested: string[] = [];
    useHandlers(
      mockApi("post", "/admin/api/apps/{vendor}/{app}/versions/{version}/refetch", ({ params }) => {
        requested.push(String(params.version));
        return {
          release: {
            version: "1.2.3",
            build: "",
            discovered_at: "2026-10-01T00:00:00Z",
            channels: [],
          },
          replaced: true,
        };
      }),
    );
    await renderWithApp(ReleaseRefetchButton, {
      props: { vendor: "acme", app: "editor", version: "1.2.3" },
    });
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Re-fetch" }));
    expect(await screen.findByText("Re-fetch version 1.2.3?")).toBeInTheDocument();
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Re-fetch" }));
    expect(
      await screen.findByText("Version 1.2.3 replaced with the upstream's current build"),
    ).toBeInTheDocument();
    expect(requested).toEqual(["1.2.3"]);
  });
});
