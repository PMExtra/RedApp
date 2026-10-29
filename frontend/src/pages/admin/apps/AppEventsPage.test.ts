import { screen, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import AppEventsPage from "./AppEventsPage.vue";
import { renderAppPage } from "@/test/appPage";
import { operationalEvent } from "@/test/factories/settings";
import { mockApi, useHandlers } from "@/test/msw";

describe("events tab", () => {
  it("lists the application's events without the application column", async () => {
    const paths: string[] = [];
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}/events", ({ request }) => {
        const url = new URL(request.url);
        paths.push(url.pathname + url.search);
        return { items: [operationalEvent(1, { message: "Refused redirect" })], next_cursor: null };
      }),
    );
    await renderAppPage(AppEventsPage, { tab: "events" });
    const table = await screen.findByRole("table", { name: /Recent operational events/ });
    expect(await within(table).findByText("Refused redirect")).toBeVisible();
    expect(within(table).queryByRole("columnheader", { name: "Application" })).toBeNull();
    expect(paths[0]).toMatch(/^\/admin\/api\/apps\/[^/]+\/[^/]+\/events\?limit=50$/);
  });
});
