import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { bootstrap, home, localized, publicVendor } from "@/test/factories";
import { catalogApp, catalogPage, categoryCount } from "@/test/factories/catalog";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderEntry } from "@/test/render";

interface CatalogRequest {
  q: string | null;
  vendor: string | null;
  category: string | null;
  page: string | null;
  limit: string | null;
}

const categories = [categoryCount("tools", 3), categoryCount("editors", 1)];

/** Serves a catalog of `total` apps (24 per page) and records each request. */
function catalogServer(
  options: { total?: number; respond?: (request: CatalogRequest) => Schema<"CatalogPage"> } = {},
) {
  const requests: CatalogRequest[] = [];
  useHandlers(
    mockApi("get", "/api/bootstrap", () => bootstrap()),
    mockApi("get", "/api/home", () => home()),
    // The header search mirrors `?q=` and searches too.
    mockApi("get", "/api/search", () => ({ items: [] })),
    mockApi("get", "/api/catalog", ({ request }) => {
      const params = new URL(request.url).searchParams;
      const entry: CatalogRequest = {
        q: params.get("q"),
        vendor: params.get("vendor"),
        category: params.get("category"),
        page: params.get("page"),
        limit: params.get("limit"),
      };
      requests.push(entry);
      if (options.respond) return options.respond(entry);
      const page = Number(entry.page ?? "1");
      const total = options.total ?? 2;
      const count = Math.max(0, Math.min(24, total - (page - 1) * 24));
      const items = Array.from({ length: count }, (_, index) =>
        catalogApp(`acme/app-${(page - 1) * 24 + index + 1}`),
      );
      return catalogPage({ items, page, total, categories });
    }),
  );
  return { requests, last: () => requests.at(-1) };
}

const searchBox = () => screen.getByRole("searchbox", { name: "Search applications" });

describe("catalog page", () => {
  it("lists published applications with site-wide category counts", async () => {
    const { last } = catalogServer({ total: 2 });
    await renderEntry("public", "/all");
    expect(await screen.findByRole("link", { name: "App app-1" })).toHaveAttribute(
      "href",
      "/acme/app-1",
    );
    expect(screen.getByRole("heading", { level: 1, name: "All applications" })).toBeInTheDocument();
    expect(screen.getByText("2 applications")).toBeInTheDocument();
    expect(last()).toMatchObject({ page: "1", limit: "24", vendor: null, q: null });

    const filter = screen.getByRole("navigation", { name: "Categories" });
    expect(within(filter).getByRole("link", { name: "All" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(within(filter).getByRole("link", { name: "Tools (3)" })).toHaveAttribute(
      "href",
      "/all?category=tools",
    );
    // No pagination for a single page.
    expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
  });

  it("searches 250 ms after typing stops and replaces the URL", async () => {
    const { requests } = catalogServer();
    const { router } = await renderEntry("public", "/");
    await router.push("/all");
    await screen.findByRole("link", { name: "App app-1" });
    const user = userEvent.setup();

    await user.type(searchBox(), "codex");
    // Nothing is sent while typing.
    expect(router.currentRoute.value.query.q).toBeUndefined();
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe("/all?q=codex");
    });
    await waitFor(() => {
      expect(requests.at(-1)?.q).toBe("codex");
    });
    expect(requests.map((request) => request.q).filter(Boolean)).toEqual(["codex"]);

    // Replaced, not pushed: Back leaves the catalog.
    router.back();
    await waitFor(() => {
      expect(router.currentRoute.value.path).toBe("/");
    });
  });

  it("explains an empty search and clears it", async () => {
    catalogServer({
      respond: (request) =>
        request.q ? catalogPage({ items: [], total: 0, categories }) : catalogPage({ categories }),
    });
    const { router } = await renderEntry("public", "/all?q=nothing");
    expect(await screen.findByText("No applications match “nothing”.")).toBeInTheDocument();
    expect(searchBox()).toHaveValue("nothing");
    await userEvent.setup().click(screen.getByRole("button", { name: "Clear search" }));
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe("/all");
    });
    expect(await screen.findByRole("link", { name: /Codex CLI/ })).toBeInTheDocument();
    expect(searchBox()).toHaveValue("");
  });

  it("pages through results and jumps to a page", async () => {
    const { last } = catalogServer({ total: 24 * 9 + 5 });
    const { router } = await renderEntry("public", "/all?q=app");
    await screen.findByRole("link", { name: "App app-1" });
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Page 2" }));
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe("/all?q=app&page=2");
    });
    expect(await screen.findByRole("link", { name: "App app-25" })).toBeInTheDocument();
    expect(last()).toMatchObject({ q: "app", page: "2" });

    const jump = screen.getByRole("textbox", { name: "Go to page" });
    await user.type(jump, "11");
    await user.click(screen.getByRole("button", { name: "Go" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Enter a page from 1 to 10.");
    expect(jump).toHaveAttribute("aria-invalid", "true");

    await user.clear(jump);
    await user.type(jump, "10{Enter}");
    await waitFor(() => {
      expect(router.currentRoute.value.query.page).toBe("10");
    });
    expect(await screen.findByRole("link", { name: "App app-221" })).toBeInTheDocument();
  });

  it("restores filters and page with the browser's Back button", async () => {
    const { last } = catalogServer({ total: 30 });
    const { router } = await renderEntry("public", "/all?q=app");
    await screen.findByRole("link", { name: "App app-1" });
    const user = userEvent.setup();

    await user.click(screen.getByRole("link", { name: "Tools (3)" }));
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe("/all?q=app&category=tools");
    });
    await user.click(await screen.findByRole("button", { name: "Page 2" }));
    await waitFor(() => {
      expect(last()).toMatchObject({ category: "tools", page: "2" });
    });

    expect(await screen.findByRole("link", { name: "App app-25" })).toBeInTheDocument();

    // Back restores page 1 of the category, then the unfiltered search.
    router.back();
    expect(await screen.findByRole("link", { name: "App app-1" })).toBeInTheDocument();
    expect(router.currentRoute.value.fullPath).toBe("/all?q=app&category=tools");
    expect(screen.getByRole("link", { name: "Tools (3)" })).toHaveAttribute("aria-current", "page");
    expect(searchBox()).toHaveValue("app");

    router.back();
    await waitFor(() => {
      expect(screen.getByRole("link", { name: "All" })).toHaveAttribute("aria-current", "page");
    });
    expect(router.currentRoute.value.fullPath).toBe("/all?q=app");
    expect(searchBox()).toHaveValue("app");
  });

  it("offers the first page when a page is past the end", async () => {
    catalogServer({ total: 3 });
    const { router } = await renderEntry("public", "/all?page=4");
    expect(await screen.findByText("This page is empty.")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Go to the first page" }));
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe("/all");
    });
    expect(await screen.findByRole("link", { name: "App app-1" })).toBeInTheDocument();
  });

  it("reports a failed load and retries", async () => {
    let fail = true;
    catalogServer();
    useHandlers(
      mockApi("get", "/api/catalog", () =>
        fail ? apiError("STORAGE_UNAVAILABLE") : catalogPage({ categories }),
      ),
    );
    await renderEntry("public", "/all");
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Server storage is unavailable.");
    fail = false;
    await userEvent.setup().click(within(alert).getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("link", { name: /Codex CLI/ })).toBeInTheDocument();
  });
});

describe("vendor catalog", () => {
  function vendorServer(delays: Record<string, Promise<void>> = {}) {
    const anthropic = publicVendor({
      id: "anthropic",
      name: localized("Anthropic"),
      description: localized("Makers of Claude."),
      icon: "/assets/presets/anthropic/icon.svg",
      localized_icons: { en: "", "zh-CN": "/assets/presets/anthropic/logo-zh.svg" },
    });
    const vendors: Record<string, Schema<"PublicVendor">> = { openai: publicVendor(), anthropic };
    const server = catalogServer({
      respond: (request) =>
        catalogPage({
          items: [catalogApp(`${request.vendor ?? "acme"}/tool`)],
          categories,
        }),
    });
    useHandlers(
      mockApi("get", "/api/vendors/{vendor}", async ({ params }) => {
        const id = String(params.vendor);
        await delays[id];
        return vendors[id] ?? apiError("VENDOR_NOT_FOUND");
      }),
    );
    return server;
  }

  it("shows the vendor with its localized logo and no category filter", async () => {
    const { last } = vendorServer();
    await renderEntry("public", "/anthropic?category=tools", { locale: "zh-CN" });
    expect(await screen.findByRole("heading", { level: 1, name: "Anthropic" })).toBeInTheDocument();
    expect(screen.getByText("Makers of Claude.")).toBeInTheDocument();
    const breadcrumbs = screen.getByRole("navigation", { name: "面包屑导航" });
    expect(within(breadcrumbs).getByRole("link", { name: "全部应用" })).toHaveAttribute("href", "/all");
    const logo = document.querySelector<HTMLImageElement>("main img[src*='logo-zh']");
    expect(logo).not.toBeNull();
    expect(screen.queryByRole("navigation", { name: "分类" })).not.toBeInTheDocument();
    expect(last()).toMatchObject({ vendor: "anthropic", category: null });
    await waitFor(() => {
      expect(document.title).toBe("Anthropic · RedApp 镜像");
    });
  });

  it("ignores a late response of the previous vendor", async () => {
    let release = () => {};
    const openaiDelay = new Promise<void>((resolve) => {
      release = resolve;
    });
    vendorServer({ openai: openaiDelay });
    const { router } = await renderEntry("public", "/openai");
    await router.push("/anthropic");
    expect(await screen.findByRole("heading", { level: 1, name: "Anthropic" })).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "App tool" })).toHaveAttribute(
      "href",
      "/anthropic/tool",
    );
    release();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.getByRole("heading", { level: 1, name: "Anthropic" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "OpenAI" })).not.toBeInTheDocument();
  });

  it("shows the not-found page for an unknown or unpublished vendor", async () => {
    vendorServer();
    useHandlers(mockApi("get", "/api/catalog", () => apiError("VENDOR_NOT_FOUND")));
    await renderEntry("public", "/missing");
    expect(
      await screen.findByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(document.title).toBe("Page not found · RedApp Mirror");
    });
  });
});
