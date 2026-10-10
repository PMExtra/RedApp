import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RouterView } from "vue-router";
import { operationalEvent } from "@/test/factories/settings";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderWithApp } from "@/test/render";
import EventsPage from "./EventsPage.vue";

function renderEvents() {
  return renderWithApp(RouterView, {
    routes: [
      { path: "/admin/events", component: EventsPage },
      {
        path: "/admin/:pathMatch(.*)*",
        name: "admin-app-settings",
        component: { render: () => null },
      },
    ],
    path: "/admin/events",
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe("events", () => {
  it("pages through events 50 at a time with cursors", async () => {
    const requests: string[] = [];
    useHandlers(
      mockApi("get", "/admin/api/events", ({ request }) => {
        const url = new URL(request.url);
        requests.push(url.search);
        const cursor = url.searchParams.get("cursor");
        return cursor === "c2"
          ? { items: [operationalEvent(51, { message: "Older failure" })], next_cursor: null }
          : {
              items: Array.from({ length: 50 }, (_, index) => operationalEvent(index + 1)),
              next_cursor: "c2",
            };
      }),
    );
    await renderEvents();
    const table = await screen.findByRole("table", { name: /Recent operational events/ });
    expect(await within(table).findByText("Upstream returned 502 (event 50)")).toBeVisible();
    expect(within(table).getAllByRole("row")).toHaveLength(51);
    expect(within(table).getAllByText("HTTP 502")[0]).toBeVisible();
    expect(within(table).getAllByRole("link", { name: "openai/codex" })[0]).toBeVisible();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Next page" }));
    expect(await within(table).findByText("Older failure")).toBeVisible();
    expect(screen.getByText("Page 2")).toBeVisible();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Previous page" }));
    await waitFor(() => {
      expect(within(table).queryByText("Older failure")).toBeNull();
    });
    expect(requests.slice(0, 2)).toEqual(["?limit=50", "?limit=50&cursor=c2"]);
  });

  it("refreshes automatically and keeps the last page when a refresh fails", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let state: "first" | "newer" | "failing" = "first";
    useHandlers(
      mockApi("get", "/admin/api/events", () => {
        if (state === "failing") return apiError("INTERNAL_ERROR");
        const items = [operationalEvent(2, { message: "Old failure" })];
        if (state === "newer") items.unshift(operationalEvent(1, { message: "New failure" }));
        return { items, next_cursor: null };
      }),
    );
    await renderEvents();
    expect(await screen.findByText("Old failure")).toBeVisible();

    state = "newer";
    await vi.advanceTimersByTimeAsync(5_000);
    expect(await screen.findByText("New failure")).toBeVisible();

    state = "failing";
    await vi.advanceTimersByTimeAsync(5_000);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Refresh failed. Showing the last successful page.",
    );
    expect(screen.getByText("New failure")).toBeVisible();
  });
});
