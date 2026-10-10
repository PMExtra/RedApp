import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { RetentionPanel } from "@/features/retention";
import type { Schema } from "@/shared/api";
import {
  appConfiguration,
  retentionPreview,
  retentionStatus,
  retentionVersionPage,
} from "@/test/factories/runtime";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderAppPage } from "@/test/appPage";

const props = { vendor: "openai", app: "codex" };

function baseHandlers(configuration = appConfiguration()) {
  return [
    mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => configuration),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/retention/status", () => retentionStatus()),
  ];
}

describe("retention", () => {
  it("asks before enabling automatic deletion and saves the policy", async () => {
    let patch: { ifMatch: string | null; body: unknown } | undefined;
    useHandlers(
      ...baseHandlers(),
      mockApi("patch", "/admin/api/apps/{vendor}/{app}/configuration", async ({ request }) => {
        const body = (await request.json()) as Schema<"AppConfigurationPatch">;
        patch = { ifMatch: request.headers.get("If-Match"), body };
        const saved = appConfiguration({ revision: 8 });
        saved.effective.retention = body.set?.retention ?? saved.effective.retention;
        return saved;
      }),
    );
    await renderAppPage(RetentionPanel, { props });
    const user = userEvent.setup();
    const toggle = await screen.findByRole("switch", {
      name: "Delete old cached versions automatically",
    });
    expect(await screen.findByText("Succeeded")).toBeInTheDocument();

    // Declining keeps it off.
    await user.click(toggle);
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Cancel" }),
    );
    expect(toggle).toHaveAttribute("aria-checked", "false");
    expect(screen.getByRole("button", { name: "Save retention" })).toBeDisabled();

    await user.click(toggle);
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Turn on" }),
    );
    expect(toggle).toHaveAttribute("aria-checked", "true");
    // Unsaved changes block a manual run with the old policy.
    expect(screen.getByRole("button", { name: "Preview run" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Save retention" }));
    expect(await screen.findByText("Retention policy saved.")).toBeInTheDocument();
    expect(patch).toEqual({
      ifMatch: '"7"',
      body: { set: { retention: { enabled: true, keep_latest: 3 } } },
    });
    expect(screen.getByRole("button", { name: "Preview run" })).toBeEnabled();
  });

  it("previews with the saved revision, pages the versions and executes", async () => {
    let ifMatch: string | null = null;
    const pages: string[] = [];
    useHandlers(
      ...baseHandlers(),
      mockApi("post", "/admin/api/apps/{vendor}/{app}/retention/preview", ({ request }) => {
        ifMatch = request.headers.get("If-Match");
        return retentionPreview();
      }),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/retention/{preview_id}", () =>
        retentionPreview(),
      ),
      mockApi(
        "get",
        "/admin/api/apps/{vendor}/{app}/retention/{preview_id}/items",
        ({ request }) => {
          const page = new URL(request.url).searchParams.get("page") ?? "1";
          pages.push(page);
          return page === "2"
            ? retentionVersionPage(
                [{ version: "0.40.0", reasons: ["outside_latest_n"], bytes: 1024, selected: true }],
                { page: 2, total: 26, total_pages: 2 },
              )
            : retentionVersionPage(
                [
                  {
                    version: "0.46.0",
                    reasons: ["latest_n", "channel"],
                    bytes: 1024,
                    selected: false,
                  },
                ],
                { total: 26, total_pages: 2 },
              );
        },
      ),
      mockApi(
        "post",
        "/admin/api/apps/{vendor}/{app}/retention/{preview_id}/execute",
        () =>
          retentionPreview({
            executed_at: new Date().toISOString(),
            result: {
              retired_versions: 1,
              logical_bytes: 1024,
              skipped: { "0.41.0": "in_use" },
              selection: [],
            },
          }),
        { status: 200 },
      ),
    );
    await renderAppPage(RetentionPanel, { props });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Preview run" }));

    const review = await screen.findByRole("region", { name: "Retention preview" });
    expect(ifMatch).toBe('"7"');
    expect(
      await within(review).findByText("Among the newest, A channel points to it"),
    ).toBeVisible();
    await user.click(within(review).getByRole("button", { name: /Next page/ }));
    expect(await within(review).findByText("0.40.0")).toBeInTheDocument();
    expect(within(review).getByText("Delete")).toBeInTheDocument();
    expect(pages).toEqual(["1", "2"]);

    await user.click(within(review).getByRole("button", { name: "Delete selected versions" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Delete selected versions",
      }),
    );
    expect(await within(review).findByText("Run finished")).toBeInTheDocument();
    expect(within(review).getByText("0.41.0 kept: In use")).toBeInTheDocument();
    expect(within(review).queryByRole("button", { name: "Delete selected versions" })).toBeNull();
  });

  it("reports a stale preview without deleting and keeps the policy intact", async () => {
    useHandlers(
      ...baseHandlers(),
      mockApi("post", "/admin/api/apps/{vendor}/{app}/retention/preview", () => retentionPreview()),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/retention/{preview_id}", () =>
        retentionPreview(),
      ),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/retention/{preview_id}/items", () =>
        retentionVersionPage([]),
      ),
      mockApi("post", "/admin/api/apps/{vendor}/{app}/retention/{preview_id}/execute", () =>
        apiError("PREVIEW_STALE"),
      ),
    );
    await renderAppPage(RetentionPanel, { props });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Preview run" }));
    const review = await screen.findByRole("region", { name: "Retention preview" });
    await user.click(within(review).getByRole("button", { name: "Delete selected versions" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Delete selected versions",
      }),
    );
    expect(await screen.findByText("Preview no longer valid")).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Retention preview" })).toBeNull();
    });
  });

  it("asks to review again when the settings changed before the preview", async () => {
    let revision = 7;
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () =>
        appConfiguration({ revision }),
      ),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/retention/status", () =>
        retentionStatus({ last_run: null, next_check_at: null }),
      ),
      mockApi("post", "/admin/api/apps/{vendor}/{app}/retention/preview", () => {
        revision = 9;
        return apiError("REVISION_CONFLICT");
      }),
    );
    await renderAppPage(RetentionPanel, { props });
    const user = userEvent.setup();
    expect(await screen.findByText("No automatic run yet.")).toBeInTheDocument();
    expect(screen.getByText("Automatic checks are not scheduled.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Preview run" }));
    expect(await screen.findByText(/The settings changed since you loaded them/)).toBeVisible();
  });
});
