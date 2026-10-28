import { screen, waitFor, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { recorder, renderAdminPage } from "@/test/directory";
import { page } from "@/test/factories/directory";
import { UPSTREAM_UID, upstream } from "@/test/factories/mirrors";
import { apiError, mockApi, noContent, useHandlers } from "@/test/msw";

const LAB_UID = "11112222333344445555666677778888";

function upstreamServer() {
  const items: Schema<"Upstream">[] = [upstream()];
  const posts = recorder<Schema<"UpstreamRequest">>();
  const deletes = recorder();
  useHandlers(
    mockApi("get", "/admin/api/upstreams", () => ({ items: [...items] })),
    mockApi("post", "/admin/api/upstreams", async ({ request }) => {
      const body = await posts.record(request);
      if (body.origin === "https://self.example") return apiError("LOOP_DETECTED");
      const created = upstream({ uid: LAB_UID, ...body, mirrors: 0 });
      items.push(created);
      return created;
    }),
    mockApi("get", "/admin/api/upstreams/{upstream}", ({ params }) => {
      return items.find((item) => item.uid === params.upstream) ?? apiError("UPSTREAM_NOT_FOUND");
    }),
    mockApi("get", "/admin/api/upstreams/{upstream}/catalog", () => page([], { limit: 25 })),
    // DMZ still has mirrors.
    mockApi("delete", "/admin/api/upstreams/{upstream}", async ({ request, params }) => {
      await deletes.record(request);
      return params.upstream === UPSTREAM_UID ? apiError("UPSTREAM_IN_USE") : noContent();
    }),
  );
  return { posts, deletes };
}

describe("upstreams", () => {
  it("lists upstreams and adds one after the server verified it", async () => {
    const { posts } = upstreamServer();
    const { router, user } = await renderAdminPage("/admin/upstreams");
    const table = await screen.findByRole("table", { name: "Upstreams" });
    const row = (await within(table).findByRole("link", { name: "DMZ" })).closest("tr");
    if (!row) throw new Error("no row");
    expect(within(row).getByText("https://dmz.example")).toBeInTheDocument();
    expect(within(row).getByText("2")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Add upstream" }));
    const dialog = await screen.findByRole("dialog", { name: "Add upstream" });
    await user.type(within(dialog).getByLabelText(/^Name/), "Lab");
    const address = within(dialog).getByLabelText(/^Address/);
    await user.type(address, "lab.example/path");
    await user.click(within(dialog).getByRole("button", { name: "Add upstream" }));
    expect(await within(dialog).findByText(/Enter an address like/)).toBeInTheDocument();
    expect(posts.calls).toHaveLength(0);

    // The server's verdict is shown in the dialog.
    await user.clear(address);
    await user.type(address, "https://self.example");
    await user.click(within(dialog).getByRole("button", { name: "Add upstream" }));
    expect(
      await within(dialog).findByText(
        "The request came back to this RedApp through its upstreams.",
      ),
    ).toBeInTheDocument();

    await user.clear(address);
    await user.type(address, " http://10.0.0.5:8080/ ");
    await user.click(within(dialog).getByRole("button", { name: "Add upstream" }));
    await waitFor(() => {
      expect(router.currentRoute.value.params.upstream).toBe(LAB_UID);
    });
    expect(posts.calls.at(-1)?.body).toEqual({ name: "Lab", origin: "http://10.0.0.5:8080" });
    expect(await screen.findByRole("heading", { level: 1, name: "Lab" })).toBeInTheDocument();
  });

  it("renames and moves an upstream, keeping the draft on a revision conflict", async () => {
    upstreamServer();
    let revision = 1;
    const patches = recorder<Schema<"UpstreamRequest">>();
    useHandlers(
      mockApi("get", "/admin/api/upstreams/{upstream}", () => upstream({ revision })),
      mockApi("patch", "/admin/api/upstreams/{upstream}", async ({ request }) => {
        const body = await patches.record(request);
        if (request.headers.get("If-Match") !== `"${String(revision)}"`) {
          return apiError("REVISION_CONFLICT");
        }
        revision += 1;
        return upstream({ ...body, revision });
      }),
    );
    const { user } = await renderAdminPage("/admin/upstreams");
    await user.click(await screen.findByRole("button", { name: "Edit DMZ" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit upstream DMZ" });
    const name = within(dialog).getByLabelText(/^Name/);
    await waitFor(() => {
      expect(name).toHaveValue("DMZ");
    });
    // Another administrator saves meanwhile.
    revision = 2;
    await user.clear(name);
    await user.type(name, "DMZ 2");
    const address = within(dialog).getByLabelText(/^Address/);
    await user.clear(address);
    await user.type(address, "https://dmz2.example:8443");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await user.click(await within(dialog).findByRole("button", { name: "Reload latest version" }));
    expect(name).toHaveValue("DMZ 2");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Upstream saved.")).toBeInTheDocument();
    expect(patches.calls.map((call) => call.ifMatch)).toEqual(['"1"', '"2"']);
    expect(patches.calls[1]?.body).toEqual({ name: "DMZ 2", origin: "https://dmz2.example:8443" });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("refuses to delete an upstream with mirrors and deletes unused ones", async () => {
    const { deletes } = upstreamServer();
    useHandlers(
      mockApi("get", "/admin/api/upstreams", () => ({
        items: [upstream(), upstream({ uid: LAB_UID, name: "Lab", mirrors: 0, revision: 4 })],
      })),
    );
    const { user } = await renderAdminPage("/admin/upstreams");
    await user.click(await screen.findByRole("button", { name: "Delete DMZ" }));
    const confirm = await screen.findByRole("alertdialog", { name: "Delete upstream DMZ?" });
    await user.click(within(confirm).getByRole("button", { name: "Delete" }));
    expect(await screen.findByText(/DMZ still has mirrored applications/)).toBeInTheDocument();
    expect(deletes.calls[0]?.ifMatch).toBe('"1"');

    await user.click(screen.getByRole("button", { name: "Delete Lab" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Delete" }),
    );
    expect(await screen.findByText("Upstream Lab deleted.")).toBeInTheDocument();
    expect(deletes.calls[1]).toMatchObject({ ifMatch: '"4"' });
    expect(deletes.calls[1]?.url.pathname).toBe(`/admin/api/upstreams/${LAB_UID}`);
  });
});
