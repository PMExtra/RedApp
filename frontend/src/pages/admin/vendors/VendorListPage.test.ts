import { screen, waitFor, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import { renderAdminPage } from "@/test/directory";
import { app, appListItem, page, vendorListItem } from "@/test/factories/directory";
import { mockApi, useHandlers } from "@/test/msw";

function vendorServer() {
  const requests: URLSearchParams[] = [];
  useHandlers(
    mockApi("get", "/admin/api/vendors", ({ request }) => {
      const query = new URL(request.url).searchParams;
      requests.push(query);
      if (query.get("q") === "nothing") return page([], { limit: 12 });
      return page([vendorListItem()], { limit: 12 });
    }),
  );
  return requests;
}

describe("vendor list", () => {
  it("searches and filters through the URL, and offers deleted only when asked", async () => {
    const requests = vendorServer();
    const { router, user } = await renderAdminPage("/admin/vendors");
    expect(await screen.findByRole("heading", { level: 2, name: "Example" })).toBeInTheDocument();
    expect(requests.at(-1)?.get("state")).toBe("current");
    expect(requests.at(-1)?.get("limit")).toBe("12");

    const filter = screen.getByRole("group", { name: "Availability" });
    expect(within(filter).queryByRole("button", { name: "Deleted" })).toBeNull();
    await user.click(within(filter).getByRole("button", { name: "Enabled" }));
    await waitFor(() => {
      expect(router.currentRoute.value.query.state).toBe("enabled");
    });
    await waitFor(() => {
      expect(requests.at(-1)?.get("state")).toBe("enabled");
    });
    expect(within(filter).getByRole("button", { name: "Enabled" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    await user.type(
      screen.getByRole("searchbox", { name: "Search vendors and applications" }),
      "nothing",
    );
    await waitFor(() => {
      expect(router.currentRoute.value.query.q).toBe("nothing");
    });
    expect(await screen.findByText("No vendors in this view")).toBeInTheDocument();
    expect(requests.at(-1)?.get("q")).toBe("nothing");

    await router.push("/admin/vendors?state=deleted");
    await waitFor(() => {
      expect(requests.at(-1)?.get("state")).toBe("deleted");
    });
    expect(within(filter).getByRole("button", { name: "Deleted" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("shows the pending cleanup notice after a deletion", async () => {
    vendorServer();
    await renderAdminPage("/admin/vendors?cleanup=pending");
    expect(await screen.findByText(/awaiting cleanup/)).toBeInTheDocument();
  });

  it("loads every application of a vendor into the card strip", async () => {
    const preview = Array.from({ length: 5 }, (_, index) =>
      app({ id: `tool-${index}`, uid: index.toString(16).padStart(32, "0") }),
    );
    const all = Array.from({ length: 7 }, (_, index) =>
      appListItem({ id: `tool-${index}`, uid: index.toString(16).padStart(32, "0") }),
    );
    const appRequests: URLSearchParams[] = [];
    useHandlers(
      mockApi("get", "/admin/api/vendors", () =>
        page([vendorListItem({ apps: preview, app_total: 7 })], { limit: 12 }),
      ),
      mockApi("get", "/admin/api/apps", ({ request }) => {
        const query = new URL(request.url).searchParams;
        appRequests.push(query);
        const pageNumber = Number(query.get("page"));
        // Two pages of 4 (the server may use smaller pages than asked for).
        return {
          ...page(all.slice((pageNumber - 1) * 4, pageNumber * 4), {
            page: pageNumber,
            limit: 4,
            total: 7,
          }),
        };
      }),
    );
    await renderAdminPage("/admin/vendors");
    const strip = await screen.findByRole("region", { name: "Applications of Example" });
    await waitFor(() => {
      expect(within(strip).getAllByRole("link")).toHaveLength(8); // 7 apps + add
    });
    expect(appRequests.map((query) => [query.get("vendor"), query.get("page")])).toEqual([
      ["example", "1"],
      ["example", "2"],
    ]);
    expect(screen.getByRole("link", { name: "7 applications" })).toHaveAttribute(
      "href",
      "/admin/vendors/example/apps",
    );
    expect(within(strip).getByRole("link", { name: "Add application" })).toHaveAttribute(
      "href",
      "/admin/vendors/example/apps/new",
    );
  });
});
