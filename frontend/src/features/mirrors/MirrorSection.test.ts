import { screen, waitFor, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import { recorder, renderAdminPage } from "@/test/directory";
import { category, page, vendor } from "@/test/factories/directory";
import {
  appMirror,
  mirroredApp,
  mirroredConfiguration,
  UPSTREAM_UID,
} from "@/test/factories/mirrors";
import { httpCacheConfiguration } from "@/test/factories/runtime";
import { mockApi, useHandlers } from "@/test/msw";

const SETTINGS = "/admin/vendors/mirror/apps/files/settings";

function mirrorServer() {
  let bound = true;
  const unbinds = recorder();
  const syncs = recorder();
  useHandlers(
    mockApi("get", "/admin/api/vendors/{vendor}", () => vendor({ id: "mirror" })),
    mockApi("get", "/admin/api/apps/{vendor}/{app}", () => mirroredApp()),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () =>
      bound ? mirroredConfiguration() : httpCacheConfiguration({}, { revision: 10 }),
    ),
    mockApi("get", "/admin/api/categories", () => page([category()], { limit: 100 })),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/mirror", () => appMirror()),
    mockApi("post", "/admin/api/apps/{vendor}/{app}/mirror/sync", async ({ request }) => {
      await syncs.record(request);
      return appMirror({ available: false, error: "UPSTREAM_UNAVAILABLE" });
    }),
    mockApi(
      "delete",
      "/admin/api/apps/{vendor}/{app}/mirror",
      async ({ request }) => {
        await unbinds.record(request);
        bound = false;
        return mirroredApp({ revision: 10 });
      },
      { status: 200 },
    ),
  );
  return { syncs, unbinds };
}

function section(title: string): HTMLElement {
  const element = screen.getByRole("heading", { name: title }).closest<HTMLElement>("section");
  if (!element) throw new Error(`no section ${title}`);
  return element;
}

describe("mirror source", () => {
  it("shows the upstream, checks it now and stops mirroring", async () => {
    const { syncs, unbinds } = mirrorServer();
    const { user } = await renderAdminPage(SETTINGS);
    await screen.findByRole("heading", { name: "Mirror source" });
    const mirror = section("Mirror source");
    expect(await within(mirror).findByRole("link", { name: "DMZ" })).toHaveAttribute(
      "href",
      `/admin/upstreams/${UPSTREAM_UID}`,
    );
    expect(within(mirror).getByText("acme/files")).toBeInTheDocument();
    expect(within(mirror).getByText("Published")).toBeInTheDocument();
    expect(within(mirror).getByText("None")).toBeInTheDocument();

    await user.click(within(mirror).getByRole("button", { name: "Check now" }));
    expect(await screen.findByText("Checked the upstream.")).toBeInTheDocument();
    expect(syncs.calls).toHaveLength(1);
    expect(
      within(mirror).getByText("The upstream server is unavailable. Try again later."),
    ).toBeInTheDocument();
    expect(within(mirror).getByText("Upstream no longer publishes it")).toBeInTheDocument();

    await user.click(within(mirror).getByRole("button", { name: "Stop mirroring" }));
    const confirm = await screen.findByRole("alertdialog", {
      name: "Stop mirroring mirror/files?",
    });
    expect(within(confirm).getByText(/keeps its current name/)).toBeInTheDocument();
    await user.click(within(confirm).getByRole("button", { name: "Stop mirroring" }));
    expect(
      await screen.findByText("mirror/files no longer follows its upstream."),
    ).toBeInTheDocument();
    expect(unbinds.calls[0]?.ifMatch).toBe('"9"');
    await waitFor(() => {
      expect(screen.queryByRole("heading", { name: "Mirror source" })).toBeNull();
    });
    // An ordinary application edits its source again.
    expect(screen.getByRole("radio", { name: "Round robin" })).toBeEnabled();
  });

  it("keeps the source read-only and lets display fields follow the upstream", async () => {
    mirrorServer();
    await renderAdminPage(SETTINGS);
    await screen.findByRole("heading", { name: "Mirror source" });
    const source = section("Upstream");
    expect(
      await within(source).findByText(/The upstream RedApp provides the source/),
    ).toBeInTheDocument();
    expect(within(source).getByRole("radio", { name: "Round robin" })).toBeDisabled();
    expect(within(source).getByRole("textbox", { name: "Source URL 1" })).toBeDisabled();
    expect(within(source).getByRole("spinbutton", { name: /Default freshness/ })).toBeEnabled();
    expect(within(source).queryByRole("button", { name: /Restore|Follow/ })).toBeNull();

    const details = section("Details");
    expect(within(details).getByText(/Mirrored application: the name/)).toBeInTheDocument();
    expect(within(details).queryByText("Template provider")).toBeNull();
    expect(
      within(details).getByRole("button", { name: "Follow the upstream value: Shared files" }),
    ).toBeInTheDocument();
  });
});
