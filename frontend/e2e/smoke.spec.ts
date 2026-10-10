import { expect, test, type Page } from "@playwright/test";

const password = process.env.REDAPP_E2E_PASSWORD ?? "";

/** CSP violations or uncaught errors fail the smoke test. */
function collectConsoleErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));
  return errors;
}

test("public home page renders the shell", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await page.goto("/");
  await expect(page.getByRole("banner")).toBeVisible();
  await expect(page.getByRole("combobox", { name: /Search|搜索/ })).toBeVisible();
  await expect(page.getByRole("contentinfo")).toContainText(/v\S+/);
  expect(errors).toEqual([]);
});

test("unknown admin pages show the admin not-found page", async ({ page }) => {
  const response = await page.goto("/admin/does-not-exist");
  expect(response?.status()).toBe(404);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
});

test.describe("administration", () => {
  test.skip(!password, "set REDAPP_E2E_PASSWORD to run the admin smoke tests");

  test("signs in, navigates and signs out", async ({ page }) => {
    const errors = collectConsoleErrors(page);
    await page.goto("/admin/vendors");
    await expect(page).toHaveURL(/\/admin\/login\?returnTo=/);
    await page.getByLabel(/Password|密码/).fill(password);
    await page.getByRole("button", { name: /Sign in|登录/ }).click();
    await expect(page).toHaveURL(/\/admin\/vendors$/);

    const navigation = page.getByRole("navigation", { name: /Main navigation|主导航/ });
    for (const path of ["/admin/overview", "/admin/events", "/admin/settings/site"]) {
      await navigation.locator(`a[href="${path}"]`).click();
      await expect(page).toHaveURL(new RegExp(`${path}$`));
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    }

    await page.getByRole("button", { name: /Administrator|管理员/ }).click();
    await page.getByRole("menuitem", { name: /Sign out|退出登录/ }).click();
    await expect(page).toHaveURL(/\/admin\/login/);
    expect(errors).toEqual([]);
  });

  // Enable once package D implements the site settings page.
  test.fixme("saves the site subtitle and restores it", async ({ page }) => {
    await page.goto("/admin/settings/site");
  });
});
