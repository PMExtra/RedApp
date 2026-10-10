import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { bootstrap, home, publicApp, searchHit, localized } from "@/test/factories";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderEntry } from "@/test/render";

function publicServer() {
  const searches: string[] = [];
  useHandlers(
    mockApi("get", "/api/bootstrap", () => bootstrap({ version: "1.4.0" })),
    mockApi("get", "/api/home", () => home({ ranking: [{ app: publicApp(), download_clients: 3 }] })),
    mockApi("get", "/api/search", ({ request }) => {
      const q = new URL(request.url).searchParams.get("q") ?? "";
      searches.push(q);
      return {
        items: [
          searchHit({ kind: "vendor", key: "anthropic", name: localized("Anthropic"), localized_icons: { en: "", "zh-CN": "" } }),
          searchHit({ key: "anthropic/claude-code", name: localized("Claude Code") }),
        ],
      };
    }),
  );
  return { searches };
}

describe("public shell", () => {
  it("shows site texts and the version", async () => {
    publicServer();
    await renderEntry("public", "/");
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
    await user.click(box);
    await user.keyboard("{ArrowDown}");
    expect(await screen.findByRole("option", { name: /Codex CLI/ })).toBeInTheDocument();

    await user.type(box, "claude");
    expect(await screen.findByRole("option", { name: /Claude Code/ })).toBeInTheDocument();
    expect(searches.at(-1)).toBe("claude");
    await user.click(screen.getByRole("option", { name: /Claude Code/ }));
    await waitFor(() => {
      expect(router.currentRoute.value.path).toBe("/anthropic/claude-code");
    });
  });

  it("opens the catalog search on Enter without a highlighted suggestion", async () => {
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
    expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeInTheDocument();
  });
});
