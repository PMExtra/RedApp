import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { bootstrap, home, publicApp, searchHit, localized } from "@/test/factories";
import { catalogPage } from "@/test/factories/catalog";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderEntry } from "@/test/render";

function publicServer() {
  const searches: string[] = [];
  useHandlers(
    mockApi("get", "/api/bootstrap", () => bootstrap({ version: "1.4.0" })),
    mockApi("get", "/api/home", () =>
      home({ ranking: [{ app: publicApp(), download_clients: 3 }] }),
    ),
    mockApi("get", "/api/catalog", () => catalogPage()),
    mockApi("get", "/api/search", ({ request }) => {
      const q = new URL(request.url).searchParams.get("q") ?? "";
      searches.push(q);
      return {
        items: [
          searchHit({
            kind: "vendor",
            key: "anthropic",
            name: localized("Anthropic"),
            localized_icons: { en: "", "zh-CN": "" },
          }),
          searchHit({ key: "anthropic/claude-code", name: localized("Claude Code") }),
        ],
      };
    }),
  );
  return { searches };
}

describe("public shell", () => {
  it("renders ordered full-page links with safe new-tab behavior", async () => {
    publicServer();
    useHandlers(
      mockApi("get", "/api/bootstrap", () =>
        bootstrap({
          site: {
            ...bootstrap().site,
            navigation: [
              { title: "Archive", url: "/legacy/", new_tab: false },
              { title: "Docs", url: "https://example.com/", new_tab: true },
            ],
          },
        }),
      ),
    );
    const { router } = await renderEntry("public", "/");
    const nav = await screen.findByRole("navigation", { name: "Custom navigation" });
    const links = within(nav).getAllByRole("link");
    expect(links.map((link) => link.getAttribute("href"))).toEqual([
      "/legacy/",
      "https://example.com/",
    ]);
    expect(links[0]).not.toHaveAttribute("target");
    expect(links[1]).toHaveAttribute("target", "_blank");
    expect(links[1]).toHaveAttribute("rel", "noopener noreferrer");
    const event = new MouseEvent("click", { bubbles: true, cancelable: true });
    let intercepted = true;
    document.addEventListener(
      "click",
      (click) => {
        intercepted = click.defaultPrevented;
        click.preventDefault(); // Stop the test DOM from actually fetching the legacy URL.
      },
      { once: true },
    );
    const archive = links[0];
    if (!archive) throw new Error("missing archive link");
    archive.dispatchEvent(event);
    expect(intercepted).toBe(false);
    expect(router.currentRoute.value.path).toBe("/");
  });

  it("shows site texts and the version", async () => {
    publicServer();
    await renderEntry("public", "/");
    expect(screen.queryByRole("navigation", { name: "Custom navigation" })).toBeNull();
    const banner = screen.getByRole("banner");
    expect(await within(banner).findByText("RedApp Mirror")).toBeInTheDocument();
    const footer = screen.getByRole("contentinfo");
    expect(await within(footer).findByText("v1.4.0")).toBeInTheDocument();
    expect(within(footer).getByText("For internal use only.")).toBeInTheDocument();
    expect(within(banner).getByRole("link", { name: "Administration" })).toHaveAttribute(
      "href",
      "/admin/overview",
    );
    await waitFor(() => {
      expect(document.title).toBe("RedApp Mirror");
    });
  });

  it("suggests popular apps, searches as you type and opens a suggestion", async () => {
    const { searches } = publicServer();
    const { router } = await renderEntry("public", "/");
    const user = userEvent.setup();
    const box = screen.getByRole("combobox", { name: "Search applications and vendors" });
    // Focusing the empty box shows the popular applications.
    await user.click(box);
    expect(await screen.findByRole("option", { name: /Codex CLI/ })).toBeInTheDocument();

    await user.type(box, "claude");
    expect(await screen.findByRole("option", { name: /Claude Code/ })).toBeInTheDocument();
    expect(searches.at(-1)).toBe("claude");
    await user.click(screen.getByRole("option", { name: /Claude Code/ }));
    await waitFor(() => {
      expect(router.currentRoute.value.path).toBe("/anthropic/claude-code");
    });
  });

  it("opens the catalog search on Enter, never the first suggestion implicitly", async () => {
    publicServer();
    const { router } = await renderEntry("public", "/");
    const user = userEvent.setup();
    const box = screen.getByRole("combobox", { name: "Search applications and vendors" });
    await user.type(box, "claude");
    expect(await screen.findByRole("option", { name: /Claude Code/ })).toBeInTheDocument();
    await user.keyboard("{Enter}");
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe("/all?q=claude");
    });
  });

  it("moves through suggestions with the arrow keys and opens the chosen one", async () => {
    publicServer();
    const { router } = await renderEntry("public", "/");
    const user = userEvent.setup();
    const box = screen.getByRole("combobox", { name: "Search applications and vendors" });
    await user.type(box, "claude");
    expect(await screen.findByRole("option", { name: /Claude Code/ })).toBeInTheDocument();
    // The first arrow starts at the first suggestion; the next one moves on.
    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("option", { name: /Anthropic/ })).toHaveAttribute("data-highlighted");
    await user.keyboard("{ArrowDown}{Enter}");
    await waitFor(() => {
      expect(router.currentRoute.value.path).toBe("/anthropic/claude-code");
    });
  });

  it("opens the catalog search on Enter with the list closed", async () => {
    publicServer();
    const { router } = await renderEntry("public", "/");
    const user = userEvent.setup();
    const box = screen.getByRole("combobox", { name: "Search applications and vendors" });
    await user.type(box, "中文");
    await user.keyboard("{Escape}{Enter}");
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe("/all?q=%E4%B8%AD%E6%96%87");
    });
  });

  it("switches the language and remembers it", async () => {
    publicServer();
    await renderEntry("public", "/");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Language" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "简体中文" }));
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(localStorage.getItem("redapp-language")).toBe("zh-CN");
    expect(await screen.findByRole("link", { name: "全部应用" })).toBeInTheDocument();
  });

  it("warns when site information cannot be loaded", async () => {
    publicServer();
    useHandlers(mockApi("get", "/api/bootstrap", () => apiError("STORAGE_UNAVAILABLE")));
    await renderEntry("public", "/all");
    expect(await screen.findByText("Site information is unavailable.")).toBeInTheDocument();
  });

  it("shows the not-found page for invalid paths", async () => {
    publicServer();
    await renderEntry("public", "/Not_A_Vendor");
    expect(
      await screen.findByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeInTheDocument();
  });
});
