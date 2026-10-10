import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { bootstrap, home } from "@/test/factories";
import { catalogApp } from "@/test/factories/catalog";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderEntry } from "@/test/render";

const codex = catalogApp("openai/codex", { name: { en: "Codex CLI", "zh-CN": "Codex CLI" } });
const claude = catalogApp("anthropic/claude-code", {
  name: { en: "Claude Code", "zh-CN": "Claude Code" },
});

function section(name: string) {
  return screen.getByRole("region", { name });
}

describe("home page", () => {
  it("shows featured and popular applications without download counts", async () => {
    useHandlers(
      mockApi("get", "/api/bootstrap", () => bootstrap()),
      mockApi("get", "/api/home", () =>
        home({ pinned: [codex], ranking: [{ app: claude, download_clients: 4321 }] }),
      ),
    );
    await renderEntry("public", "/");
    expect(await screen.findByRole("heading", { level: 2, name: "Featured" })).toBeInTheDocument();
    expect(
      within(section("Featured")).getByRole("link", { name: /Codex CLI/ }),
    ).toHaveAttribute("href", "/openai/codex");
    expect(
      within(section("Popular this week")).getByRole("link", { name: /Claude Code/ }),
    ).toHaveAttribute("href", "/anthropic/claude-code");
    expect(screen.queryByText(/4321|4,321/)).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Browse all applications/ })).toHaveAttribute(
      "href",
      "/all",
    );
  });

  it("hides an empty featured list and explains an empty ranking", async () => {
    useHandlers(
      mockApi("get", "/api/bootstrap", () => bootstrap()),
      mockApi("get", "/api/home", () => home({ pinned: [], ranking: [] })),
    );
    await renderEntry("public", "/");
    expect(await screen.findByText("No downloads in the last 7 days yet.")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Featured" })).not.toBeInTheDocument();
  });

  it("reports a failure and recovers on retry", async () => {
    let fail = true;
    useHandlers(
      mockApi("get", "/api/bootstrap", () => bootstrap()),
      mockApi("get", "/api/home", () =>
        fail ? apiError("STORAGE_UNAVAILABLE", { requestId: "00000000000000aa" }) : home(),
      ),
    );
    await renderEntry("public", "/");
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Server storage is unavailable.");
    expect(alert).toHaveTextContent("00000000000000aa");
    fail = false;
    await userEvent.setup().click(within(alert).getByRole("button", { name: "Retry" }));
    await waitFor(() => {
      expect(screen.getByRole("region", { name: "Featured" })).toBeInTheDocument();
    });
  });
});
