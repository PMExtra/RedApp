import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { api, unwrap } from "@/shared/api";
import { bootstrap, session } from "@/test/factories";
import { globalStatus } from "@/test/factories/metrics";
import { apiError, mockApi, noContent, useHandlers } from "@/test/msw";
import { renderEntry } from "@/test/render";
import { renderAdminPage } from "@/test/directory";
import { vendor, vendorConfiguration } from "@/test/factories/directory";

const PASSWORD = "correct horse battery";
const emptyPage = { items: [], page: 1, limit: 25, total: 0, total_pages: 1 };

function signedOutServer() {
  let signedIn = false;
  const csrf: (string | null)[] = [];
  useHandlers(
    mockApi("get", "/api/bootstrap", () => bootstrap()),
    // Data of the pages the shell tests visit.
    mockApi("get", "/admin/api/status", () => globalStatus()),
    mockApi("get", "/admin/api/events", () => ({ items: [], next_cursor: null })),
    mockApi("get", "/admin/api/session", () => (signedIn ? session() : apiError("AUTH_REQUIRED"))),
    mockApi("post", "/admin/api/session", async ({ request }) => {
      const { password } = (await request.json()) as { password: string };
      if (password !== PASSWORD) return apiError("LOGIN_FAILED");
      signedIn = true;
      return session();
    }),
    mockApi("delete", "/admin/api/session", ({ request }) => {
      csrf.push(request.headers.get("X-CSRF-Token"));
      signedIn = false;
      return noContent();
    }),
    // Pages visited by the navigation tests.
    mockApi("get", "/admin/api/vendors", () => emptyPage),
    mockApi("get", "/admin/api/categories", () => emptyPage),
  );
  return { csrf, signIn: () => (signedIn = true) };
}

describe("admin sign-in", () => {
  it("sends deep links to sign-in and back after a successful sign-in", async () => {
    signedOutServer();
    const { router } = await renderEntry("admin", "/admin/vendors?q=codex");
    expect(router.currentRoute.value.name).toBe("admin-login");
    expect(router.currentRoute.value.query.returnTo).toBe("/admin/vendors?q=codex");
    const user = userEvent.setup();

    // Required field: the error is announced and tied to the input.
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    const password = screen.getByLabelText(/Password/);
    expect(await screen.findByText("Enter the password.")).toBeInTheDocument();
    expect(password).toHaveAttribute("aria-invalid", "true");
    expect(password).toHaveAccessibleDescription("Enter the password.");

    await user.type(password, "wrong");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("The password is incorrect.")).toBeInTheDocument();

    await user.clear(password);
    await user.type(password, PASSWORD);
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe("/admin/vendors?q=codex");
    });
    expect(screen.getByRole("navigation", { name: "Main navigation" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { level: 1, name: "Vendors" })).toBeInTheDocument();
    await waitFor(() => {
      expect(document.title).toBe("Vendors · Administration · RedApp Mirror");
    });
  });

  it("rejects returnTo targets outside the admin app", async () => {
    const server = signedOutServer();
    server.signIn();
    const { router } = await renderEntry("admin", "/admin/login?returnTo=//evil.example/admin/x");
    expect(router.currentRoute.value.fullPath).toBe("/admin/overview");
  });

  it("signs out with the CSRF token and shows the notice", async () => {
    const server = signedOutServer();
    server.signIn();
    const { router } = await renderEntry("admin", "/admin/overview");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Administrator" }));
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));
    expect(await screen.findByText("You have signed out.")).toBeInTheDocument();
    expect(router.currentRoute.value.name).toBe("admin-login");
    expect(server.csrf).toEqual(["c".repeat(64)]);
  });

  it("asks to sign in again in place when the session expires", async () => {
    const server = signedOutServer();
    server.signIn();
    useHandlers(mockApi("get", "/admin/api/status", () => apiError("AUTH_REQUIRED")));
    const { router } = await renderEntry("admin", "/admin/events");
    await screen.findByRole("heading", { level: 1, name: "Events" });

    await unwrap(api.GET("/admin/api/status")).catch(() => null);
    const dialog = await screen.findByRole("dialog", { name: "Session expired" });
    expect(router.currentRoute.value.name).toBe("admin-events");

    const user = userEvent.setup();
    await user.type(within(dialog).getByLabelText(/Password/), PASSWORD);
    await user.click(within(dialog).getByRole("button", { name: "Sign in" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Session expired" })).toBeNull();
    });
    expect(router.currentRoute.value.name).toBe("admin-events");
  });
});

describe("admin shell", () => {
  it("moves focus to the main content on navigation and shows not-found pages", async () => {
    signedOutServer().signIn();
    const { router } = await renderEntry("admin", "/admin/overview");
    const user = userEvent.setup();
    await screen.findByRole("heading", { level: 1, name: "Overview" });
    await user.click(screen.getAllByRole("link", { name: "Categories" })[0] as HTMLElement);
    await screen.findByRole("heading", { level: 1, name: "Categories" });
    await waitFor(() => {
      expect(document.getElementById("main-content")).toHaveFocus();
    });

    await router.push("/admin/no/such/page");
    expect(
      await screen.findByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeInTheDocument();
    await router.push("/admin/vendors/Not_A_Slug/settings");
    expect(router.currentRoute.value.name).toBe("admin-not-found");
  });

  it("changes the password and requires a new sign-in", async () => {
    signedOutServer().signIn();
    let body: unknown;
    useHandlers(
      mockApi("post", "/admin/api/password", async ({ request }) => {
        body = await request.json();
        return noContent();
      }),
    );
    const { router } = await renderEntry("admin", "/admin/overview");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Administrator" }));
    await user.click(await screen.findByRole("menuitem", { name: "Change password" }));
    const dialog = await screen.findByRole("dialog", { name: "Change password" });
    await user.type(within(dialog).getByLabelText(/^Current password/), "old password!");
    await user.type(within(dialog).getByLabelText(/^New password/), "short");
    await user.type(within(dialog).getByLabelText(/^Confirm new password/), "different");
    await user.click(within(dialog).getByRole("button", { name: "Change password" }));
    expect(await within(dialog).findByText("The passwords do not match.")).toBeInTheDocument();
    expect(within(dialog).getAllByText("12 to 72 bytes.").length).toBeGreaterThan(0);

    await user.clear(within(dialog).getByLabelText(/^New password/));
    await user.type(within(dialog).getByLabelText(/^New password/), "a much longer password");
    await user.clear(within(dialog).getByLabelText(/^Confirm new password/));
    await user.type(
      within(dialog).getByLabelText(/^Confirm new password/),
      "a much longer password",
    );
    await user.click(within(dialog).getByRole("button", { name: "Change password" }));
    expect(
      await screen.findByText("Password changed. Sign in with the new password."),
    ).toBeInTheDocument();
    expect(router.currentRoute.value.name).toBe("admin-login");
    expect(body).toEqual({
      current_password: "old password!",
      new_password: "a much longer password",
    });
  });
});

describe("unsaved drafts around leaving actions", () => {
  async function dirtyVendorSettings(onSignOut: () => Response | Promise<Response>) {
    useHandlers(
      mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
      mockApi("get", "/admin/api/vendors/{vendor}/configuration", () => vendorConfiguration()),
      mockApi("delete", "/admin/api/session", onSignOut),
      mockApi("get", "/admin/api/status", () => globalStatus()),
    );
    const rendered = await renderAdminPage("/admin/vendors/example/settings");
    await rendered.user.type(await screen.findByLabelText(/^Name \(English\)/), " Labs");
    return rendered;
  }

  async function answerDiscard(user: ReturnType<typeof userEvent.setup>, answer: string) {
    const dialog = await screen.findByRole("alertdialog", { name: "Discard unsaved changes?" });
    await user.click(within(dialog).getByRole("button", { name: answer }));
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).toBeNull();
    });
  }

  it("keeps guarding the draft after a failed sign-out and asks once on success", async () => {
    let fail = true;
    const { router, user } = await dirtyVendorSettings(() => {
      if (!fail) return noContent();
      fail = false;
      return apiError("INTERNAL_ERROR");
    });
    await user.click(screen.getByRole("button", { name: "Administrator" }));
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));
    await answerDiscard(user, "Discard");
    expect(await screen.findByText("An unexpected server error occurred.")).toBeInTheDocument();
    expect(router.currentRoute.value.name).toBe("admin-vendor-settings");

    // The earlier answer does not carry over to a later navigation.
    void router.push("/admin/overview");
    await answerDiscard(user, "Keep editing");
    expect(router.currentRoute.value.name).toBe("admin-vendor-settings");

    await user.click(screen.getByRole("button", { name: "Administrator" }));
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));
    await answerDiscard(user, "Discard");
    expect(await screen.findByText("You have signed out.")).toBeInTheDocument();
    expect(router.currentRoute.value.name).toBe("admin-login");
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("confirms the draft before changing the password, then goes to sign-in", async () => {
    useHandlers(mockApi("post", "/admin/api/password", () => noContent()));
    const { router, user } = await dirtyVendorSettings(() => noContent());
    await user.click(screen.getByRole("button", { name: "Administrator" }));
    await user.click(await screen.findByRole("menuitem", { name: "Change password" }));
    await answerDiscard(user, "Keep editing");
    expect(screen.queryByRole("dialog", { name: "Change password" })).toBeNull();

    await user.click(screen.getByRole("button", { name: "Administrator" }));
    await user.click(await screen.findByRole("menuitem", { name: "Change password" }));
    await answerDiscard(user, "Discard");
    const dialog = await screen.findByRole("dialog", { name: "Change password" });
    await user.type(within(dialog).getByLabelText(/^Current password/), "old password!");
    await user.type(within(dialog).getByLabelText(/^New password/), "a much longer password");
    await user.type(
      within(dialog).getByLabelText(/^Confirm new password/),
      "a much longer password",
    );
    await user.click(within(dialog).getByRole("button", { name: "Change password" }));
    expect(
      await screen.findByText("Password changed. Sign in with the new password."),
    ).toBeInTheDocument();
    expect(router.currentRoute.value.name).toBe("admin-login");
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("leaves a deleted vendor without asking, and asks again afterwards", async () => {
    useHandlers(
      mockApi("delete", "/admin/api/vendors/{vendor}", () => noContent()),
      mockApi("get", "/admin/api/vendors", () => ({ ...emptyPage, limit: 12 })),
    );
    const { router, user } = await dirtyVendorSettings(() => noContent());
    await user.click(screen.getByRole("button", { name: "Delete vendor" }));
    const confirmDelete = await screen.findByRole("alertdialog", {
      name: "Delete vendor example?",
    });
    await user.click(within(confirmDelete).getByRole("button", { name: "Delete" }));
    await waitFor(() => {
      expect(router.currentRoute.value.name).toBe("admin-vendors");
    });
    expect(screen.queryByRole("alertdialog")).toBeNull();

    // A new draft elsewhere is guarded as usual.
    await router.push("/admin/vendors/example/settings");
    await user.type(await screen.findByLabelText(/^Name \(English\)/), "!");
    void router.push("/admin/overview");
    await answerDiscard(user, "Keep editing");
    expect(router.currentRoute.value.name).toBe("admin-vendor-settings");
  });
});
