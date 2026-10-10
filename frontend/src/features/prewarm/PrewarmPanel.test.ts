import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PREWARM_POLL_MS, PrewarmPanel } from "@/features/prewarm";
import type { Schema } from "@/shared/api";
import {
  appConfiguration,
  hexId,
  prewarmItemPage,
  prewarmJob,
  prewarmOptions,
} from "@/test/factories/runtime";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderAppPage } from "@/test/appPage";

const props = { vendor: "openai", app: "codex" };
const STORAGE = "redapp-prewarm-job:openai/codex";

type Job = Schema<"PrewarmJob">;

/** A job that runs for `polls` reads, then completes. */
function jobServer(polls: number) {
  const state = { reads: 0, items: 0, job: prewarmJob() };
  const handlers = [
    mockApi("get", "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}", () => {
      state.reads++;
      if (state.job.state === "running" && state.reads > polls) {
        state.job = { ...state.job, state: "completed", completed: 2, succeeded: 2 };
      }
      return state.job;
    }),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}/items", () => {
      state.items++;
      return prewarmItemPage([
        { key: "linux-x64/codex", status: "downloaded", reason: null, bytes: 1024 },
        {
          key: "darwin-arm64/codex",
          status: state.job.state === "running" ? "pending" : "cached",
          reason: null,
          bytes: 0,
        },
      ]);
    }),
  ];
  return { state, handlers };
}

function common(options = prewarmOptions()) {
  return [
    mockApi("get", "/admin/api/apps/{vendor}/{app}/prewarm/options", () => options),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => appConfiguration()),
  ];
}

describe("prewarm", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("starts a release task, remembers it and polls until it finishes", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const server = jobServer(2);
    let body: Record<string, unknown> | undefined;
    useHandlers(
      ...common(),
      ...server.handlers,
      mockApi("post", "/admin/api/apps/{vendor}/{app}/prewarm/jobs", async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return server.state.job;
      }),
    );
    await renderAppPage(PrewarmPanel, { props });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime.bind(vi) });

    await user.click(await screen.findByRole("button", { name: "Start prewarming" }));
    expect(await screen.findByText("Choose at least one platform.")).toBeInTheDocument();
    await user.click(screen.getAllByRole("checkbox", { name: "Linux x64" })[0] as HTMLElement);
    await user.click(screen.getByRole("button", { name: "Start prewarming" }));

    const task = await screen.findByRole("region", { name: "Prewarm task" });
    expect(body).toMatchObject({
      target: "latest",
      platforms: ["linux-x64"],
      limits: prewarmOptions().default_limits,
    });
    expect(String(body?.request_id)).toMatch(/^[0-9a-f]{32}$/);
    expect(localStorage.getItem(STORAGE)).toBe(server.state.job.id);
    expect(within(task).getByText("Running")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start prewarming" })).toBeDisabled();

    await vi.advanceTimersByTimeAsync(PREWARM_POLL_MS * 3);
    expect(await within(task).findByText("Completed")).toBeInTheDocument();
    expect(await within(task).findByText("Already cached")).toBeInTheDocument();
    const reads = server.state.reads;
    await vi.advanceTimersByTimeAsync(PREWARM_POLL_MS * 4);
    expect(server.state.reads).toBe(reads);
  });

  it("resumes the remembered job, cancels it and stops polling when unmounted", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    localStorage.setItem(STORAGE, prewarmJob().id);
    const server = jobServer(1000);
    let cancelled = false;
    useHandlers(
      ...common(),
      ...server.handlers,
      mockApi(
        "post",
        "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}/cancel",
        () => {
          cancelled = true;
          server.state.job = { ...server.state.job, state: "cancelled", reason: null };
          return server.state.job;
        },
        { status: 202 },
      ),
    );
    const view = await renderAppPage(PrewarmPanel, { props });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime.bind(vi) });

    const task = await screen.findByRole("region", { name: "Prewarm task" });
    expect(within(task).getByText("Running")).toBeInTheDocument();
    await vi.advanceTimersByTimeAsync(PREWARM_POLL_MS);
    await waitFor(() => {
      expect(server.state.reads).toBeGreaterThan(1);
    });

    await user.click(within(task).getByRole("button", { name: "Cancel task" }));
    expect(await within(task).findByText("Cancelled")).toBeInTheDocument();
    expect(cancelled).toBe(true);
    expect(within(task).getByRole("button", { name: "Retry unsuccessful files" })).toBeVisible();

    // Back to running (as if retried elsewhere); then leave the page.
    server.state.job = { ...server.state.job, state: "running" };
    await vi.advanceTimersByTimeAsync(PREWARM_POLL_MS);
    view.unmount();
    const reads = server.state.reads;
    await vi.advanceTimersByTimeAsync(PREWARM_POLL_MS * 5);
    expect(server.state.reads).toBe(reads);
  });

  it("forgets a remembered job the server no longer has", async () => {
    localStorage.setItem(STORAGE, hexId(9));
    useHandlers(
      ...common(),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}", () =>
        apiError("JOB_NOT_FOUND"),
      ),
    );
    await renderAppPage(PrewarmPanel, { props });
    await waitFor(() => {
      expect(localStorage.getItem(STORAGE)).toBeNull();
    });
    expect(screen.queryByRole("region", { name: "Prewarm task" })).toBeNull();
  });

  it("retries the unsuccessful files of a finished job as a new job", async () => {
    const finished = prewarmJob({ state: "completed_with_errors", reason: "prewarm_failed" });
    const retried: Job = prewarmJob({ id: hexId(301), state: "completed" });
    localStorage.setItem(STORAGE, finished.id);
    let retryBody: unknown;
    useHandlers(
      ...common(),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}", ({ params }) =>
        params.job_id === retried.id ? retried : finished,
      ),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}/items", () =>
        prewarmItemPage([
          { key: "linux-x64/codex", status: "failed", reason: "upstream: 502", bytes: 0 },
        ]),
      ),
      mockApi(
        "post",
        "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}/retry",
        async ({ request }) => {
          retryBody = await request.json();
          return retried;
        },
      ),
    );
    await renderAppPage(PrewarmPanel, { props });
    const user = userEvent.setup();
    const task = await screen.findByRole("region", { name: "Prewarm task" });
    expect(await within(task).findByText("upstream: 502")).toBeInTheDocument();
    expect(within(task).getByText("Some files could not be prewarmed.")).toBeInTheDocument();

    await user.click(within(task).getByRole("button", { name: "Retry unsuccessful files" }));
    await waitFor(() => {
      expect(localStorage.getItem(STORAGE)).toBe(retried.id);
    });
    expect(retryBody).toEqual({ request_id: expect.stringMatching(/^[0-9a-f]{32}$/) as string });
    expect(await within(task).findByText("Completed")).toBeInTheDocument();
  });

  it("starts an HTTP cache task from paths, a manifest and a filter", async () => {
    let body: Record<string, unknown> | undefined;
    useHandlers(
      ...common(prewarmOptions({ kind: "http_cache", channels: [], platforms: [] })),
      ...jobServer(0).handlers,
      mockApi("post", "/admin/api/apps/{vendor}/{app}/prewarm/jobs", async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return prewarmJob({ target: null, resolved_version: null, platforms: [] });
      }),
    );
    await renderAppPage(PrewarmPanel, {
      key: "example/mirror",
      props: { vendor: "example", app: "mirror" },
    });
    const user = userEvent.setup();

    await user.type(
      await screen.findByRole("textbox", { name: "File paths" }),
      "/a.zip{Enter}/b.zip",
    );
    const manifest = new File(["/c.zip\n\n/d.zip\n"], "paths.txt", { type: "text/plain" });
    const input = document.querySelector<HTMLInputElement>('input[type="file"]');
    if (!input) throw new Error("no file input");
    await user.upload(input, manifest);
    expect(await screen.findByText("paths.txt: 2 paths")).toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Only files matching a pattern" }));
    const pattern = screen.getByRole("textbox", { name: /Path pattern/ });
    await user.clear(pattern);
    await user.type(pattern, "/**/*.zip");
    await user.click(screen.getByRole("button", { name: "Start prewarming" }));

    await waitFor(() => {
      expect(body).toBeDefined();
    });
    expect(body).toMatchObject({
      paths: ["/a.zip", "/b.zip"],
      manifest: "/c.zip\n\n/d.zip\n",
      match: { type: "glob", pattern: "/**/*.zip" },
    });
    expect(body).not.toHaveProperty("indexes");
  });

  it("saves the automatic prewarm policy of a release application", async () => {
    let patch: unknown;
    useHandlers(
      ...common(),
      mockApi("patch", "/admin/api/apps/{vendor}/{app}/configuration", async ({ request }) => {
        patch = await request.json();
        const saved = appConfiguration({ revision: 8 });
        saved.effective.prewarm = {
          enabled: true,
          channels: ["stable"],
          platforms: ["darwin-arm64"],
        };
        return saved;
      }),
    );
    await renderAppPage(PrewarmPanel, { props });
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("switch", { name: "Prewarm new versions automatically" }),
    );
    const save = screen.getByRole("button", { name: "Save automatic prewarm" });
    expect(save).toBeDisabled();
    expect(screen.getByText(/Choose at least one channel and one platform/)).toBeVisible();

    const auto = screen.getByRole("region", { name: "Automatic prewarm" });
    await user.click(within(auto).getByRole("checkbox", { name: "stable" }));
    await user.click(within(auto).getByRole("checkbox", { name: "macOS Apple silicon" }));
    await user.click(save);
    expect(await screen.findByText("Automatic prewarm saved.")).toBeInTheDocument();
    expect(patch).toEqual({
      set: { prewarm: { enabled: true, channels: ["stable"], platforms: ["darwin-arm64"] } },
    });
  });
});
