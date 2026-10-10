import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { RouterView } from "vue-router";
import type { Schema } from "@/shared/api";
import { bootstrap, localized, session, siteSettingsState } from "@/test/factories";
import { appListItem, page } from "@/test/factories/directory";
import { homepageState, pinnedApp, publicUrlState } from "@/test/factories/settings";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderEntry, renderWithApp } from "@/test/render";
import SiteSettingsPage from "./SiteSettingsPage.vue";

interface Put {
  ifMatch: string | null;
  body: unknown;
}

/** A settings server: GETs return the stored state, PUTs store and bump the revision. */
function settingsServer(pins = homepageState()) {
  let site = siteSettingsState();
  let publicUrl = publicUrlState();
  let homepage = pins;
  const puts: Record<"site" | "publicUrl" | "homepage", Put[]> = {
    site: [],
    publicUrl: [],
    homepage: [],
  };
  let conflictNext = false;
  const record = async (list: Put[], request: Request) => {
    const body: unknown = await request.json();
    list.push({ ifMatch: request.headers.get("If-Match"), body });
    return body;
  };
  useHandlers(
    mockApi("get", "/admin/api/settings/site", () => site),
    mockApi("put", "/admin/api/settings/site", async ({ request }) => {
      const body = (await record(puts.site, request)) as Schema<"SiteSettings">;
      if (conflictNext) {
        conflictNext = false;
        return apiError("REVISION_CONFLICT");
      }
      site = { ...body, revision: site.revision + 1 };
      return site;
    }),
    mockApi("get", "/admin/api/settings/public-url", () => publicUrl),
    mockApi("put", "/admin/api/settings/public-url", async ({ request }) => {
      const body = (await record(puts.publicUrl, request)) as Schema<"PublicUrlSettings">;
      publicUrl = {
        ...publicUrl,
        override_url: body.override_url?.replace(/\/$/, "") ?? null,
        effective_url: body.override_url?.replace(/\/$/, "") ?? "http://localhost",
        source: body.override_url ? "override" : "request",
        revision: publicUrl.revision + 1,
      };
      return publicUrl;
    }),
    mockApi("get", "/admin/api/settings/homepage", () => homepage),
    mockApi("put", "/admin/api/settings/homepage", async ({ request }) => {
      const body = (await record(puts.homepage, request)) as Schema<"HomepageSettings">;
      homepage = homepageState(
        body.pinned_app_keys.map(
          (key) =>
            homepage.pinned_apps.find((app) => app.key === key) ??
            pinnedApp(key, key === "google/gemini-cli" ? "Gemini CLI" : key),
        ),
        homepage.revision + 1,
      );
      return homepage;
    }),
    mockApi("get", "/admin/api/apps", ({ request }) => {
      const q = new URL(request.url).searchParams.get("q") ?? "";
      const apps = [
        appListItem({ id: "gemini-cli", vendor_id: "google", name: localized("Gemini CLI") }),
        appListItem({ id: "claude-code", vendor_id: "anthropic", name: localized("Claude Code") }),
      ];
      return page(
        apps.filter((app) => !q || app.key.includes(q)),
        { limit: 8 },
      );
    }),
  );
  return {
    puts,
    conflictOnce: () => (conflictNext = true),
    changeSiteElsewhere: (title: string) => {
      site = { ...site, title: localized(title), revision: site.revision + 1 };
    },
  };
}

function renderPage() {
  return renderWithApp(RouterView, {
    routes: [{ path: "/admin/settings/site", component: SiteSettingsPage }],
    path: "/admin/settings/site",
  });
}

async function siteCard() {
  return (await screen.findByRole("heading", { name: "Site texts" })).closest(
    "section",
  ) as HTMLElement;
}

describe("site texts", () => {
  it("saves with If-Match and refreshes the site title in the shell", async () => {
    const server = settingsServer();
    let bootstraps = 0;
    let title = "RedApp Mirror";
    useHandlers(
      mockApi("get", "/admin/api/session", () => session()),
      mockApi("get", "/api/bootstrap", () => {
        bootstraps++;
        return bootstrap({ site: { ...bootstrap().site, title: localized(title) } });
      }),
    );
    await renderEntry("admin", "/admin/settings/site");
    const card = await siteCard();
    const [english] = await within(card).findAllByLabelText(/^Site title/);
    if (!english) throw new Error("no title field");
    await waitFor(() => {
      expect(english).toHaveValue("RedApp Mirror");
    });
    const before = bootstraps;

    const user = userEvent.setup();
    await user.clear(english);
    await user.type(english, "  Downloads  ");
    title = "Downloads";
    await user.click(within(card).getByRole("button", { name: "Save site texts" }));

    expect(await screen.findByText("Site texts saved.")).toBeVisible();
    expect(server.puts.site).toEqual([
      {
        ifMatch: '"3"',
        body: {
          title: localized("Downloads", "RedApp 镜像"),
          subtitle: localized("Internal application mirror", "内部应用镜像"),
          disclaimer: localized("For internal use only.", "仅供内部使用。"),
        },
      },
    ]);
    await waitFor(() => {
      expect(bootstraps).toBeGreaterThan(before);
    });
    expect(await screen.findByRole("link", { name: /^Downloads/ })).toBeVisible();
  });

  it("validates titles before saving", async () => {
    const server = settingsServer();
    await renderPage();
    const card = await siteCard();
    const [english] = await within(card).findAllByLabelText(/^Site title/);
    if (!english) throw new Error("no title field");
    await waitFor(() => {
      expect(english).toHaveValue("RedApp Mirror");
    });
    const user = userEvent.setup();
    await user.clear(english);
    await user.type(english, "   ");
    await user.click(within(card).getByRole("button", { name: "Save site texts" }));
    expect(await within(card).findByText("Enter a title.")).toBeVisible();
    expect(english).toHaveAttribute("aria-invalid", "true");
    expect(server.puts.site).toEqual([]);
  });

  it("keeps the draft on a revision conflict and reloads the latest version on request", async () => {
    const server = settingsServer();
    await renderPage();
    const card = await siteCard();
    const [english] = await within(card).findAllByLabelText(/^Site title/);
    if (!english) throw new Error("no title field");
    await waitFor(() => {
      expect(english).toHaveValue("RedApp Mirror");
    });
    const user = userEvent.setup();
    await user.clear(english);
    await user.type(english, "My draft");
    server.conflictOnce();
    server.changeSiteElsewhere("Changed elsewhere");
    await user.click(within(card).getByRole("button", { name: "Save site texts" }));

    expect(await within(card).findByText("Changed by someone else")).toBeVisible();
    expect(english).toHaveValue("My draft");
    expect(screen.queryByText("Site texts saved.")).toBeNull();

    await user.click(within(card).getByRole("button", { name: "Reload latest version" }));
    await waitFor(() => {
      expect(english).toHaveValue("Changed elsewhere");
    });
    expect(within(card).queryByText("Changed by someone else")).toBeNull();

    await user.clear(english);
    await user.type(english, "Second try");
    await user.click(within(card).getByRole("button", { name: "Save site texts" }));
    expect(await screen.findByText("Site texts saved.")).toBeVisible();
    expect(server.puts.site.map((put) => put.ifMatch)).toEqual(['"3"', '"4"']);
  });
});

describe("public URL", () => {
  it("shows the effective URL and its source, and sets and clears the override", async () => {
    const server = settingsServer();
    await renderPage();
    const card = (await screen.findByRole("heading", { name: "Public URL" })).closest(
      "section",
    ) as HTMLElement;
    expect(await within(card).findByText("http://localhost")).toBeVisible();
    expect(within(card).getByText("From this request")).toBeVisible();

    const user = userEvent.setup();
    const field = within(card).getByLabelText("Override");
    await user.type(field, "https://user@downloads.example.com/path");
    await user.click(within(card).getByRole("button", { name: "Save public URL" }));
    expect(
      await within(card).findByText(
        "Enter an http or https address without user name, path, query or fragment.",
      ),
    ).toBeVisible();
    expect(server.puts.publicUrl).toEqual([]);

    await user.clear(field);
    await user.type(field, "https://downloads.example.com/");
    await user.click(within(card).getByRole("button", { name: "Save public URL" }));
    expect(await screen.findByText("Public URL saved.")).toBeVisible();
    expect(within(card).getByText("From the override")).toBeVisible();
    // The draft follows the saved (normalized) value.
    await waitFor(() => {
      expect(field).toHaveValue("https://downloads.example.com");
    });

    await user.click(within(card).getByRole("button", { name: "Clear override" }));
    await user.click(within(card).getByRole("button", { name: "Save public URL" }));
    expect(await within(card).findByText("From this request")).toBeVisible();
    expect(server.puts.publicUrl).toEqual([
      { ifMatch: '"2"', body: { override_url: "https://downloads.example.com/" } },
      { ifMatch: '"3"', body: { override_url: null } },
    ]);
  });
});

describe("homepage pins", () => {
  it("reorders, adds and removes pinned applications", async () => {
    const server = settingsServer();
    await renderPage();
    const card = (await screen.findByRole("heading", { name: "Pinned applications" })).closest(
      "section",
    ) as HTMLElement;
    const user = userEvent.setup();

    // Keyboard reordering through the drag handle.
    const handle = await within(card).findByRole("button", {
      name: "Reorder Claude Code (anthropic/claude-code)",
    });
    handle.focus();
    await user.keyboard("{ArrowUp}");
    const list = within(card).getByRole("group", { name: "Pinned applications in display order" });
    expect(
      within(list)
        .getAllByRole("button", { name: /^Remove / })
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual(["Remove Claude Code (anthropic/claude-code)", "Remove Codex (openai/codex)"]);

    await user.click(within(card).getByRole("button", { name: "Remove Codex (openai/codex)" }));
    expect(within(list).queryByText("openai/codex")).toBeNull();

    // Add through the application search; already pinned ones are disabled.
    await user.type(within(card).getByRole("combobox", { name: "Add an application" }), "o");
    const claude = await screen.findByRole("option", { name: /Claude Code/ });
    expect(claude).toHaveTextContent("Pinned");
    await user.click(await screen.findByRole("option", { name: /Gemini CLI/ }));
    expect(await within(list).findByText("google/gemini-cli")).toBeVisible();

    await user.click(within(card).getByRole("button", { name: "Save pinned applications" }));
    expect(await screen.findByText("Pinned applications saved.")).toBeVisible();
    expect(server.puts.homepage).toEqual([
      { ifMatch: '"5"', body: { pinned_app_keys: ["anthropic/claude-code", "google/gemini-cli"] } },
    ]);
  });

  it("names saved pins and flags those hidden from the public", async () => {
    settingsServer(
      homepageState([
        pinnedApp("openai/codex", "Codex"),
        pinnedApp("google/gemini-cli", "Gemini CLI", { state: "disabled" }),
        pinnedApp("acme/tool", "Tool", { state: "deleted" }),
        { key: "acme/gone", name: null, icon: null, state: "missing" },
      ]),
    );
    await renderPage();
    const list = await screen.findByRole("group", {
      name: "Pinned applications in display order",
    });
    const rows = within(list).getAllByRole("listitem");
    expect(rows.map((row) => row.textContent.replace(/\s+/g, " ").trim())).toEqual([
      expect.stringMatching(/1 ?Codex ?openai\/codex$/),
      expect.stringMatching(/Gemini CLI ?google\/gemini-cli ?Disabled, hidden$/),
      expect.stringMatching(/Tool ?acme\/tool ?Deleted, hidden$/),
      expect.stringMatching(/acme\/gone ?No longer exists$/),
    ]);
    expect(within(list).getByRole("button", { name: "Remove acme/gone" })).toBeVisible();
  });
});
