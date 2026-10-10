import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { bootstrap, home, localized, publicApp } from "@/test/factories";
import {
  catalogApp,
  hostedCapabilities,
  hostedFile,
  hostedFilePage,
  httpCacheCapabilities,
} from "@/test/factories/catalog";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderEntry } from "@/test/render";

const banner = () => screen.getByRole("banner");

/** The card titled `name` (cards are sections headed by their title). */
async function card(name: string): Promise<HTMLElement> {
  const heading = await screen.findByRole("heading", { level: 2, name });
  const section = heading.closest("section");
  if (!section) throw new Error(`no card for ${name}`);
  return section;
}

function appServer(app: Schema<"PublicApp"> | Response) {
  useHandlers(
    mockApi("get", "/api/bootstrap", () => bootstrap()),
    mockApi("get", "/api/home", () => home()),
    mockApi("get", "/api/apps/{vendor}/{app}", () => app),
  );
}

describe("application page", () => {
  it("shows the application with breadcrumbs, version and instructions", async () => {
    appServer(
      publicApp({
        latest_known_version: {
          version: "0.46.0",
          first_seen: new Date(Date.now() - 3 * 86_400_000).toISOString(),
        },
        categories: [{ id: "tools", name: localized("Tools") }],
      }),
    );
    await renderEntry("public", "/openai/codex");
    expect(await screen.findByRole("heading", { level: 1, name: "Codex CLI" })).toBeInTheDocument();
    expect(screen.getByText("OpenAI’s coding agent for your terminal.")).toBeInTheDocument();

    const crumbs = screen.getByRole("navigation", { name: "Breadcrumbs" });
    expect(within(crumbs).getByRole("link", { name: "All applications" })).toHaveAttribute(
      "href",
      "/all",
    );
    expect(within(crumbs).getByRole("link", { name: "OpenAI" })).toHaveAttribute("href", "/openai");
    expect(within(crumbs).getByText("Codex CLI")).toHaveAttribute("aria-current", "page");

    expect(screen.getByText("0.46.0")).toBeInTheDocument();
    expect(screen.getByText("3 days ago")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Tools" })).toHaveAttribute(
      "href",
      "/all?category=tools",
    );

    const frame = screen.getByTitle("Usage instructions");
    expect(frame).toHaveAttribute("src", "/api/apps/openai/codex/instructions/document?lang=en");
    // Installer apps have no download prefix.
    expect(screen.queryByText("Download URL prefix")).not.toBeInTheDocument();

    await waitFor(() => {
      expect(document.title).toBe("Codex CLI · RedApp Mirror");
    });
    // The header's admin link opens this application's main admin tab.
    expect(within(banner()).getByRole("link", { name: "Administration" })).toHaveAttribute(
      "href",
      "/admin/vendors/openai/apps/codex/versions",
    );
  });

  it("shows instructions only when they exist in the current language", async () => {
    appServer(publicApp({ instructions_available: { en: false, "zh-CN": true } }));
    await renderEntry("public", "/openai/codex");
    await screen.findByRole("heading", { level: 1, name: "Codex CLI" });
    expect(screen.queryByTitle("Usage instructions")).not.toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Language" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "简体中文" }));
    expect(await screen.findByTitle("使用说明")).toHaveAttribute(
      "src",
      "/api/apps/openai/codex/instructions/document?lang=zh-CN",
    );
  });

  it("shows the download prefix of a file-serving app without versions", async () => {
    appServer(
      catalogApp("acme/mirror", {
        provider: "http-cache",
        capabilities: httpCacheCapabilities,
        latest_known_version: null,
        instructions_available: { en: false, "zh-CN": false },
      }),
    );
    await renderEntry("public", "/acme/mirror");
    const prefix = await card("Download URL prefix");
    expect(
      await within(prefix).findByText("https://mirror.example.internal/acme/mirror/"),
    ).toBeInTheDocument();
    expect(screen.queryByText("No version seen yet")).not.toBeInTheDocument();
    expect(within(banner()).getByRole("link", { name: "Administration" })).toHaveAttribute(
      "href",
      "/admin/vendors/acme/apps/mirror/cache",
    );
  });

  it("lists hosted downloads 25 per page", async () => {
    appServer(
      catalogApp("acme/tools", {
        provider: "hosted",
        capabilities: hostedCapabilities,
        latest_known_version: null,
      }),
    );
    const pages: string[] = [];
    useHandlers(
      mockApi("get", "/api/apps/{vendor}/{app}/files", ({ request }) => {
        const query = new URL(request.url).searchParams;
        const page = Number(query.get("page"));
        pages.push(`${query.get("page")}/${query.get("limit")}`);
        const items =
          page === 1
            ? Array.from({ length: 25 }, (_, index) => hostedFile(`bin/tool ${index + 1}.exe`))
            : [hostedFile("zz/last file.zip", { size_bytes: 2048 })];
        return hostedFilePage({ items, page, total: 26 });
      }),
    );
    await renderEntry("public", "/acme/tools");
    const downloads = await card("Downloads");
    expect(await within(downloads).findByRole("link", { name: "bin/tool 1.exe" })).toHaveAttribute(
      "href",
      "/acme/tools/bin/tool%201.exe",
    );
    expect(within(downloads).getByText("26 files")).toBeInTheDocument();
    expect(screen.queryByText("Download URL prefix")).not.toBeInTheDocument();

    await userEvent.setup().click(within(downloads).getByRole("button", { name: "Page 2" }));
    const last = await within(downloads).findByRole("link", { name: "zz/last file.zip" });
    expect(last).toHaveAttribute("href", "/acme/tools/zz/last%20file.zip");
    expect(last).toHaveAttribute("download");
    expect(within(downloads).getByText("2.00 KiB")).toBeInTheDocument();
    expect(pages).toEqual(["1/25", "2/25"]);
    expect(within(banner()).getByRole("link", { name: "Administration" })).toHaveAttribute(
      "href",
      "/admin/vendors/acme/apps/tools/files",
    );
  });

  it("explains an empty hosted application", async () => {
    appServer(catalogApp("acme/tools", { provider: "hosted", capabilities: hostedCapabilities }));
    useHandlers(
      mockApi("get", "/api/apps/{vendor}/{app}/files", () =>
        hostedFilePage({ items: [], total: 0 }),
      ),
    );
    await renderEntry("public", "/acme/tools");
    expect(await screen.findByText("No files have been published yet.")).toBeInTheDocument();
  });

  it("shows the not-found page for an unknown or unpublished application", async () => {
    appServer(apiError("APPLICATION_NOT_FOUND"));
    await renderEntry("public", "/openai/missing");
    expect(
      await screen.findByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(document.title).toBe("Page not found · RedApp Mirror");
    });
    expect(within(banner()).getByRole("link", { name: "Administration" })).toHaveAttribute(
      "href",
      "/admin/overview",
    );
  });

  it("reports a failed load and retries", async () => {
    let fail = true;
    useHandlers(
      mockApi("get", "/api/bootstrap", () => bootstrap()),
      mockApi("get", "/api/home", () => home()),
      mockApi("get", "/api/apps/{vendor}/{app}", () =>
        fail ? apiError("STORAGE_UNAVAILABLE") : publicApp(),
      ),
    );
    await renderEntry("public", "/openai/codex");
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Server storage is unavailable.");
    fail = false;
    await userEvent.setup().click(within(alert).getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Codex CLI" })).toBeInTheDocument();
  });
});
