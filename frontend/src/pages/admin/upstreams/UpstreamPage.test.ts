import { screen, waitFor, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { recorder, renderAdminPage } from "@/test/directory";
import { app, page } from "@/test/factories/directory";
import { localized } from "@/test/factories";
import { UPSTREAM_UID, upstream, upstreamCatalogItem } from "@/test/factories/mirrors";
import { apiError, mockApi, useHandlers } from "@/test/msw";

const catalog = [
  upstreamCatalogItem(),
  upstreamCatalogItem({ key: "acme/tools", provider: "http-cache", name: localized("Tools") }),
  upstreamCatalogItem({ key: "acme/codex", provider: "codex", name: localized("Codex") }),
  upstreamCatalogItem({
    key: "acme/legacy",
    provider: "legacy",
    name: localized("Legacy"),
    supported: false,
  }),
  upstreamCatalogItem({
    key: "acme/feed",
    provider: "json-release",
    name: localized("Feed"),
    mirror: "mirror/feed",
  }),
];

function catalogServer() {
  const mirrors = recorder<Schema<"MirrorRequest">>();
  const queries: URLSearchParams[] = [];
  useHandlers(
    mockApi("get", "/admin/api/upstreams/{upstream}", () => upstream()),
    mockApi("get", "/admin/api/upstreams/{upstream}/catalog", ({ request }) => {
      const query = new URL(request.url).searchParams;
      queries.push(query);
      const q = query.get("q") ?? "";
      return page(
        catalog.filter((item) => item.key.includes(q)),
        { limit: 25 },
      );
    }),
    // Only acme/tools exists locally.
    mockApi("get", "/admin/api/apps/{vendor}/{app}", ({ params }) =>
      params.vendor === "acme" && params.app === "tools"
        ? app({ vendor_id: "acme", id: "tools", revision: 5 })
        : apiError("APPLICATION_NOT_FOUND"),
    ),
    mockApi("post", "/admin/api/upstreams/{upstream}/mirrors", async ({ request }) => {
      const body = await mirrors.record(request);
      if (body.remote_key === "acme/codex") return apiError("LOOP_DETECTED");
      return app({ vendor_id: body.vendor, id: body.app, uid: body.app.padEnd(32, "0") });
    }),
  );
  return { mirrors, queries };
}

function item(dialog: HTMLElement, key: string): HTMLElement {
  const row = within(dialog).getByText(key).closest("li");
  if (!row) throw new Error(`no row for ${key}`);
  return row;
}

describe("upstream catalog", () => {
  it("shows the upstream and the state of each published application", async () => {
    const { queries } = catalogServer();
    const { router, user } = await renderAdminPage(`/admin/upstreams/${UPSTREAM_UID}`);
    expect(await screen.findByRole("heading", { level: 1, name: "DMZ" })).toBeInTheDocument();
    expect(screen.getByText("https://dmz.example")).toBeInTheDocument();
    expect(screen.getByText("f".repeat(32))).toBeInTheDocument();

    const table = await screen.findByRole("table", { name: "Applications of the upstream" });
    expect(await within(table).findByText("Not supported")).toBeInTheDocument();
    expect(within(table).queryByRole("checkbox", { name: "Select Legacy" })).toBeNull();
    expect(within(table).queryByRole("checkbox", { name: "Select Feed" })).toBeNull();
    expect(within(table).getByRole("link", { name: "Mirrored as mirror/feed" })).toHaveAttribute(
      "href",
      "/admin/vendors/mirror/apps/feed/settings",
    );
    expect(queries[0]?.get("limit")).toBe("25");

    await user.type(screen.getByRole("searchbox", { name: "Search applications" }), "tools");
    await waitFor(() => {
      expect(router.currentRoute.value.query.q).toBe("tools");
    });
    await waitFor(() => {
      expect(within(table).queryByText("Feed")).toBeNull();
    });
    expect(queries.at(-1)?.get("q")).toBe("tools");
  });

  it("checks the target again after a failed adoption and needs a new confirmation", async () => {
    const { mirrors } = catalogServer();
    let revision = 5;
    let conflicts = 1;
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}", () =>
        app({ vendor_id: "acme", id: "tools", revision }),
      ),
      mockApi("post", "/admin/api/upstreams/{upstream}/mirrors", async ({ request }) => {
        const body = await mirrors.record(request);
        if (conflicts > 0) {
          // Someone saved the local application meanwhile.
          conflicts -= 1;
          revision = 6;
          return apiError("REVISION_CONFLICT");
        }
        return app({ vendor_id: body.vendor, id: body.app });
      }),
    );
    const { user } = await renderAdminPage(`/admin/upstreams/${UPSTREAM_UID}`);
    const table = await screen.findByRole("table", { name: "Applications of the upstream" });
    await user.click(await within(table).findByRole("checkbox", { name: "Select Tools" }));
    await user.click(screen.getByRole("button", { name: "Mirror selected" }));
    const dialog = await screen.findByRole("dialog", { name: "Mirror applications" });
    const tools = item(dialog, "acme/tools");
    const adopt = await within(tools).findByRole("checkbox", {
      name: "Adopt the existing application",
    });
    await user.click(adopt);
    await user.click(within(dialog).getByRole("button", { name: "Mirror" }));

    expect(
      await within(tools).findByText("Someone else changed this item. Reload before saving."),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(
        within(tools).getByRole("checkbox", { name: "Adopt the existing application" }),
      ).not.toBeChecked();
    });
    const retry = within(dialog).getByRole("button", { name: "Mirror the remaining applications" });
    await waitFor(() => {
      expect(retry).toBeEnabled();
    });
    await user.click(retry);
    expect(
      within(tools).getByText("Confirm adopting the existing application or choose another ID."),
    ).toBeInTheDocument();
    expect(mirrors.calls).toHaveLength(1);

    await user.click(
      within(tools).getByRole("checkbox", { name: "Adopt the existing application" }),
    );
    await user.click(retry);
    expect(
      await within(tools).findByRole("link", { name: "Mirrored as acme/tools" }),
    ).toBeInTheDocument();
    expect(mirrors.calls.map((call) => call.body.revision)).toEqual([5, 6]);
  });

  it("mirrors the selection row by row, adopting an existing application", async () => {
    const { mirrors, queries } = catalogServer();
    const { user } = await renderAdminPage(`/admin/upstreams/${UPSTREAM_UID}`);
    const table = await screen.findByRole("table", { name: "Applications of the upstream" });
    const mirrorSelected = screen.getByRole("button", { name: "Mirror selected" });
    expect(mirrorSelected).toBeDisabled();
    for (const name of ["Files", "Tools", "Codex"]) {
      await user.click(await within(table).findByRole("checkbox", { name: `Select ${name}` }));
    }
    expect(screen.getByText("3 selected")).toBeInTheDocument();
    await user.click(mirrorSelected);

    const dialog = await screen.findByRole("dialog", { name: "Mirror applications" });
    const files = item(dialog, "acme/files");
    expect(within(files).getByLabelText("Local vendor ID")).toHaveValue("acme");
    expect(within(files).getByLabelText("Local application ID")).toHaveValue("files");
    expect(await within(files).findByText("A new application is created.")).toBeInTheDocument();

    // An existing application of another provider cannot be adopted.
    const codex = item(dialog, "acme/codex");
    const codexId = within(codex).getByLabelText("Local application ID");
    await user.clear(codexId);
    await user.type(codexId, "tools");
    expect(
      await within(codex).findByText(
        "The existing application uses HTTP cache; an upstream Codex application can only be mirrored into a Codex application.",
      ),
    ).toBeInTheDocument();
    await user.clear(codexId);
    await user.type(codexId, "codex");
    expect(await within(codex).findByText("A new application is created.")).toBeInTheDocument();

    // The existing application needs an explicit adoption.
    const tools = item(dialog, "acme/tools");
    expect(await within(tools).findByText("acme/tools already exists.")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Mirror" }));
    expect(
      within(tools).getByText("Confirm adopting the existing application or choose another ID."),
    ).toBeInTheDocument();
    expect(mirrors.calls).toHaveLength(0);
    await user.click(
      within(tools).getByRole("checkbox", { name: "Adopt the existing application" }),
    );
    const before = queries.length;
    await user.click(within(dialog).getByRole("button", { name: "Mirror" }));

    expect(
      await within(files).findByRole("link", { name: "Mirrored as acme/files" }),
    ).toHaveAttribute("href", "/admin/vendors/acme/apps/files/settings");
    expect(
      await within(tools).findByRole("link", { name: "Mirrored as acme/tools" }),
    ).toBeInTheDocument();
    expect(
      await within(codex).findByText("The request came back to this RedApp through its upstreams."),
    ).toBeInTheDocument();
    expect(mirrors.calls.map((call) => call.body)).toEqual([
      { remote_key: "acme/files", vendor: "acme", app: "files" },
      { remote_key: "acme/tools", vendor: "acme", app: "tools", revision: 5 },
      { remote_key: "acme/codex", vendor: "acme", app: "codex" },
    ]);
    expect(within(dialog).getByText("2 of 3 applications mirrored.")).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Mirror the remaining applications" }),
    ).toBeEnabled();
    // The catalog is reloaded and only the failed application stays selected.
    await waitFor(() => {
      expect(queries.length).toBeGreaterThan(before);
    });
    expect(screen.getByText("1 selected")).toBeInTheDocument();
  });
});
