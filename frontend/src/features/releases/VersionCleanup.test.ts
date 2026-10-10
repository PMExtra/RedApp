import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { VersionCleanup } from "@/features/releases";
import { sourceEpoch, versionCleanupPreview } from "@/test/factories/runtime";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderWithApp } from "@/test/render";

const props = { vendor: "openai", app: "codex" };

/** The sources; the historical one is gone once `emptied()` (its last copies deleted). */
function sources(emptied = () => false) {
  return mockApi("get", "/admin/api/apps/{vendor}/{app}/sources", () => ({
    items: emptied()
      ? [sourceEpoch()]
      : [
          sourceEpoch({ epoch: 1, current: false, base_url: "https://old.example.com" }),
          sourceEpoch(),
        ],
  }));
}

async function preview(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByRole("textbox", { name: /Minimum version to keep/ }), "0.45.0");
  await user.click(screen.getByRole("button", { name: "Preview cleanup" }));
  return screen.findByRole("region", { name: "Review the selection" });
}

describe("version cleanup", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("previews the selected source epoch and executes after confirmation", async () => {
    let previewBody: unknown;
    let executed: string | undefined;
    useHandlers(
      sources(() => executed !== undefined),
      mockApi(
        "post",
        "/admin/api/apps/{vendor}/{app}/version-cleanup/preview",
        async ({ request }) => {
          previewBody = await request.json();
          return versionCleanupPreview({ source_epoch: 1, unknown_versions: ["nightly"] });
        },
      ),
      mockApi(
        "post",
        "/admin/api/apps/{vendor}/{app}/version-cleanup/{preview_id}/execute",
        ({ params }) => {
          executed = String(params.preview_id);
          return versionCleanupPreview({ executed_at: new Date().toISOString() });
        },
        { status: 200 },
      ),
    );
    await renderWithApp(VersionCleanup, { props });
    const user = userEvent.setup();

    await user.click(await screen.findByRole("combobox", { name: "Cache source" }));
    await user.click(await screen.findByRole("option", { name: /Source 1 \(historical\)/ }));
    const review = await preview(user);
    expect(previewBody).toEqual({ minimum_version: "0.45.0", source_epoch: 1 });
    expect(within(review).getByText("0.40.0")).toBeInTheDocument();
    expect(within(review).getByText(/cannot be compared: nightly/)).toBeInTheDocument();

    await user.click(within(review).getByRole("button", { name: "Delete selected copies" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Delete selected copies" }));
    expect(await screen.findByText(/Cleanup executed/)).toBeInTheDocument();
    expect(executed).toBe(versionCleanupPreview().id);

    // The source list follows: the emptied historical source is gone.
    await user.click(screen.getByRole("combobox", { name: "Cache source" }));
    expect(await screen.findByRole("option", { name: /Source 2 \(current\)/ })).toBeVisible();
    await waitFor(() => {
      expect(screen.queryByRole("option", { name: /historical/ })).toBeNull();
    });
  });

  it("drops a preview that became stale and asks for a new one", async () => {
    useHandlers(
      sources(),
      mockApi("post", "/admin/api/apps/{vendor}/{app}/version-cleanup/preview", () =>
        versionCleanupPreview(),
      ),
      mockApi("post", "/admin/api/apps/{vendor}/{app}/version-cleanup/{preview_id}/execute", () =>
        apiError("PREVIEW_STALE"),
      ),
    );
    await renderWithApp(VersionCleanup, { props });
    const user = userEvent.setup();
    const review = await preview(user);
    await user.click(within(review).getByRole("button", { name: "Delete selected copies" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Delete selected copies",
      }),
    );

    expect(await screen.findByText("Preview no longer valid")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Review the selection" })).toBeNull();
    // Handled inline: no error toast.
    expect(screen.queryByText("fedcba9876543210")).toBeNull();
  });

  it("blocks execution once the preview expired", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    useHandlers(
      sources(),
      mockApi("post", "/admin/api/apps/{vendor}/{app}/version-cleanup/preview", () =>
        versionCleanupPreview(),
      ),
    );
    await renderWithApp(VersionCleanup, { props });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime.bind(vi) });
    const review = await preview(user);
    const execute = within(review).getByRole("button", { name: "Delete selected copies" });
    expect(execute).toBeEnabled();

    await vi.advanceTimersByTimeAsync(10 * 60_000 + 1_000);
    await waitFor(() => {
      expect(execute).toBeDisabled();
    });
    expect(within(review).getByText("Preview expired")).toBeInTheDocument();
  });
});
