import { screen, waitFor } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import { recorder, renderAdminPage } from "@/test/directory";
import { notes, vendor } from "@/test/factories/directory";
import { apiError, mockApi, useHandlers } from "@/test/msw";

describe("vendor admin notes", () => {
  it("shows rendered notes until the edit button opens the text", async () => {
    const source = "## Token\n\n**Rotate**";
    useHandlers(
      mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
      mockApi("get", "/admin/api/vendors/{vendor}/admin-notes", () =>
        notes({ text: source, html: "<h2>Token</h2>\n<p><strong>Rotate</strong></p>\n" }),
      ),
    );
    const { user } = await renderAdminPage("/admin/vendors/example/admin-notes");
    expect(await screen.findByRole("heading", { name: "Token" })).toBeInTheDocument();
    expect(screen.getByText("Rotate").tagName).toBe("STRONG");
    expect(screen.queryByRole("textbox", { name: "Notes" })).toBeNull();

    await user.click(screen.getByRole("button", { name: "Edit notes" }));
    const text = screen.getByRole("textbox", { name: "Notes" });
    expect(text).toHaveValue(source);
    await user.type(text, " soon");
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("textbox", { name: "Notes" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Edit notes" }));
    expect(screen.getByRole("textbox", { name: "Notes" })).toHaveValue(source);
  });

  it("shows a placeholder for empty notes and none of the editor for deleted vendors", async () => {
    useHandlers(
      mockApi("get", "/admin/api/vendors/{vendor}", () =>
        vendor({ deleted_at: "2026-01-01T00:00:00Z" }),
      ),
      mockApi("get", "/admin/api/vendors/{vendor}/admin-notes", () =>
        notes({ text: "", html: "" }),
      ),
    );
    await renderAdminPage("/admin/vendors/example/admin-notes");
    expect(await screen.findByText("No notes yet.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit notes" })).toBeNull();
  });

  it("saves with the notes revision and keeps the draft on a conflict", async () => {
    let revision = 2;
    let conflict = true;
    const saves = recorder<{ text: string }>();
    useHandlers(
      mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
      mockApi("get", "/admin/api/vendors/{vendor}/admin-notes", () =>
        notes({ revision, text: revision === 2 ? "Old note" : "Someone else's note" }),
      ),
      mockApi("put", "/admin/api/vendors/{vendor}/admin-notes", async ({ request }) => {
        const body = await saves.record(request);
        if (conflict) {
          conflict = false;
          revision = 3;
          return apiError("REVISION_CONFLICT");
        }
        return notes({ revision: 4, text: body.text });
      }),
    );
    const { user } = await renderAdminPage("/admin/vendors/example/admin-notes");
    await user.click(await screen.findByRole("button", { name: "Edit notes" }));
    const text = await screen.findByRole("textbox", { name: "Notes" });
    await waitFor(() => {
      expect(text).toHaveValue("Old note");
    });
    await user.clear(text);
    await user.type(text, "Rotate the token");
    expect(screen.getByText("Unsaved changes")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save notes" }));

    expect(await screen.findByText("Changed by someone else")).toBeInTheDocument();
    expect(saves.calls[0]).toMatchObject({ ifMatch: '"2"', body: { text: "Rotate the token" } });
    await user.click(screen.getByRole("button", { name: "Reload latest version" }));
    await waitFor(() => {
      expect(screen.queryByText("Changed by someone else")).toBeNull();
    });
    expect(text).toHaveValue("Rotate the token");

    await user.click(screen.getByRole("button", { name: "Save notes" }));
    expect(await screen.findByText("Notes saved.")).toBeInTheDocument();
    expect(saves.calls[1]?.ifMatch).toBe('"3"');
    expect(screen.queryByText("Unsaved changes")).toBeNull();
    expect(screen.queryByRole("textbox", { name: "Notes" })).toBeNull();
    expect(screen.getByRole("button", { name: "Edit notes" })).toBeInTheDocument();
  });

  it("limits notes to 12000 characters", async () => {
    const saves = recorder();
    useHandlers(
      mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
      mockApi("get", "/admin/api/vendors/{vendor}/admin-notes", () => notes({ text: "" })),
      mockApi("put", "/admin/api/vendors/{vendor}/admin-notes", async ({ request }) => {
        await saves.record(request);
        return notes();
      }),
    );
    const { user } = await renderAdminPage("/admin/vendors/example/admin-notes");
    await user.click(await screen.findByRole("button", { name: "Edit notes" }));
    const text = screen.getByRole("textbox", { name: "Notes" });
    await user.click(text);
    await user.paste("x".repeat(12001));
    await user.click(screen.getByRole("button", { name: "Save notes" }));
    expect(await screen.findByText("At most 12000 characters.")).toBeInTheDocument();
    expect(text).toHaveAccessibleDescription(/12,001 of 12,000 characters/);
    expect(saves.calls).toHaveLength(0);
  });
});
