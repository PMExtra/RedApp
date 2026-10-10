import { expect, test } from "@playwright/test";
import { collectConsoleErrors } from "./console";

/**
 * Public site smoke: home → catalog search → application page on a real
 * server with the embedded frontend. Needs at least one published
 * application (a fresh install has the built-in presets).
 */

test("browses from the home page through the catalog search to an application", async ({
  page,
}) => {
  const errors = collectConsoleErrors(page);
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

  await page
    .getByRole("main")
    .getByRole("link", { name: /Browse all applications|浏览全部应用/ })
    .click();
  await expect(page).toHaveURL(/\/all$/);
  const firstApp = page.getByRole("main").getByRole("article").first().getByRole("link");
  await expect(firstApp).toBeVisible();
  const name = (await firstApp.textContent())?.trim() ?? "";
  expect(name).not.toBe("");

  await page.getByRole("searchbox", { name: /Search applications|搜索应用/ }).fill(name);
  // The URL follows the search box once typing pauses.
  await expect.poll(() => new URL(page.url()).searchParams.get("q")).toBe(name);
  const match = page.getByRole("main").getByRole("link", { name, exact: true });
  await expect(match).toBeVisible();

  await match.click();
  await expect(page).toHaveURL(/^[^?]+\/[a-z0-9-]+\/[a-z0-9-]+$/);
  await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
  await expect(page.getByRole("navigation", { name: /Breadcrumbs|面包屑导航/ })).toBeVisible();
  await expect(page).toHaveTitle(new RegExp(`^${name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")} · `));
  expect(errors).toEqual([]);
});

test("unknown applications show the public not-found page", async ({ page }) => {
  const response = await page.goto("/no-such-vendor/no-such-app");
  expect(response?.status()).toBe(404);
  await expect(
    page.getByRole("heading", { level: 1, name: /Page not found|页面不存在/ }),
  ).toBeAttached();
});
