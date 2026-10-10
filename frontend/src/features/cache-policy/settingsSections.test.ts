import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { CachePolicySection, ChannelTtlSection } from "@/features/cache-policy";
import type { Schema } from "@/shared/api";
import { renderAppPage } from "@/test/appPage";
import { appConfiguration, httpCacheConfiguration } from "@/test/factories/runtime";
import { apiError, mockApi, useHandlers } from "@/test/msw";

type Patch = Schema<"AppConfigurationPatch">;

describe("cache policy section", () => {
  it("adds a TTL rule, tests its pattern on the server and saves only the rules", async () => {
    let tested: unknown;
    let patch: { body: Patch; ifMatch: string | null } | undefined;
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () =>
        httpCacheConfiguration(),
      ),
      mockApi(
        "post",
        "/admin/api/path-match",
        async ({ request }) => {
          tested = await request.json();
          return { matches: true, path: "/releases/1.0/tool.zip" };
        },
        { status: 200 },
      ),
      mockApi("patch", "/admin/api/apps/{vendor}/{app}/configuration", async ({ request }) => {
        const body = (await request.json()) as Patch;
        patch = { body, ifMatch: request.headers.get("If-Match") };
        return httpCacheConfiguration(
          { rules: body.set?.["http_policy.rules"] ?? [] },
          { revision: 8 },
        );
      }),
    );
    await renderAppPage(CachePolicySection, {
      key: "example/mirror",
      props: { vendor: "example", app: "mirror" },
    });
    const user = userEvent.setup();

    expect(await screen.findByText(/No rules: Cache-Control decides/)).toBeInTheDocument();
    const save = screen.getByRole("button", { name: "Save cache rules" });
    expect(save).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Add TTL rule" }));

    const rule = screen.getByRole("group", { name: "TTL rule 1" });
    const pattern = within(rule).getByRole("textbox", { name: /Path pattern/ });
    await user.clear(pattern);
    await user.type(pattern, "/releases/");
    await user.click(within(rule).getByText("Test a path"));
    const sample = within(rule).getByRole("textbox", { name: "Sample path" });
    await user.clear(sample);
    await user.type(sample, "/releases/1.0/tool.zip");
    await user.click(within(rule).getByRole("button", { name: "Test match" }));
    expect(await within(rule).findByText("Matches /releases/1.0/tool.zip")).toBeInTheDocument();
    expect(tested).toEqual({
      match: { type: "glob", pattern: "/releases/" },
      path: "/releases/1.0/tool.zip",
    });

    await user.click(save);
    expect(await screen.findByText("Cache rules saved.")).toBeInTheDocument();
    expect(patch).toEqual({
      ifMatch: '"7"',
      body: {
        set: {
          "http_policy.rules": [
            { match: { type: "glob", pattern: "/releases/" }, ttl_seconds: 300 },
          ],
        },
      },
    });
    await waitFor(() => {
      expect(save).toBeDisabled();
    });
  });

  it("keeps the draft on a revision conflict and reloads the saved rules on request", async () => {
    let stale = true;
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () =>
        httpCacheConfiguration({ stale_fallback: stale }, { revision: stale ? 7 : 9 }),
      ),
      mockApi("patch", "/admin/api/apps/{vendor}/{app}/configuration", () => {
        stale = false;
        return apiError("REVISION_CONFLICT");
      }),
    );
    await renderAppPage(CachePolicySection, {
      key: "example/mirror",
      props: { vendor: "example", app: "mirror" },
    });
    const user = userEvent.setup();
    const fallback = await screen.findByRole("switch", {
      name: "Serve a stale copy when the origin fails",
    });
    await user.click(screen.getByRole("button", { name: "Add cleanup rule" }));
    await user.click(screen.getByRole("button", { name: "Save cache rules" }));

    expect(await screen.findByText("Changed by someone else")).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "Cleanup rule 1" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Reload latest version" }));
    await waitFor(() => {
      expect(fallback).toHaveAttribute("aria-checked", "false");
    });
    expect(screen.queryByRole("group", { name: "Cleanup rule 1" })).toBeNull();
  });
});

describe("channel TTL section", () => {
  it("saves a new TTL and restores the template value", async () => {
    const bodies: Patch[] = [];
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => appConfiguration()),
      mockApi("patch", "/admin/api/apps/{vendor}/{app}/configuration", async ({ request }) => {
        const body = (await request.json()) as Patch;
        bodies.push(body);
        const saved = appConfiguration({ revision: 7 + bodies.length });
        if (body.set?.cache_ttl_seconds !== undefined) {
          saved.effective.cache_ttl_seconds = body.set.cache_ttl_seconds;
          saved.fields.cache_ttl_seconds = { source: "custom", differs_from_template: true };
        }
        return saved;
      }),
    );
    await renderAppPage(ChannelTtlSection, { props: { vendor: "openai", app: "codex" } });
    const user = userEvent.setup();
    const input = await screen.findByRole("spinbutton", { name: /Channel TTL/ });
    expect(input).toHaveValue("60");

    await user.clear(input);
    await user.type(input, "120");
    await user.tab();
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Channel TTL saved.")).toBeInTheDocument();
    expect(bodies[0]).toEqual({ set: { cache_ttl_seconds: 120 } });

    await user.click(
      await screen.findByRole("button", { name: "Restore the template value: 1 min" }),
    );
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(bodies[1]).toEqual({ unset: ["cache_ttl_seconds"] });
    });
  });
});
