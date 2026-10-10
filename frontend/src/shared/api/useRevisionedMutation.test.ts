import { screen, waitFor } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import Editor from "@/test/components/RevisionedEditor.vue";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { siteSettingsState } from "@/test/factories";
import { renderWithApp } from "@/test/render";

describe("useRevisionedMutation", () => {
  it("replaces the cached resource with the saved state", async () => {
    let ifMatch: string | null = null;
    useHandlers(
      mockApi("get", "/admin/api/settings/site", () => siteSettingsState({ revision: 3 })),
      mockApi("put", "/admin/api/settings/site", async ({ request }) => {
        ifMatch = request.headers.get("If-Match");
        const body = (await request.json()) as ReturnType<typeof siteSettingsState>;
        return { ...body, revision: 4 };
      }),
    );
    await renderWithApp(Editor);
    const user = userEvent.setup();
    const input = await screen.findByDisplayValue("RedApp Mirror");
    await user.clear(input);
    await user.type(input, "New title");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Saved: New title (r4)")).toBeInTheDocument();
    expect(ifMatch).toBe('"3"');
  });

  it("keeps the draft on REVISION_CONFLICT and reloads the baseline on request", async () => {
    let revision = 3;
    useHandlers(
      mockApi("get", "/admin/api/settings/site", () => siteSettingsState({ revision })),
      mockApi("put", "/admin/api/settings/site", () => apiError("REVISION_CONFLICT")),
    );
    await renderWithApp(Editor);
    const user = userEvent.setup();
    const input = await screen.findByDisplayValue("RedApp Mirror");
    await user.clear(input);
    await user.type(input, "My draft");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Changed by someone else")).toBeInTheDocument();
    expect(screen.getByDisplayValue("My draft")).toBeInTheDocument();
    // Conflicts are presented inline, not as an error toast.
    expect(screen.queryByText("Someone else changed this item. Reload before saving.")).toBeNull();

    revision = 5;
    await user.click(screen.getByRole("button", { name: "Reload latest version" }));
    await waitFor(() => {
      expect(screen.getByText("Saved: RedApp Mirror (r5)")).toBeInTheDocument();
    });
    expect(screen.queryByText("Changed by someone else")).toBeNull();
  });

  it("reports other failures through the global error toast with the request ID", async () => {
    useHandlers(
      mockApi("get", "/admin/api/settings/site", () => siteSettingsState()),
      mockApi("put", "/admin/api/settings/site", () =>
        apiError("VALIDATION_FAILED", {
          message: "title.en must be 1 to 80 characters",
          requestId: "1234567890abcdef",
        }),
      ),
    );
    await renderWithApp(Editor);
    const user = userEvent.setup();
    await screen.findByDisplayValue("RedApp Mirror");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("A value is invalid.")).toBeInTheDocument();
    expect(screen.getByText("title.en must be 1 to 80 characters")).toBeInTheDocument();
    expect(screen.getByText("1234567890abcdef")).toBeInTheDocument();
  });
});
