import { screen, waitFor } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { mockApi, useHandlers } from "@/test/msw";
import { renderWithApp } from "@/test/render";
import ReleaseDownloads from "./ReleaseDownloads.vue";
const response: Schema<"ReleaseDownloads"> = {
  channel: "stable",
  release_id: "r-" + "a".repeat(64),
  version: "1.2.3",
  build: "abc",
  artifacts: [
    {
      key: "win-user",
      os: "windows",
      arch: "x64",
      format: "exe",
      scope: "user",
      url: "/microsoft/vscode/releases/fixed/win-user",
      path: "releases/fixed/win-user",
      sha256: "b".repeat(64),
    },
    {
      key: "linux-deb",
      os: "linux",
      arch: "arm64",
      format: "deb",
      scope: "system",
      url: "/microsoft/vscode/releases/fixed/linux-deb",
      path: "releases/fixed/linux-deb",
      sha256: "c".repeat(64),
    },
  ],
};
describe("public release downloads", () => {
  it.each(["en", "zh-CN"] as const)(
    "keeps manual package selection and uses local URLs (%s)",
    async (locale) => {
      useHandlers(mockApi("get", "/api/apps/{vendor}/{app}/downloads", () => response));
      await renderWithApp(ReleaseDownloads, {
        props: { vendor: "microsoft", app: "vscode" },
        locale,
      });
      const user = userEvent.setup();
      const selector = await screen.findByRole("combobox");
      expect(screen.queryByRole("link")).not.toBeInTheDocument();
      await user.click(selector);
      await user.click(await screen.findByRole("option", { name: "Linux · arm64 · deb · system" }));
      await waitFor(() =>
        expect(screen.getByRole("link")).toHaveAttribute("href", response.artifacts[1]?.url),
      );
      expect(screen.getByRole("link").textContent).toContain("Linux · arm64 · deb · system");
      await user.click(selector);
      await user.click(await screen.findByRole("option", { name: "Windows · x64 · exe · user" }));
      expect(screen.getByRole("link")).toHaveAttribute("href", response.artifacts[0]?.url);
    },
  );
});
