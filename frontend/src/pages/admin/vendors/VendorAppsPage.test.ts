import { screen, waitFor, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import { recorder, renderAdminPage } from "@/test/directory";
import { appListItem, page, vendor } from "@/test/factories/directory";
import { apiError, mockApi, useHandlers } from "@/test/msw";

function appsServer(
  items = [appListItem({ latest_version: "1.2.0", successful_downloads: 1234 })],
) {
  const requests: URLSearchParams[] = [];
  useHandlers(
    mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
    mockApi("get", "/admin/api/apps", ({ request }) => {
      requests.push(new URL(request.url).searchParams);
      return page(items);
    }),
  );
  return requests;
}

describe("vendor applications", () => {
  it("sorts on the server: name ascending first, other columns descending first", async () => {
    const requests = appsServer();
    const { user } = await renderAdminPage("/admin/vendors/example/apps");
    const table = await screen.findByRole("table", { name: "Applications of Example" });
    expect(await within(table).findByText("1.2.0")).toBeInTheDocument();
    expect(within(table).getByText("1,234")).toBeInTheDocument();
    const last = () => Object.fromEntries(requests.at(-1) ?? []);
    expect(last()).toMatchObject({ vendor: "example", sort: "name", order: "asc", limit: "20" });

    await user.click(within(table).getByRole("button", { name: "Latest version" }));
    await waitFor(() => {
      expect(last()).toMatchObject({ sort: "version", order: "desc" });
    });
    expect(within(table).getByRole("columnheader", { name: "Latest version" })).toHaveAttribute(
      "aria-sort",
      "descending",
    );
    await user.click(within(table).getByRole("button", { name: "Latest version" }));
    await waitFor(() => {
      expect(last()).toMatchObject({ sort: "version", order: "asc" });
    });
    // Back to the name column: ascending again (served from the query cache).
    await user.click(within(table).getByRole("button", { name: "Application" }));
    await waitFor(() => {
      expect(within(table).getByRole("columnheader", { name: "Application" })).toHaveAttribute(
        "aria-sort",
        "ascending",
      );
    });
  });

  it("enables a row at once with its revision", async () => {
    appsServer();
    const updates = recorder<{ enabled: boolean }>();
    useHandlers(
      mockApi("patch", "/admin/api/apps/{vendor}/{app}", async ({ request }) => {
        const body = await updates.record(request);
        return { ...appListItem(), enabled: body.enabled, revision: 10 };
      }),
    );
    const { user } = await renderAdminPage("/admin/vendors/example/apps");
    await user.click(await screen.findByRole("switch", { name: "Enable Tools" }));
    await waitFor(() => {
      expect(updates.calls).toHaveLength(1);
    });
    expect(updates.calls[0]).toMatchObject({ ifMatch: '"9"', body: { enabled: false } });
  });

  it("treats 404 on a confirmed delete as done and protects built-in rows", async () => {
    appsServer([
      appListItem(),
      appListItem({
        id: "preset",
        uid: "1".repeat(32),
        builtin_template: true,
        name: { en: "Preset", "zh-CN": "预置" },
      }),
    ]);
    const deletes = recorder();
    useHandlers(
      mockApi("delete", "/admin/api/apps/{vendor}/{app}", async ({ request }) => {
        await deletes.record(request);
        return apiError("APPLICATION_NOT_FOUND");
      }),
    );
    const { user } = await renderAdminPage("/admin/vendors/example/apps");
    const preset = await screen.findByRole("button", { name: "Delete Preset" });
    expect(preset).toHaveAttribute("aria-disabled", "true");
    await user.click(preset);
    expect(screen.queryByRole("alertdialog")).toBeNull();

    await user.click(screen.getByRole("button", { name: "Delete Tools" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Delete application example/tools?",
    });
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    expect(await screen.findByText("example/tools deleted.")).toBeInTheDocument();
    expect(deletes.calls[0]?.url.searchParams.get("confirm_uid")).toBe(
      "ffeeddccbbaa99887766554433221100",
    );
    expect(deletes.calls[0]?.ifMatch).toBe('"9"');
    // No error toast for the 404.
    expect(screen.queryByText(/not found/i)).toBeNull();
  });
});
