import { screen, waitFor } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { RouterView } from "vue-router";
import type { Schema } from "@/shared/api";
import { globalProxyState, redirectProtectionState } from "@/test/factories/settings";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderWithApp } from "@/test/render";
import ProxySettingsPage from "./ProxySettingsPage.vue";

function proxyServer() {
  let state = globalProxyState();
  let redirect = redirectProtectionState();
  const puts: { ifMatch: string | null; body: unknown }[] = [];
  const redirectPuts: { ifMatch: string | null; body: unknown }[] = [];
  useHandlers(
    mockApi("get", "/admin/api/settings/redirect-protection", () => redirect),
    mockApi("put", "/admin/api/settings/redirect-protection", async ({ request }) => {
      const body = (await request.json()) as Schema<"RedirectProtectionSettings">;
      redirectPuts.push({ ifMatch: request.headers.get("If-Match"), body });
      redirect = { mode: body.mode, revision: redirect.revision + 1 };
      return redirect;
    }),
    mockApi("get", "/admin/api/settings/proxy", () => state),
    mockApi("put", "/admin/api/settings/proxy", async ({ request }) => {
      const body = (await request.json()) as Schema<"GlobalProxySettings">;
      puts.push({ ifMatch: request.headers.get("If-Match"), body });
      if (body.url?.includes("****") && !body.url.startsWith("socks5://user:")) {
        return apiError("PROXY_REDACTED_MISMATCH");
      }
      state = { ...state, ...body, revision: state.revision + 1 };
      if (body.mode === "direct") delete state.url;
      return state;
    }),
  );
  return { puts, redirectPuts };
}

function renderPage() {
  return renderWithApp(RouterView, {
    routes: [{ path: "/admin/settings/proxy", component: ProxySettingsPage }],
    path: "/admin/settings/proxy",
  });
}

describe("global proxy", () => {
  it("keeps the redacted password unless the URL changes and saves a direct connection", async () => {
    const server = proxyServer();
    await renderPage();
    const url = await screen.findByLabelText(/^Proxy URL/, { selector: "input" });
    expect(url).toHaveValue("socks5://user:****@proxy.example.internal:1080");
    const viaProxy = "Upstream connections go through the proxy, which also resolves host names.";
    expect(screen.getByText(viaProxy)).toBeVisible();
    const saveButton = screen.getByRole("button", { name: "Save proxy" });
    expect(saveButton).toBeDisabled();

    // Changing the user while keeping **** is rejected at the field, without a toast.
    const user = userEvent.setup();
    await user.clear(url);
    await user.type(url, "socks5://other:****@proxy.example.internal:1080");
    await user.click(saveButton);
    const message =
      "Enter the proxy password again: the scheme, user or host changed since it was saved.";
    expect(await screen.findByText(message)).toBeVisible();
    expect(url).toHaveAccessibleDescription(expect.stringContaining(message) as string);
    expect(screen.getAllByText(message)).toHaveLength(1);

    // The hint follows the selected mode before saving.
    await user.click(screen.getByRole("radio", { name: "Direct connection" }));
    expect(
      screen.getByText("Upstream connections are made directly, without a proxy."),
    ).toBeVisible();
    expect(screen.queryByText(viaProxy)).toBeNull();
    await user.click(saveButton);
    expect(await screen.findByText(/^Proxy saved\./)).toBeVisible();
    await waitFor(() => {
      expect(
        screen.getByText("Upstream connections are made directly, without a proxy."),
      ).toBeVisible();
    });
    expect(server.puts).toEqual([
      {
        ifMatch: '"4"',
        body: { mode: "url", url: "socks5://other:****@proxy.example.internal:1080" },
      },
      { ifMatch: '"4"', body: { mode: "direct" } },
    ]);
  });

  it("requires a URL in proxy mode", async () => {
    const server = proxyServer();
    await renderPage();
    const url = await screen.findByLabelText(/^Proxy URL/, { selector: "input" });
    const user = userEvent.setup();
    await user.clear(url);
    await user.click(screen.getByRole("button", { name: "Save proxy" }));
    expect(await screen.findByText("Enter the proxy URL.")).toBeVisible();
    expect(server.puts).toEqual([]);
  });

  it("saves the global redirect protection and explains each mode", async () => {
    const server = proxyServer();
    await renderPage();
    const strict = await screen.findByRole("radio", { name: "Strict" });
    expect(strict).toBeChecked();
    expect(screen.queryByRole("radio", { name: "Inherit" })).toBeNull();
    expect(
      screen.getByText(/^Upstream redirects may only reach the configured sources/),
    ).toBeVisible();
    const save = screen.getByRole("button", { name: "Save redirect protection" });
    expect(save).toBeDisabled();

    const user = userEvent.setup();
    await user.click(screen.getByRole("radio", { name: "Basic" }));
    expect(screen.getByText(/^Any target is allowed/)).toBeVisible();
    await user.click(save);
    expect(await screen.findByText(/^Redirect protection saved\./)).toBeVisible();
    expect(server.redirectPuts).toEqual([{ ifMatch: '"1"', body: { mode: "basic" } }]);
  });
});
