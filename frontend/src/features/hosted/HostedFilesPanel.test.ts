import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import { HostedFilesPanel, TRANSFER_FIRST_POLL_MS, TRANSFER_POLL_MS } from "@/features/hosted";
import { renderAppPage } from "@/test/appPage";
import { hostedFile, hostedFilePage } from "@/test/factories/catalog";
import { apiError, mockApi, noContent, useHandlers } from "@/test/msw";

const props = { vendor: "acme", app: "tools" };
const existing = hostedFile("tools/setup.exe", { size_bytes: 4096 });

function list(files = [existing]) {
  return mockApi("get", "/admin/api/apps/{vendor}/{app}/files", () =>
    hostedFilePage({ items: files }),
  );
}

/** Resolves the pending upload/import when `release()` is called. */
function gate() {
  let release: () => void = () => undefined;
  const opened = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { opened, release: () => release() };
}

async function render(extra: { deleted?: boolean } = {}) {
  await renderAppPage(HostedFilesPanel, { key: "acme/tools", props: { ...props, ...extra } });
  return userEvent.setup({
    advanceTimers: (ms) => {
      if (vi.isFakeTimers()) vi.advanceTimersByTime(ms);
    },
  });
}

function fileInput(): HTMLInputElement {
  const input = document.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("no file input");
  return input;
}

describe("hosted files", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("uploads with the multipart fields in order and shows server progress", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const upload = gate();
    let fields: string[] = [];
    let transferId: string | null = null;
    const polls: number[] = [];
    const started = Date.now();
    useHandlers(
      list(),
      http.post("*/admin/api/apps/:vendor/:app/files", async ({ request }) => {
        transferId = new URL(request.url).searchParams.get("transfer_id");
        const form = await request.formData();
        fields = [...form.keys()];
        await upload.opened;
        return HttpResponse.json(hostedFile(form.get("path") as string, { id: "f".repeat(32) }), {
          status: 201,
        });
      }),
      mockApi(
        "get",
        "/admin/api/apps/{vendor}/{app}/files/transfers/{transfer_id}",
        ({ params }) => {
          polls.push(Date.now() - started);
          return {
            id: String(params.transfer_id),
            path: "docs/readme.txt",
            bytes: 1024 * 1024,
            total_bytes: 4 * 1024 * 1024,
            state: "receiving" as const,
          };
        },
      ),
    );
    const user = await render();
    await screen.findByText("tools/setup.exe");

    await user.upload(fileInput(), new File(["hello"], "readme.txt", { type: "text/plain" }));
    const path = screen.getByRole("textbox", { name: /^Path/ });
    expect(path).toHaveValue("readme.txt");
    await user.clear(path);
    await user.type(path, "/docs/readme.txt");
    await user.click(screen.getByRole("button", { name: "Save file" }));

    await vi.advanceTimersByTimeAsync(TRANSFER_FIRST_POLL_MS + TRANSFER_POLL_MS + 10);
    const progressText = await screen.findByText("1.00 MiB of 4.00 MiB");
    expect(screen.getByRole("progressbar", { name: "Transfer progress" })).toBeInTheDocument();
    // Screen readers hear the state, not every progress update.
    expect(screen.getByText("Transferring the file…")).toHaveAttribute("role", "status");
    expect(progressText.closest("[role=status], [aria-live]")).toBeNull();
    expect(polls.length).toBeGreaterThanOrEqual(2);
    expect(polls[0]).toBeGreaterThanOrEqual(TRANSFER_FIRST_POLL_MS);
    expect(transferId).toMatch(/^[0-9a-f]{32}$/);
    expect(fields).toEqual(["path", "file"]);

    upload.release();
    expect(await screen.findByText("docs/readme.txt saved.")).toBeInTheDocument();
    expect(screen.queryByRole("progressbar", { name: "Transfer progress" })).toBeNull();
    const count = polls.length;
    await vi.advanceTimersByTimeAsync(TRANSFER_POLL_MS * 3);
    expect(polls.length).toBe(count);
  });

  it("replaces a file version with expected_id and reports conflicts inline", async () => {
    let fields: [string, string][] = [];
    useHandlers(
      list(),
      http.post("*/admin/api/apps/:vendor/:app/files", async ({ request }) => {
        const form = await request.formData();
        fields = [...form.entries()].map(([key, value]) => [
          key,
          typeof value === "string" ? value : value.name,
        ]);
        return apiError("FILE_CONFLICT");
      }),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/files/transfers/{transfer_id}", () =>
        apiError("TRANSFER_NOT_FOUND"),
      ),
    );
    const user = await render();
    await user.click(await screen.findByRole("button", { name: "Replace tools/setup.exe" }));
    expect(screen.getByRole("textbox", { name: /^Path/ })).toHaveAttribute("readonly");
    await user.upload(fileInput(), new File(["v2"], "setup-v2.exe"));
    await user.click(screen.getByRole("button", { name: "Replace file" }));

    expect(await screen.findByText("The file changed")).toBeInTheDocument();
    expect(fields).toEqual([
      ["path", "tools/setup.exe"],
      ["expected_id", existing.id],
      ["file", "setup-v2.exe"],
    ]);
    // Inline only, no error toast.
    expect(screen.queryByText("fedcba9876543210")).toBeNull();
  });

  it("imports from a URL and cancels the running transfer", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let body: unknown;
    let cancelled: string | undefined;
    useHandlers(
      list([]),
      http.post("*/admin/api/apps/:vendor/:app/files/import", async ({ request }) => {
        body = await request.json();
        // Never answers by itself; the client aborts it.
        await new Promise(() => undefined);
        return HttpResponse.json({});
      }),
      mockApi(
        "get",
        "/admin/api/apps/{vendor}/{app}/files/transfers/{transfer_id}",
        ({ params }) => ({
          id: String(params.transfer_id),
          path: "tools/setup.exe",
          bytes: 2048,
          total_bytes: null,
          state: "receiving" as const,
        }),
      ),
      mockApi(
        "delete",
        "/admin/api/apps/{vendor}/{app}/files/transfers/{transfer_id}",
        ({ params }) => {
          cancelled = String(params.transfer_id);
          return noContent();
        },
      ),
    );
    const user = await render();
    expect(await screen.findByText("No files yet.")).toBeInTheDocument();
    await user.click(screen.getByRole("radio", { name: "Import from a URL" }));
    const urlBox = screen.getByRole("textbox", { name: /HTTP\(S\) URL/ });
    await user.type(urlBox, "https://user:secret@downloads.example.com/setup.exe");
    await user.type(screen.getByRole("textbox", { name: /^Path/ }), "tools/setup.exe");
    await user.click(screen.getByRole("button", { name: "Save file" }));
    expect(await screen.findByText(/without credentials or a fragment/)).toBeVisible();
    expect(body).toBeUndefined();

    // A signed download link keeps its query.
    await user.clear(urlBox);
    await user.type(urlBox, "https://downloads.example.com/setup.exe?token=abc");
    await user.click(screen.getByRole("button", { name: "Save file" }));
    await vi.advanceTimersByTimeAsync(TRANSFER_FIRST_POLL_MS + 10);
    expect(await screen.findByText("2.00 KiB transferred")).toBeInTheDocument();
    expect(body).toEqual({
      path: "tools/setup.exe",
      url: "https://downloads.example.com/setup.exe?token=abc",
    });

    await user.click(screen.getByRole("button", { name: "Cancel transfer" }));
    expect(await screen.findByText("Transfer cancelled. Nothing was saved.")).toBeInTheDocument();
    expect(cancelled).toMatch(/^[0-9a-f]{32}$/);
    await waitFor(() => {
      expect(screen.queryByRole("progressbar", { name: "Transfer progress" })).toBeNull();
    });
  });

  it("stops offering to cancel once the server stores the file", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    useHandlers(
      list([]),
      http.post("*/admin/api/apps/:vendor/:app/files/import", async () => {
        await new Promise(() => undefined);
        return HttpResponse.json({});
      }),
      mockApi(
        "get",
        "/admin/api/apps/{vendor}/{app}/files/transfers/{transfer_id}",
        ({ params }) => ({
          id: String(params.transfer_id),
          path: "tools/setup.exe",
          bytes: 2048,
          total_bytes: 2048,
          state: "committing" as const,
        }),
      ),
    );
    const user = await render();
    await user.click(await screen.findByRole("radio", { name: "Import from a URL" }));
    // Signed links can be long: up to the 8192 characters the server accepts.
    const longUrl = `https://downloads.example.com/setup.exe?token=${"a".repeat(6000)}`;
    await user.click(screen.getByRole("textbox", { name: /HTTP\(S\) URL/ }));
    await user.paste(longUrl);
    expect(screen.getByRole("textbox", { name: /HTTP\(S\) URL/ })).toHaveValue(longUrl);
    await user.type(screen.getByRole("textbox", { name: /^Path/ }), "tools/setup.exe");
    await user.click(screen.getByRole("button", { name: "Save file" }));
    expect(screen.getByRole("button", { name: "Cancel transfer" })).toBeInTheDocument();

    await vi.advanceTimersByTimeAsync(TRANSFER_FIRST_POLL_MS + 10);
    expect(await screen.findAllByText("Saving the file…")).not.toHaveLength(0);
    expect(screen.queryByRole("button", { name: "Cancel transfer" })).toBeNull();
  });

  it("moves to the new last page when its last file is deleted", async () => {
    let files = Array.from({ length: 26 }, (_, index) => hostedFile(`file-${String(index)}.bin`));
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/files", ({ request }) => {
        const page = Number(new URL(request.url).searchParams.get("page") ?? "1");
        return hostedFilePage({
          items: files.slice((page - 1) * 25, page * 25),
          page,
          total: files.length,
        });
      }),
      mockApi("delete", "/admin/api/apps/{vendor}/{app}/files/{file_id}", ({ params }) => {
        files = files.filter((file) => file.id !== String(params.file_id));
        return noContent();
      }),
    );
    const user = await render();
    const table = await screen.findByRole("table", { name: "Hosted files" });
    await user.click(await screen.findByRole("button", { name: "Page 2" }));
    expect(await within(table).findByText("file-25.bin")).toBeInTheDocument();
    await user.click(within(table).getByRole("button", { name: "Delete file-25.bin" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Delete" }),
    );
    expect(await within(table).findByText("file-0.bin")).toBeInTheDocument();
    expect(within(table).queryByText("No files yet.")).toBeNull();
  });

  it("shows a failed upload and keeps the form", async () => {
    useHandlers(
      list([]),
      http.post("*/admin/api/apps/:vendor/:app/files", () => apiError("ARTIFACT_TOO_LARGE")),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/files/transfers/{transfer_id}", () =>
        apiError("TRANSFER_NOT_FOUND"),
      ),
    );
    const user = await render();
    await user.upload(fileInput(), new File(["x"], "big.iso"));
    await user.click(screen.getByRole("button", { name: "Save file" }));
    expect(await screen.findByText("fedcba9876543210")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: /^Path/ })).toHaveValue("big.iso");
    expect(screen.getByRole("button", { name: "Save file" })).toBeEnabled();
  });

  it("deletes a file after confirmation", async () => {
    let deleted: string | undefined;
    useHandlers(
      list(),
      mockApi("delete", "/admin/api/apps/{vendor}/{app}/files/{file_id}", ({ params }) => {
        deleted = String(params.file_id);
        return noContent();
      }),
    );
    const user = await render();
    const table = await screen.findByRole("table", { name: "Hosted files" });
    expect(await within(table).findByRole("link", { name: /tools\/setup\.exe/ })).toHaveAttribute(
      "href",
      "/acme/tools/tools/setup.exe",
    );
    await user.click(within(table).getByRole("button", { name: "Delete tools/setup.exe" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Delete" }),
    );
    expect(await screen.findByText("tools/setup.exe deleted.")).toBeInTheDocument();
    expect(deleted).toBe(existing.id);
  });

  it("explains that files of a deleted application can only be downloaded or deleted", async () => {
    useHandlers(list());
    await render({ deleted: true });
    expect(
      await screen.findByText(
        "This application is deleted. Its files can still be downloaded and deleted, but not added or replaced.",
      ),
    ).toBeVisible();
    const table = await screen.findByRole("table", { name: "Hosted files" });
    expect(await within(table).findByRole("link", { name: /tools\/setup\.exe/ })).toBeVisible();
    expect(within(table).getByRole("button", { name: "Delete tools/setup.exe" })).toBeEnabled();
    expect(within(table).queryByRole("button", { name: "Replace tools/setup.exe" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Save file" })).toBeNull();
  });
});
